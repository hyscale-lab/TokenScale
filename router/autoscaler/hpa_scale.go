package autoscaler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"router/detect"
	"router/model"
	"router/window"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/google/uuid"
)

type HPAAutoscaler struct {
	BaseAutoscaler
}

// Pair holds “timestamp, value” for one series.
type Pair struct {
	Timestamp int64   // Unix seconds
	Value     float64 // parsed from the Prom response
}

// obtainLatestMetric hits /api/v1/query and returns one Pair per series.
func obtainLatestMetric(ctx context.Context, cfg model.HPAAutoScalerModelConfig) ([]Pair, error) {
	promAddr := detect.GetPrometheusAddress() // "ip:port"
	if promAddr == "" {
		return nil, fmt.Errorf("prometheus address is empty")
	}

	// Build URL   http://<ip:port>/api/v1/query?query=<metric>
	u := url.URL{
		Scheme: "http",
		Host:   promAddr,
		Path:   "/api/v1/query",
	}
	q := u.Query()
	q.Set("query", cfg.PromMetric)
	u.RawQuery = q.Encode()

	cli := &http.Client{Timeout: 5 * time.Second}
	resp, err := cli.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("query Prometheus: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("prometheus %s: %s", resp.Status, body)
	}

	// extended JSON model with metric labels
	var raw struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string  `json:"metric"`
				Value  [2]json.RawMessage `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if raw.Status != "success" || raw.Data.ResultType != "vector" {
		return nil, fmt.Errorf("unexpected result type: %s", raw.Data.ResultType)
	}

	if len(raw.Data.Result) == 0 {
		return []Pair{}, nil
	}

	pairs := make([]Pair, 0, len(raw.Data.Result))
	for _, r := range raw.Data.Result {
		// 🔍 filter by label "liquid_model"
		if r.Metric["liquid_model"] != ctx.Value(modelNameKey) {
			continue
		}

		var tsF float64
		var valStr string
		if err := json.Unmarshal(r.Value[0], &tsF); err != nil {
			return nil, fmt.Errorf("parse timestamp: %w", err)
		}
		if err := json.Unmarshal(r.Value[1], &valStr); err != nil {
			return nil, fmt.Errorf("parse value: %w", err)
		}
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			return nil, fmt.Errorf("convert value: %w", err)
		}
		pairs = append(pairs, Pair{Timestamp: int64(tsF), Value: val})
	}
	hlog.CtxInfof(ctx, "obtain metrics fetched %d instances!", len(pairs))

	return pairs, nil
}

func (d *HPAAutoscaler) calculateNeededInstance(ctx context.Context, instanceCounter int64, now time.Time, config model.HPAAutoScalerModelConfig, scaleDownStableWin *window.Window[int64]) int64 {
	metrics, err := obtainLatestMetric(ctx, config)
	if err != nil {
		hlog.CtxErrorf(ctx, "Failed to obtain latest metrics: %v", err)
		return 0
	}
	avgMetric := 0.0
	for _, metric := range metrics {
		avgMetric += metric.Value / float64(len((metrics)))
	}

	scaleRatio := avgMetric * d.ProvisionRatio / float64(config.TargetUtilization)
	// tolerate a certain ratio of down and up scaling
	if scaleRatio >= 1-config.DownTolerance && scaleRatio <= 1+config.UpTolerance {
		scaleRatio = 1
	}
	instanceNeeded := int64(math.Ceil(scaleRatio * float64(instanceCounter)))

	instanceNeeded = max(instanceNeeded, config.MinInstance)
	instanceNeeded = min(instanceNeeded, config.MaxInstance)
	hlog.CtxDebugf(ctx, "model %v: avgMetric for %s: %f, scaleRatio:%f", ctx.Value(modelNameKey), config.PromMetric, avgMetric, scaleRatio)
	scaleDownStableWin.Add(now, instanceNeeded)
	if instanceNeeded < instanceCounter && config.StabilizationWindowSeconds > 0 {
		if max, ok := window.Max(scaleDownStableWin, now); ok {
			instanceNeeded = max
		}
	}
	return instanceNeeded

}

func (d *HPAAutoscaler) scaler(ctx context.Context, s *model.ScalerChan, config model.HPAAutoScalerModelConfig) {
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
		reqCounter := atomic.LoadInt64(&d.reqCounter)
		instanceCounterLock.Lock()
		instanceCounter := atomic.LoadInt64(&d.instanceCounter)
		instanceCreatingCounterAtomic := atomic.LoadInt64(&instanceCreatingCounter)
		instanceDeletingCounterAtomic := atomic.LoadInt64(&instanceDeletingCounter)
		instanceCounterLock.Unlock()
		calculatedInstanceNeeded := d.calculateNeededInstance(ctx, instanceCounter, now, config, scaleDownStableWin)
		if os.Getenv("DISABLE_AUTOSCALER") == "" {
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
					if !v.IsZero() && time.Since(v) > time.Duration(1)*time.Second {
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

func (d *HPAAutoscaler) RegisterModel(ctx context.Context, modelName string, autoScalerModelConfig map[string]interface{}) (*model.ScalerChan, error) {
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
	var config model.HPAAutoScalerModelConfig
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
