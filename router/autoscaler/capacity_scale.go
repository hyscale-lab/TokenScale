package autoscaler

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"router/capacity"
	"router/model"
	"router/window"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/google/uuid"
)

type CapacityAutoscaler struct {
	BaseAutoscaler
}

func (d *CapacityAutoscaler) newRequestMonitor(ctx context.Context, s *model.ScalerChan, scalerCounters *ScalerCounters, lastRequestTime *int64, config model.CAPAutoScalerModelConfig) {
	var rpsBuffer int64
	var tpsBuffer int64
	// Allocate the map first
	tpsMapBuffer := make(map[string]*int64)

	// Iterate over keys in tpsCounterMap
	for key := range scalerCounters.tpsCounterMap {
		var v int64 = 0        // initialize value
		tpsMapBuffer[key] = &v // store pointer
	}
	window := 1 * time.Second // custom window duration

	var started int32 = 0 // flag to ensure goroutine starts once
	// set RPS/TPS per s
	rpsRecorder := func() {
		ticker := time.NewTicker(window)
		defer ticker.Stop()
		for range ticker.C {
			// Swap and reset rps count atomically
			rate := atomic.SwapInt64(&rpsBuffer, 0)
			currentRPS := float64(rate) / window.Seconds()

			// Swap and reset tps count atomically
			tokRate := atomic.SwapInt64(&tpsBuffer, 0)
			currentTPS := float64(tokRate) / window.Seconds()
			hlog.CtxInfof(ctx, "Current RPS over %v: %.2f, TPS: %.2f", window, currentRPS, currentTPS)

			// Swap and reset tps counter map atomically
			for key := range scalerCounters.tpsCounterMap {
				tokRate := atomic.SwapInt64(tpsMapBuffer[key], 0)
				currentTPS := float64(tokRate) / window.Seconds()
				atomicStoreFloat64(scalerCounters.tpsCounterMap[key], currentTPS)
			}

			// Atomically update the external rpsCounter pointer with new value
			atomicStoreFloat64(&scalerCounters.rpsCounter, currentRPS)
			atomicStoreFloat64(&scalerCounters.tpsCounter, currentTPS)
		}
	}
	go func() {
		// blocks for
		for {
			query := <-s.PrefillTokenVelocityChan
			hlog.CtxInfof(ctx, "model %s received prefill token velocity query", s.ModelName)

			// Build file path: PROJECT_ROOT_PATH + config.CapacityProfileFilePath
			projectRoot := os.Getenv("PROJECT_ROOT_PATH")
			if projectRoot == "" {
				hlog.CtxInfof(ctx, "PROJECT_ROOT_PATH not set")
				continue
			}
			filePath := filepath.Join(projectRoot, config.CapacityProfileFilePath)

			// Read and parse JSON file
			data, err := os.ReadFile(filePath)
			if err != nil {
				hlog.CtxInfof(ctx, "failed to read file %s: %v", filePath, err)
				continue
			}
			type Profile struct {
				Prefill            int64 `json:"prefill"`
				ConvertiblePrefill int64 `json:"convertible_prefill"`
			}
			var profile Profile
			if err := json.Unmarshal(data, &profile); err != nil {
				hlog.CtxInfof(ctx, "failed to parse JSON %s: %v", filePath, err)
				continue
			}

			prefill := profile.Prefill

			// Do the check
			query.RespChan <- prefill
		}

	}()

	// monitor scaler chan exceed capacity channel, compute whether exceed, then return
	go func() {
		// blocks for
		for {
			query := <-s.ExceedCapacityChan
			currentTps := atomic.LoadInt64(&tpsBuffer)
			hlog.CtxInfof(ctx, "model %s received exceed cap query, current tps buffer: %d", s.ModelName, currentTps)

			// Build file path: PROJECT_ROOT_PATH + config.CapacityProfileFilePath
			projectRoot := os.Getenv("PROJECT_ROOT_PATH")
			if projectRoot == "" {
				hlog.CtxInfof(ctx, "PROJECT_ROOT_PATH not set")
				continue
			}
			filePath := filepath.Join(projectRoot, config.CapacityProfileFilePath)

			// Read and parse JSON file
			data, err := os.ReadFile(filePath)
			if err != nil {
				hlog.CtxInfof(ctx, "failed to read file %s: %v", filePath, err)
				continue
			}
			type Profile struct {
				Prefill            int64 `json:"prefill"`
				ConvertiblePrefill int64 `json:"convertible_prefill"`
			}
			var profile Profile
			if err := json.Unmarshal(data, &profile); err != nil {
				hlog.CtxInfof(ctx, "failed to parse JSON %s: %v", filePath, err)
				continue
			}

			prefill := profile.Prefill

			// Do the check
			isExceed := currentTps > (query.PrefillInstanceNum*prefill) && currentTps < query.PrefillInstanceNum*prefill+config.ConvertInstance*profile.ConvertiblePrefill // can only take prefill requests within a node
			hlog.CtxInfof(ctx, "model %s prefill capacity exceed? %v (tpsBuffer=%d, threshold=%d)",
				s.ModelName, isExceed, currentTps, query.PrefillInstanceNum*prefill)
			query.RespChan <- isExceed
		}

	}()

	for {
		select {
		case scalerRequestInfo := <-s.NewRequest:
			if atomic.CompareAndSwapInt32(&started, 0, 1) {
				go rpsRecorder()
			}
			hlog.CtxDebugf(ctx, "New Request arrived, reqCounter Update")
			atomic.AddInt64(&scalerCounters.reqCounter, 1)
			// request related metrics
			atomic.StoreInt64(lastRequestTime, time.Now().Unix())
			atomic.AddInt64(&rpsBuffer, 1)
			// token related metrics
			atomic.AddInt64(&tpsBuffer, int64(scalerRequestInfo.PromptLength))
			atomic.AddInt64(&scalerCounters.tokCounter, int64(scalerRequestInfo.PromptLength))
			// capacity related metrics
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
func (d *CapacityAutoscaler) calculateNeededInstance(ctx context.Context, tpsCounterMap map[string]*float64, lastReqCounter *int64, config model.CAPAutoScalerModelConfig, scaleDownStableWin *window.Window[int64], instanceCounter int64, now time.Time) int64 {
	// ins := atomic.LoadInt64(instanceCounter)
	// Read provision ratio from environment variable (default to 1.0 if missing/invalid)
	instanceNeededFloat := float64(0)
	for key := range tpsCounterMap {
		targetVal, err := d.CapacityProfileData.ObtainCapacity(key)
		targetVal /= d.ProvisionRatio // over-provision ratio, high means more instances used
		if err != nil {
			hlog.CtxErrorf(ctx, "Failed to obtain capacity for key: %s, %v", key, err)
			panic(err)
		}
		tokenRate := *tpsCounterMap[key]
		instanceNeededFloat += tokenRate / targetVal
		hlog.CtxDebugf(ctx, "[%s] token rate: %.1f tok/s targetVal: %.1f tok/s, instanceNeeded: %.1f", key, tokenRate, targetVal, instanceNeededFloat)
	}
	instanceNeeded := int64(math.Round(instanceNeededFloat))
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

func (d *CapacityAutoscaler) scaler(ctx context.Context, s *model.ScalerChan, config model.CAPAutoScalerModelConfig) {
	// reqCounter := int64(0)
	// instanceCounter := int64(0)
	var instanceCounterLock sync.Mutex
	instanceCreatingCounter := int64(0)
	instanceDeletingCounter := int64(0)
	lastRequestTime := int64(0)
	scaleDownStableWin := window.NewWindow[int64](config.StabilizationWindowSeconds)
	convertInsCounter := int64(0)
	go d.newRequestMonitor(ctx, s, &d.ScalerCounters, &lastRequestTime, config)
	go d.newInstanceMonitor(ctx, s, &d.instanceCounter, &instanceCreatingCounter, &instanceDeletingCounter, &instanceCounterLock)
	ticker := time.NewTicker(1 * time.Second)
	convertInstanceUUIDSet := make(map[string]struct{})
	for now := range ticker.C {

		reqCounter := atomic.LoadInt64(&d.reqCounter)
		instanceCounterLock.Lock()
		instanceCounter := atomic.LoadInt64(&d.instanceCounter)
		instanceCreatingCounterAtomic := atomic.LoadInt64(&instanceCreatingCounter)
		instanceDeletingCounterAtomic := atomic.LoadInt64(&instanceDeletingCounter)
		instanceCounterLock.Unlock()
		if os.Getenv("DISABLE_AUTOSCALER") == "" {
			calculatedInstanceNeeded := d.calculateNeededInstance(ctx, d.tpsCounterMap, &lastRequestTime, config, scaleDownStableWin, instanceCounter, now)
			// calculatedInstanceNeeded += config.ConvertInstance
			d.UpdateInstanceNeeded(calculatedInstanceNeeded)
		}
		modelName := fmt.Sprintf("%v", ctx.Value(modelNameKey))
		hlog.CtxDebugf(ctx, "model %s: reqCounter: %d, instanceCounter: %d, instanceNeeded: %d, instanceCreating: %d, instanceDeleting: %d", modelName, reqCounter, instanceCounter, d.instanceNeeded, instanceCreatingCounterAtomic, instanceDeletingCounterAtomic)
		d.ReportToPrometheus(ctx)

		if d.instanceNeeded > instanceCreatingCounterAtomic+instanceCounter {
			instanceDeltaGauge.WithLabelValues(s.ModelName).Set(1)
			hlog.CtxInfof(ctx, "Decide to create new instance for model %s", modelName)
			atomic.AddInt64(&instanceCreatingCounter, 1)
			uuid := uuid.New().String()
			isConvert := false
			maxRequest := int64(0)
			if convertInsCounter < config.ConvertInstance {
				isConvert = true
				convertInsCounter += 1
				convertInstanceUUIDSet[uuid] = struct{}{}
				maxRequest = config.ConvertMaxRequest
			}
			go func() {
				s.ScaleUpDecision <- model.AllocateRequest{
					InstanceUUID:  uuid,
					GPUNumPerNode: config.TPLevel,
					NodeNum:       1,
					IsConvert:     isConvert,
					MaxRequest:    maxRequest,
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
					if _, exists := convertInstanceUUIDSet[k]; exists {
						hlog.CtxInfof(ctx, "Instance %s is convertiable instance, will not scale down!", k)
						continue
					}
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

func (d *CapacityAutoscaler) RegisterModel(ctx context.Context, modelName string, autoScalerModelConfig map[string]interface{}) (*model.ScalerChan, error) {
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
		PrefillTokenVelocityChan: make(chan  model.PrefillTokenVelocityQuery),
		InstanceNumChan:    make(chan model.InstanceNumQuery),
	}
	var config model.CAPAutoScalerModelConfig
	rawJSON, err := json.Marshal(autoScalerModelConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to re-marshal autoscaler config: %w", err)
	}
	if err := json.Unmarshal(rawJSON, &config); err != nil {
		return nil, fmt.Errorf("failed to decode into %T: %w", config, err)
	}
	d.CapacityProfileData, err = capacity.LoadProfile(config.CapacityProfileFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain capacity profile data from %s, %v", config.CapacityProfileFilePath, err)
	}
	// init tps counter map
	for key := range d.CapacityProfileData.CapacityMap {
		var val float64 = 0
		d.tpsCounterMap[key] = &val
	}

	scalerChan.ScalerType = config.Type
	go d.scaler(context.WithValue(context.Background(), modelNameKey, modelName), scalerChan, config)
	d.Instancelock.Lock()
	d.InstanceMap[modelName] = make(map[string]time.Time)
	d.Instancelock.Unlock()
	return scalerChan, nil
}
