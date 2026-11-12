package metrics

import (
	"net/http"

	"router/detect"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	registry = prometheus.NewRegistry()
)

func Register(metrics ...prometheus.Collector) {
	registry.MustRegister(metrics...)
}

func Start() {
	go func() {
		handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{
			EnableOpenMetrics: false,
		})
		http.Handle("/metrics", handler)
		http.ListenAndServe(detect.GetMetricsPort(), nil)
	}()
}
