package autoscaler

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"router/model"
	"router/window"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/google/uuid"
)

type KPAAutoscaler struct {
	BaseAutoscaler
}

func (d *KPAAutoscaler) calculateNeededInstance(ctx context.Context, metricCounter int64, lastReqCounter *int64, config model.KPAAutoScalerModelConfig, scaleDownStableWin *window.Window[int64], instanceCounter int64, now time.Time) int64 {
	// ins := atomic.LoadInt64(instanceCounter)
	targetValue := int64(math.Ceil(float64(config.TargetValue) / d.ProvisionRatio))
	instanceNeeded := (metricCounter + targetValue - 1) / targetValue
	instanceNeeded = max(instanceNeeded, config.MinInstance)
	instanceNeeded = min(instanceNeeded, config.MaxInstance)
	if time.Since(time.Unix(atomic.LoadInt64(lastReqCounter), 0)) < time.Duration(config.ScaleDownToZeroGracePeriod)*time.Second {
		// will not scale down to zero
		instanceNeeded = max(instanceNeeded, 1)
	}
	// when scale down, use values from stablization winodw
	scaleDownStableWin.Add(now, instanceNeeded)
	if config.StabilizationWindowSeconds > 0 {
		if max, ok := window.Max(scaleDownStableWin, now); ok {
			instanceNeeded = max
		}
	}
	return instanceNeeded
}

func (d *KPAAutoscaler) scaler(ctx context.Context, s *model.ScalerChan, config model.KPAAutoScalerModelConfig) {
	// reqCounter := int64(0)
	// instanceCounter := int64(0)
	var instanceCounterLock sync.Mutex
	instanceCreatingCounter := int64(0)
	instanceDeletingCounter := int64(0)
	lastRequestTime := int64(0)
	scaleDownStableWin := window.NewWindow[int64](config.StabilizationWindowSeconds)
	go d.newRequestMonitor(ctx, s, &d.ScalerCounters, &lastRequestTime)
	go d.newInstanceMonitor(ctx, s, &d.instanceCounter, &instanceCreatingCounter, &instanceDeletingCounter, &instanceCounterLock)
	ticker := time.NewTicker(1 * time.Second)
	for now := range ticker.C {
		var metricCounter int64

		switch config.TargetMetric {
		case "req_counter":
			metricCounter = atomic.LoadInt64(&d.reqCounter)
		case "tok_counter":
			metricCounter = atomic.LoadInt64(&d.tokCounter)
		case "rps_counter":
			metricCounter = int64(atomicLoadFloat64(&d.rpsCounter))
		case "tps_counter":
			metricCounter = int64(atomicLoadFloat64(&d.tpsCounter))
		}
		reqCounter := atomic.LoadInt64(&d.reqCounter)
		instanceCounterLock.Lock()
		instanceCounter := atomic.LoadInt64(&d.instanceCounter)
		instanceCreatingCounterAtomic := atomic.LoadInt64(&instanceCreatingCounter)
		instanceDeletingCounterAtomic := atomic.LoadInt64(&instanceDeletingCounter)
		instanceCounterLock.Unlock()
		if os.Getenv("DISABLE_AUTOSCALER") == "" {
			calculatedInstanceNeeded := d.calculateNeededInstance(ctx, metricCounter, &lastRequestTime, config, scaleDownStableWin, instanceCounter, now)
			d.UpdateInstanceNeeded(calculatedInstanceNeeded)
		}
		modelName := fmt.Sprintf("%v", ctx.Value(modelNameKey))
		hlog.CtxDebugf(ctx, "model %s: reqCounter: %d, instanceCounter: %d, instanceNeeded: %d", modelName, reqCounter, instanceCounter, d.instanceNeeded)
		d.ReportToPrometheus(ctx)

		if d.instanceNeeded > instanceCreatingCounterAtomic+instanceCounter {
			instanceDeltaGauge.WithLabelValues(s.ModelName).Set(1)
			hlog.CtxInfof(ctx, "Decide to create new instance")
			atomic.AddInt64(&instanceCreatingCounter, 1)
			go func() {
				s.ScaleUpDecision <- model.AllocateRequest{
					InstanceUUID:  uuid.New().String(),
					GPUNumPerNode: config.TPLevel,
					NodeNum:       1,
				}
			}()
		} else if d.instanceNeeded < instanceDeletingCounterAtomic+instanceCounter {
			hlog.CtxInfof(ctx, "Decide to delete instance")
			instanceDeltaGauge.WithLabelValues(s.ModelName).Set(-1)
			func() {
				atomic.AddInt64(&instanceDeletingCounter, -1)
				d.Instancelock.Lock()
				defer d.Instancelock.Unlock()
				uuid := ""
				for k, v := range d.InstanceMap[s.ModelName] {
					if !v.IsZero() && time.Since(v) > time.Duration(config.MinLifeTime)*time.Second {
						hlog.CtxInfof(ctx, "Instance %s is ready to delete, which is created at %s (lasting for %s seconds)", k, v, time.Since(v))
						uuid = k
						break
					}
				}
				if uuid == "" {
					hlog.CtxInfof(ctx, "No instance to delete")
					atomic.AddInt64(&instanceDeletingCounter, 1)
					return
				}
				d.InstanceMap[s.ModelName][uuid] = time.Time{}
				go func() {
					s.ScaleDownDecision <- uuid
				}()
			}()
		} else {
			instanceDeltaGauge.WithLabelValues(s.ModelName).Set(0)
		}
	}
}

func (d *KPAAutoscaler) RegisterModel(ctx context.Context, modelName string, autoScalerModelConfig map[string]interface{}) (*model.ScalerChan, error) {
	hlog.CtxInfof(ctx, "RegisterModel %s", modelName)
	scalerChan := &model.ScalerChan{
		ModelName:          modelName,
		ScaleUpDecision:    make(chan model.AllocateRequest),
		ScaleUpResult:      make(chan interface{}),
		ScaleDownDecision:  make(chan string),
		ScaleDownResult:    make(chan interface{}),
		NewRequest:         make(chan model.ScalerRequest),
		FinishedRequest:    make(chan model.ScalerRequest),
		ExceedCapacityChan: make(chan model.ExceedCapacityQuery),
		InstanceNumChan:    make(chan model.InstanceNumQuery),
	}
	var config model.KPAAutoScalerModelConfig
	rawJSON, err := json.Marshal(autoScalerModelConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to re-marshal autoscaler config: %w", err)
	}
	if err := json.Unmarshal(rawJSON, &config); err != nil {
		return nil, fmt.Errorf("failed to decode into %T: %w", config, err)
	}
	scalerChan.ScalerType = config.Type
	go d.scaler(context.WithValue(context.Background(), modelNameKey, modelName), scalerChan, config)
	d.Instancelock.Lock()
	d.InstanceMap[modelName] = make(map[string]time.Time)
	d.Instancelock.Unlock()
	return scalerChan, nil
}
