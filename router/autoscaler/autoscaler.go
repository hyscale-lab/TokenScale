package autoscaler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"router/metrics"
	"router/model"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	AutoscalerMap map[string]Autoscaler = make(map[string]Autoscaler)
)

type ScalerInfo struct {
	ReqCounter      int64 `json:"req_counter"`
	InstanceCounter int64 `json:"instance_counter"`
	TargetValue		int64 `json:"target_value"`
}

type Autoscaler interface {
	RegisterModel(ctx context.Context, modelName string, autoScalerModelConfig map[string]interface{}) (*model.ScalerChan, error)
	RegisterNode(ctx context.Context, node *model.Node)
	GetScalerInfo(ctx context.Context) ScalerInfo
	UpdateInstanceNeeded(instanceNeeded int64)
}

var (
	instanceDeltaGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "router:instance_delta",
			Help: "Number of instances changed by autoscaler",
		},
		[]string{"model"},
	)
	reqCounterGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "router:req_counter",
			Help: "Number of requests processing",
		},
		[]string{"model"},
	)
	tokCounterGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "router:tok_counter",
			Help: "Number of tokens processing",
		},
		[]string{"model"},
	)
	rpsCounterGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "router:rps_counter",
			Help: "Number of requests arriving per second",
		},
		[]string{"model"},
	)
	tpsCounterGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "router:tps_counter",
			Help: "Number of token arriving per second",
		},
		[]string{"model"},
	)
	insCounterGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "router:ins_counter",
			Help: "Instance counter",
		},
		[]string{"model"},
	)
	insNeededGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "router:ins_needed",
			Help: "Instance needed by autoscaler",
		},
		[]string{"model"},
	)
	tpsCounterMapGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "router:tps_counter_map",
			Help: "Number of token arriving per second for each requestType",
		},
		[]string{"model", "requestType"},
	)
)

func init() {
	metrics.Register(instanceDeltaGauge)
	metrics.Register(reqCounterGauge)
	metrics.Register(rpsCounterGauge)
	metrics.Register(tokCounterGauge)
	metrics.Register(tpsCounterGauge)
	metrics.Register(insCounterGauge)
	metrics.Register(insNeededGauge)
	metrics.Register(tpsCounterMapGauge)
}

func RegisterNode(ctx context.Context, node *model.Node) {
	hlog.CtxInfof(ctx, "RegisterNode %s", node.Host)
	hlog.CtxInfof(ctx, "In current autoscaler, node information is NOT used")
}

func RegisterModel(ctx context.Context, modelName string, autoScalerConfig map[string]interface{}) (*model.ScalerChan, error) {
	typeRaw, ok := autoScalerConfig["type"]
	if !ok {
		return nil, errors.New(`"type" field missing in autoscaler config`)
	}
	typeStr, ok := typeRaw.(string)
	if !ok {
		return nil, errors.New(`"type" field must be a string`)
	}

	// Read provision ratio from environment variable (default to 1.0 if missing/invalid)
	provisionRatio := 1.0
	if val := os.Getenv("PROVISION_RATIO"); val != "" {
		if parsed, err := strconv.ParseFloat(val, 64); err == nil && parsed > 0 {
			provisionRatio = parsed
		} else {
			hlog.CtxWarnf(ctx, "Invalid PROVISION_RATIO %q, falling back to default 1.0", val)
		}
	}
	hlog.CtxInfof(ctx, "provision ratio for model %s is set to %f", modelName, provisionRatio)
	var atc Autoscaler
	switch strings.ToUpper(typeStr) {
	case model.DEFAULTSTR:
		atc = &DefaultAutoscaler{
			BaseAutoscaler: BaseAutoscaler{
				InstanceMap:        make(map[string]map[string]time.Time),
				Instancelock:       sync.Mutex{},
				instanceNeededLock: sync.Mutex{},
				config: autoScalerConfig,
				ProvisionRatio: provisionRatio,
			}}
	case model.HPASTR:
		atc = &HPAAutoscaler{
			BaseAutoscaler: BaseAutoscaler{
				InstanceMap:        make(map[string]map[string]time.Time),
				Instancelock:       sync.Mutex{},
				instanceNeededLock: sync.Mutex{},
				config: autoScalerConfig,
				ProvisionRatio: provisionRatio,
			}}
	case model.KPASTR:
		atc = &KPAAutoscaler{
			BaseAutoscaler: BaseAutoscaler{
				InstanceMap:        make(map[string]map[string]time.Time),
				Instancelock:       sync.Mutex{},
				instanceNeededLock: sync.Mutex{},
				config: autoScalerConfig,
				ProvisionRatio: provisionRatio,
			}}
	case model.RPSSTR:
		atc = &RPSAutoscaler{
			BaseAutoscaler: BaseAutoscaler{
				InstanceMap:        make(map[string]map[string]time.Time),
				Instancelock:       sync.Mutex{},
				instanceNeededLock: sync.Mutex{},
				config: autoScalerConfig,
				ProvisionRatio: provisionRatio,
			},
			targetRPS: 1,
		}
	case model.KPAOPTIMIZERSTR:
		atc = &KPAOptimizerAutoscaler{
			BaseAutoscaler: BaseAutoscaler{
				InstanceMap:        make(map[string]map[string]time.Time),
				Instancelock:       sync.Mutex{},
				instanceNeededLock: sync.Mutex{},
				config: autoScalerConfig,
				ProvisionRatio: provisionRatio,
			}}
	case model.CAPSTR:
		// init keys in tps counter map
		// Initialize the map before using

		atc = &CapacityAutoscaler{
			BaseAutoscaler: BaseAutoscaler{
				InstanceMap:        make(map[string]map[string]time.Time),
				Instancelock:       sync.Mutex{},
				instanceNeededLock: sync.Mutex{},
				ScalerCounters: ScalerCounters{
					tpsCounterMap: make(map[string]*float64),
				},
				config: autoScalerConfig,
				ProvisionRatio: provisionRatio,
			}}
			
	default:
		return nil, fmt.Errorf("unsupported autoscaler type: %s", typeStr)
	}
	AutoscalerMap[modelName] = atc

	// Init different types of autoscaler
	return atc.RegisterModel(ctx, modelName, autoScalerConfig)
}


func GetScalerInfoMap(ctx context.Context) map[string]ScalerInfo {
	scalerInfoMap := map[string]ScalerInfo{}
	for modelName, atc := range AutoscalerMap {
		scalerInfoMap[modelName] = atc.GetScalerInfo(ctx)
	}
	return scalerInfoMap
}

func GetAutoscaler(ctx context.Context, modelName string) Autoscaler {
	return AutoscalerMap[modelName]
}
