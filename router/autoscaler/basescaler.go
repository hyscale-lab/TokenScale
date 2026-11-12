package autoscaler

import (
	"context"
	"fmt"
	"math"
	"router/capacity"
	"router/model"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/cloudwego/hertz/pkg/common/hlog"
)

type ScalerCounters struct {
	reqCounter         int64
	rpsCounter		   float64
	tokCounter		   int64
	tpsCounter		   float64
	tpsCounterMap      map[string]*float64
}

type BaseAutoscaler struct {
	InstanceMap        map[string]map[string]time.Time
	Instancelock       sync.Mutex
	ScalerCounters
	config             map[string]interface{}
	instanceCounter    int64
	instanceNeeded     int64
	instanceNeededLock sync.Mutex
	CapacityProfileData        *capacity.CapacityProfileData  `json:"capacity_profile_data"`
	ProvisionRatio     float64
}

func (d *BaseAutoscaler) ReportToPrometheus(ctx context.Context) {

	modelName := fmt.Sprintf("%v", ctx.Value(modelNameKey))
	reqCounterGauge.WithLabelValues(modelName).Set(float64(d.reqCounter))
	rpsCounterGauge.WithLabelValues(modelName).Set(d.rpsCounter)
	tokCounterGauge.WithLabelValues(modelName).Set(float64(d.tokCounter))
	tpsCounterGauge.WithLabelValues(modelName).Set(d.tpsCounter)
	insCounterGauge.WithLabelValues(modelName).Set(float64(d.instanceCounter))
	insNeededGauge.WithLabelValues(modelName).Set(float64(d.instanceNeeded))
	for key := range d.tpsCounterMap {
		tpsCounterMapGauge.WithLabelValues(modelName, key).Set(*d.tpsCounterMap[key])
	}
}

func (d *BaseAutoscaler) UpdateInstanceNeeded(instanceNeeded int64) {
	d.instanceNeededLock.Lock()
	d.instanceNeeded = instanceNeeded
	d.instanceNeededLock.Unlock()
}

func (d *BaseAutoscaler) newInstanceMonitor(ctx context.Context, s *model.ScalerChan, instanceCounter, instanceCreatingCounter, instanceDeletingCounter *int64, instanceCounterLock *sync.Mutex) {
	// monitor for instance query
	go func() {
		hlog.CtxInfof(ctx, "Start to monitor instance num query channel")
		for {
			query := <- s.InstanceNumChan
			hlog.CtxInfof(ctx, "model %s received query on instance number, returning %d", s.ModelName, *instanceCounter)
			query.RespChan <- *instanceCounter
		}
	}()
	for {
		select {
		case result := <-s.ScaleUpResult:
			res := result.(model.Pair)
			success, ok := res.First.(bool)
			if !ok {
				hlog.CtxErrorf(ctx, "Invalid ScaleUpResult for autoscaler")
				continue
			}
			uuid, ok := res.Second.(string)
			if !ok {
				hlog.CtxErrorf(ctx, "Invalid ScaleUpResult for autoscaler")
				continue
			}
			instanceCounterLock.Lock()
			atomic.AddInt64(instanceCreatingCounter, -1)
			if !success {
				hlog.CtxInfof(ctx, "Instance %s is failed to create", uuid)
				instanceCounterLock.Unlock()
				continue
			}
			atomic.AddInt64(instanceCounter, 1)
			instanceCounterLock.Unlock()
			hlog.CtxInfof(ctx, "Instance %s is created", uuid)
			d.Instancelock.Lock()
			d.InstanceMap[s.ModelName][uuid] = time.Now()
			d.Instancelock.Unlock()
		case uuid := <-s.ScaleDownResult:
			uuid_str, ok := uuid.(string)
			instanceCounterLock.Lock()
			atomic.AddInt64(instanceCounter, -1)
			atomic.AddInt64(instanceDeletingCounter, 1)
			instanceCounterLock.Unlock()
			if !ok {
				hlog.CtxErrorf(ctx, "Invalid ScaleDownResult for autoscaler")
				continue
			}
			hlog.CtxInfof(ctx, "Instance %s is deleted", uuid_str)
			d.Instancelock.Lock()
			delete(d.InstanceMap[s.ModelName], uuid_str)
			d.Instancelock.Unlock()
		}
	}
}

// atomicStoreFloat64 atomically stores a float64 value to the given pointer.
func atomicStoreFloat64(ptr *float64, val float64) {
    atomic.StoreUint64((*uint64)(unsafe.Pointer(ptr)), math.Float64bits(val))
}

