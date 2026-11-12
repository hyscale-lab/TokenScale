package autoscaler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"router/model"
	"router/window"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/google/uuid"
)

type RPSAutoscaler struct {
	BaseAutoscaler
	targetRPS float64
}

type DistsimResult struct {
	PrefillGoodput float64 `json:"prefill_goodput"`
	DecodeGoodput  float64 `json:"decode_goodput"`
}

type DistsimResponse struct {
	Status string        `json:"status"`
	Result DistsimResult `json:"result"`
}

func (d *RPSAutoscaler) updateTargetRPS(ctx context.Context, modelNameRaw string, config model.RPSAutoScalerModelConfig) {

	modelName, modelType, err := model.GetModelNameAndType(modelNameRaw)
	if err != nil {
		hlog.CtxErrorf(ctx, "failed to get model name and type: %v", err)
		return
	}

	var targetRPS float64
	hlog.CtxInfof(ctx, "using 'distserve' method to update target RPS for model: %s", modelNameRaw)
	// TODO: implement your logic here
	requestNumber := window.WorkloadWindow.GetLength()
	if requestNumber <= 1 {
		hlog.CtxWarnf(ctx, "request number: %d <=1, will skip updating target rps", requestNumber)
		return
	}
	workloads, err := window.GetWorkloads(window.WorkloadWindow, time.Now())
	if err != nil {
		hlog.CtxErrorf(ctx, "failed to get workload str: %v", err)
		return
	}
	if len(workloads) >= 2 {
		spanMs := workloads[len(workloads)-1].Arrival - workloads[0].Arrival
		hlog.CtxInfof(ctx, "workload span: %.2f ms (%d requests)", spanMs, len(workloads))
	} else {
		hlog.CtxInfof(ctx, "workload span: N/A (%d request)", len(workloads))
	}
	hlog.CtxInfof(ctx, "running simulator for model: %s, workload str: %v", modelNameRaw, workloads)
	prefillGoodput, decodeGoodput, err := RunSimulator(modelName, workloads, config)
	if err != nil {
		hlog.CtxErrorf(ctx, "failed to run simulator: %v", err)
		return
	}
	hlog.CtxInfof(ctx, "finished running simulator for model: %s", modelNameRaw)
	if modelType == "prefill" {
		targetRPS = prefillGoodput
	} else {
		targetRPS = decodeGoodput
	}
	originalTargetRPS := atomicLoadFloat64(&d.targetRPS)
	atomicStoreFloat64(&d.targetRPS, targetRPS)
	hlog.CtxInfof(ctx, "update target rps from %f req/s to %f req/s", originalTargetRPS, targetRPS)
}

func RunSimulator(modelName string, workloads []window.ReqInfo, config model.RPSAutoScalerModelConfig) (prefillGoodput, decodeGoodput float64, err error) {
	// Step 1: Prepare payload
	payload := map[string]interface{}{
		"model":     modelName,
		"workloads": workloads,
	}

	if config.PrefillTargetMs > 0 {
		payload["prefill_target_ms"] = config.PrefillTargetMs
	}
	if config.DecodeTargetMs > 0 {
		payload["decode_target_ms"] = config.DecodeTargetMs
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Step 2: Send HTTP POST request
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(config.DistsimUrl, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, 0, fmt.Errorf("failed to send POST request: %w", err)
	}
	defer resp.Body.Close()

	// Step 3: Decode response
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("received non-200 response: %d", resp.StatusCode)
	}

	var respData DistsimResponse
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		return 0, 0, fmt.Errorf("failed to decode response: %w", err)
	}

	return respData.Result.PrefillGoodput, respData.Result.DecodeGoodput, nil
}

func (d *RPSAutoscaler) calculateNeededInstance(ctx context.Context, rpsCounter float64, lastReqCounter *int64, config model.RPSAutoScalerModelConfig) int64 {
	// ins := atomic.LoadInt64(instanceCounter)
	targetRPS := atomicLoadFloat64(&d.targetRPS)
	targetRPS /= d.ProvisionRatio
	var instanceNeeded int64
	if targetRPS > 0 {
		instanceNeeded = int64(math.Ceil(rpsCounter / targetRPS))
	} else {
		instanceNeeded = config.MaxInstance
	}
	instanceNeeded = max(instanceNeeded, config.MinInstance)
	instanceNeeded = min(instanceNeeded, config.MaxInstance)
	hlog.CtxInfof(ctx, "min instance is %d, max instance is %d", config.MinInstance, config.MaxInstance)
	if time.Since(time.Unix(atomic.LoadInt64(lastReqCounter), 0)) < time.Duration(config.ScaleDownToZeroGracePeriod)*time.Second {
		// will not scale down to zero
		instanceNeeded = max(instanceNeeded, 1)
	}
	return instanceNeeded
}

func (d *RPSAutoscaler) scaler(ctx context.Context, s *model.ScalerChan, config model.RPSAutoScalerModelConfig) {
	// reqCounter := int64(0)
	// instanceCounter := int64(0)
	var instanceCounterLock sync.Mutex
	instanceCreatingCounter := int64(0)
	instanceDeletingCounter := int64(0)
	lastRequestTime := int64(0)
	atomicStoreFloat64(&d.targetRPS, config.TargetRPS)
	go d.newRequestMonitor(ctx, s, &d.ScalerCounters, &lastRequestTime)
	go d.newInstanceMonitor(ctx, s, &d.instanceCounter, &instanceCreatingCounter, &instanceDeletingCounter, &instanceCounterLock)
	ticker := time.NewTicker(1 * time.Second)
	modelName := fmt.Sprintf("%v", ctx.Value(modelNameKey))
	// start target rps updating loop
	if distsimUrl := config.DistsimUrl; distsimUrl != "" {
		go func() {
			updateTicker := time.NewTicker(10 * time.Second)
			defer updateTicker.Stop()

			for range updateTicker.C {
				d.updateTargetRPS(ctx, modelName, config)
			}
		}()

	}

	for range ticker.C {
		reqCounter := atomic.LoadInt64(&d.reqCounter)
		rpsCounter := atomicLoadFloat64(&d.rpsCounter)
		instanceCounterLock.Lock()
		instanceCounter := atomic.LoadInt64(&d.instanceCounter)
		instanceCreatingCounterAtomic := atomic.LoadInt64(&instanceCreatingCounter)
		instanceDeletingCounterAtomic := atomic.LoadInt64(&instanceDeletingCounter)
		instanceCounterLock.Unlock()
		if os.Getenv("DISABLE_AUTOSCALER") == "" {
			calculatedInstanceNeeded := d.calculateNeededInstance(ctx, rpsCounter, &lastRequestTime, config)
			d.UpdateInstanceNeeded(calculatedInstanceNeeded)
		}
		hlog.CtxDebugf(ctx, "model %s: reqCounter: %d, rpsCounter: %f req/s, targetRPS: %f, instanceCounter: %d, instanceNeeded: %d", modelName, reqCounter, rpsCounter, atomicLoadFloat64(&d.targetRPS), instanceCounter, d.instanceNeeded)
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

func (d *RPSAutoscaler) RegisterModel(ctx context.Context, modelName string, autoScalerModelConfig map[string]interface{}) (*model.ScalerChan, error) {
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
	var config model.RPSAutoScalerModelConfig
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
