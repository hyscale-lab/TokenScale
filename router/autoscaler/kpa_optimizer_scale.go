package autoscaler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"router/model"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/google/uuid"
)

type KPAOptimizerAutoscaler struct {
	BaseAutoscaler
}

func (d *KPAOptimizerAutoscaler) calculateNeededInstance(ctx context.Context, modelName string, config model.KPAOptimizerAutoScalerModelConfig) int64 {
	hlog.CtxInfof(ctx, "[KPA_OPT] Querying optimizer HTTP server for model %s", modelName)

	url := fmt.Sprintf("http://%s:%d/optimizer?model_name=%s", config.OptimizerHost, config.OptimizerPort, modelName)
	resp, err := http.Get(url)
	if err != nil {
		hlog.CtxWarnf(ctx, "[KPA_OPT] HTTP request failed: %v", err)
		return config.MinInstance
	}
	defer resp.Body.Close()

	var result struct {
		SuggestedReplicas int64  `json:"suggested_replicas"`
		ModelName         string `json:"model_name"`
	}

	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		hlog.CtxWarnf(ctx, "[KPA_OPT] Failed to decode JSON: %v", err)
		return config.MinInstance
	}

	replicas := result.SuggestedReplicas
	hlog.CtxInfof(ctx, "[KPA_OPT] Suggested replicas = %d", replicas)
	replicas = max(replicas, config.MinInstance)
	replicas = min(replicas, config.MaxInstance)
	return replicas
}

func (d *KPAOptimizerAutoscaler) scaler(ctx context.Context, s *model.ScalerChan, config model.KPAOptimizerAutoScalerModelConfig) {
	var instanceCounterLock sync.Mutex
	instanceCreatingCounter := int64(0)
	instanceDeletingCounter := int64(0)
	lastRequestTime := int64(0)
	go d.newRequestMonitor(ctx, s, &d.ScalerCounters, &lastRequestTime)
	go d.newInstanceMonitor(ctx, s, &d.instanceCounter, &instanceCreatingCounter, &instanceDeletingCounter, &instanceCounterLock)
	ticker := time.NewTicker(1 * time.Second)
	for range ticker.C {
		instanceCounterLock.Lock()
		instanceCounter := atomic.LoadInt64(&d.instanceCounter)
		instanceCreatingCounterAtomic := atomic.LoadInt64(&instanceCreatingCounter)
		instanceDeletingCounterAtomic := atomic.LoadInt64(&instanceDeletingCounter)
		instanceCounterLock.Unlock()
		if os.Getenv("DISABLE_AUTOSCALER") == "" {
			calculatedInstanceNeeded := d.calculateNeededInstance(ctx, s.ModelName, config)
			d.UpdateInstanceNeeded(calculatedInstanceNeeded)
			hlog.CtxDebugf(ctx, "[KPA_OPT] calculatedInstanceNeeded is %d", calculatedInstanceNeeded)
		}
		modelName := s.ModelName
		reqCounter := atomic.LoadInt64(&d.reqCounter)
		hlog.CtxDebugf(ctx, "[KPA_OPT] model %s: reqCounter: %d, instanceCounter: %d, instanceNeeded: %d", modelName, reqCounter, instanceCounter, d.instanceNeeded)
		d.ReportToPrometheus(ctx)
		if d.instanceNeeded > instanceCreatingCounterAtomic+instanceCounter {
			atomic.AddInt64(&instanceCreatingCounter, 1)
			instanceDeltaGauge.WithLabelValues(s.ModelName).Set(1)
			hlog.CtxInfof(ctx, "[KPA_OPT] Decide to create new instance")
			go func() {
				s.ScaleUpDecision <- model.AllocateRequest{
					InstanceUUID:  uuid.New().String(),
					GPUNumPerNode: config.TPLevel,
					NodeNum:       1,
				}
			}()
		} else if d.instanceNeeded < instanceDeletingCounterAtomic+instanceCounter {
			hlog.CtxInfof(ctx, "[KPA_OPT] Decide to delete instance")
			instanceDeltaGauge.WithLabelValues(s.ModelName).Set(-1)
			atomic.AddInt64(&instanceDeletingCounter, -1)
			d.Instancelock.Lock()
			uuid := ""
			for k, v := range d.InstanceMap[s.ModelName] {
				if !v.IsZero() && time.Since(v) > time.Duration(config.MinLifeTime)*time.Second {
					uuid = k
					break
				}
			}
			d.Instancelock.Unlock()
			if uuid == "" {
				hlog.CtxInfof(ctx, "[KPA_OPT] No instance to delete")
				atomic.AddInt64(&instanceDeletingCounter, 1)
				continue
			}
			d.InstanceMap[s.ModelName][uuid] = time.Time{}
			go func() { s.ScaleDownDecision <- uuid }()
		} else {
			instanceDeltaGauge.WithLabelValues(s.ModelName).Set(0)
		}
	}
}

func (d *KPAOptimizerAutoscaler) RegisterModel(ctx context.Context, modelName string, autoScalerModelConfig map[string]interface{}) (*model.ScalerChan, error) {
	hlog.CtxInfof(ctx, "[KPA_OPT] RegisterModel %s", modelName)
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
	var config model.KPAOptimizerAutoScalerModelConfig
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