// atomicLoadFloat64 atomically loads a float64 value from the given pointer.
func atomicLoadFloat64(ptr *float64) float64 {
    bits := atomic.LoadUint64((*uint64)(unsafe.Pointer(ptr)))
    return math.Float64frombits(bits)
}

func (d *BaseAutoscaler) newRequestMonitor(ctx context.Context, s *model.ScalerChan, scalerCounters *ScalerCounters, lastRequestTime *int64) {
	var rpsBuffer int64
	var tpsBuffer int64
	tpsMapBuffer := make(map[string]*int64)

	for key := range scalerCounters.tpsCounterMap {
		var v int64 = 0
		tpsMapBuffer[key] = &v
	}

	window := 1 * time.Second
	var started int32 = 0 // flag to ensure goroutine starts once

	rpsRecorder := func() {
			ticker := time.NewTicker(window)
			defer ticker.Stop()

			for range ticker.C {
				// Swap and reset RPS
				rate := atomic.SwapInt64(&rpsBuffer, 0)
				currentRPS := float64(rate) / window.Seconds()

				// Swap and reset TPS
				tokRate := atomic.SwapInt64(&tpsBuffer, 0)
				currentTPS := float64(tokRate) / window.Seconds()
				hlog.CtxInfof(ctx, "Current RPS over %v: %.2f, TPS: %.2f", window, currentRPS, currentTPS)

				// Swap and reset TPS map
				for key := range scalerCounters.tpsCounterMap {
					tokRate := atomic.SwapInt64(tpsMapBuffer[key], 0)
					currentTPS := float64(tokRate) / window.Seconds()
					atomicStoreFloat64(scalerCounters.tpsCounterMap[key], currentTPS)
				}

				// Update external counters
				atomicStoreFloat64(&scalerCounters.rpsCounter, currentRPS)
				atomicStoreFloat64(&scalerCounters.tpsCounter, currentTPS)
			}
		}

	for {
		select {
		case scalerRequestInfo := <-s.NewRequest:
			// Start RPS recorder goroutine on first request
			if atomic.CompareAndSwapInt32(&started, 0, 1) {
				go rpsRecorder()
			}

			hlog.CtxDebugf(ctx, "New Request arrived, reqCounter Update")
			atomic.AddInt64(&scalerCounters.reqCounter, 1)
			atomic.StoreInt64(lastRequestTime, time.Now().Unix())
			atomic.AddInt64(&rpsBuffer, 1)
			atomic.AddInt64(&tpsBuffer, int64(scalerRequestInfo.PromptLength))
			atomic.AddInt64(&scalerCounters.tokCounter, int64(scalerRequestInfo.PromptLength))

			if d.CapacityProfileData != nil {
				requestType, err := d.CapacityProfileData.PredictRequestType(scalerRequestInfo.PromptLength, scalerRequestInfo.GtDecodeLength)
				if err != nil {
					hlog.CtxErrorf(ctx, "failed to predict request type, err: %v", err)
				}
				inputTok := requestType.InputLength
				predictedOutputTok := requestType.OutputBucket
				totalTok := int64(inputTok + predictedOutputTok)
				atomic.AddInt64(tpsMapBuffer[requestType.String()], totalTok)
			}

		case scalerRequestInfo := <-s.FinishedRequest:
			hlog.CtxDebugf(ctx, "Request Finished, reqCounter Update")
			atomic.AddInt64(&scalerCounters.reqCounter, -1)
			atomic.AddInt64(&scalerCounters.tokCounter, int64(-scalerRequestInfo.PromptLength))
		}
	}
}

func (d *BaseAutoscaler) RegisterNode(ctx context.Context, node *model.Node) {
	hlog.CtxInfof(ctx, "RegisterNode %s", node.Host)
	hlog.CtxInfof(ctx, "In Default mode, node information is NOT used")
}

func (d *BaseAutoscaler) GetScalerInfo(ctx context.Context) ScalerInfo {
	req := atomic.LoadInt64(&d.reqCounter)
	ins := atomic.LoadInt64(&d.instanceCounter)
	target, ok := d.config["target_value"].(int64)
	if !ok {
		panic("target_value is not int64")
	}
	scalerInfo := ScalerInfo{
		ReqCounter:      req,
		InstanceCounter: ins,
		TargetValue: target,
	}
	return scalerInfo
}
