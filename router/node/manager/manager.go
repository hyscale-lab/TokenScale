package manager

import (
	"context"
	"os"

	"router/metrics"
	"router/model"
	"router/node/traffic_monitor"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	createInstanceDurationSummary = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "router:create_instance_duration_seconds",
			Help:       "Summary of create instance duration in seconds",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"model", "tp"},
	)
)

func init() {
	metrics.Register(createInstanceDurationSummary)
}

type Manager interface {
	RegisterModel(ctx context.Context, req model.RegisterModel) error
	CheckModel(ctx context.Context, modelName string) bool
	CreateInstance(ctx context.Context, uuid string, modelName string, host string, ports model.LMCachePorts, gpus []string, registerURI string, createFileURI string, isConvert bool, maxRequest int64) error
	StopInstance(ctx context.Context, uuid string, registerURI string) error
	GetInstanceInfo(ctx context.Context, uuid string) (model.InstanceStatus, error)
	GetInstanceReverseProxy(ctx context.Context, uuid string) (model.ReverseProxy, error)
	InitBackground()
	GetRegisteredModels(ctx context.Context) []string
	GetTrafficMonitor(ctx context.Context, modelName string) (*traffic_monitor.TrafficMonitor, error)
}

func New() Manager {
	manager := os.Getenv("MANAGER")
	var mgr Manager
	switch manager {
	default:
		mgr = &DefaultManager{}
	}
	mgr.InitBackground()
	return mgr
}
