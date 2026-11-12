package handler

import (
	"context"
	"time"

	"router/gateway"
	"router/metrics"
	"router/model"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	nodesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nodes_total",
		Help: "The total number of nodes",
	})

	requestDurationSummary = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "request_duration_seconds",
			Help:       "Summary of request duration in seconds",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"model"},
	)
)

func init() {
	metrics.Register(nodesTotal, requestDurationSummary)
}

func RegisterNode(ctx context.Context, c *app.RequestContext) {
	var request model.RegisterNode
	if err := c.BindJSON(&request); err != nil {
		hlog.CtxErrorf(ctx, "bind request failed: %v", err)
		c.JSON(400, utils.H{"error": err.Error()})
		return
	}
	hlog.CtxInfof(ctx, "register node: %v", request)
	gateway.RegisterNode(ctx, request)
	nodesTotal.Inc()

	c.JSON(200, utils.H{"message": "register node success"})
}

func RegisterModel(ctx context.Context, c *app.RequestContext) {
	var request model.RegisterModel
	if err := c.BindJSON(&request); err != nil {
		hlog.CtxErrorf(ctx, "bind request failed: %v", err)
		c.JSON(400, utils.H{"error": err.Error()})
		return
	}
	hlog.CtxInfof(ctx, "register model: %v", request)
	if err := gateway.RegisterModel(ctx, request); err != nil {
		hlog.CtxErrorf(ctx, "register model failed: %v", err)
		c.JSON(400, utils.H{"error": err.Error()})
		return
	}
	c.JSON(200, utils.H{"message": "register model success"})
}

func RequestForward(ctx context.Context, c *app.RequestContext) {
	var partOfBody struct {
		ModelName string `json:"model"`
	}
	if err := c.BindJSON(&partOfBody); err != nil {
		c.JSON(400, utils.H{"error": err.Error()})
		return
	}
	startTime := time.Now()
	gateway.Route(ctx, partOfBody.ModelName, c)
	duration := time.Since(startTime)
	requestDurationSummary.WithLabelValues(partOfBody.ModelName).Observe(duration.Seconds())
}

func GetRouterInfo(ctx context.Context, c *app.RequestContext) {
	_, err := c.Body()
	if err != nil {
		c.JSON(400, utils.H{"error": err.Error()})
		return
	}
	routerInfo := gateway.GetRouterInfo(ctx)
	c.JSON(200, routerInfo)
}
