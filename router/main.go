package main

import (
	"context"

	"router/detect"
	"router/handler"
	"router/kvcclient"
	"router/metrics"
	_ "router/servicediscovery"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func main() {
	metrics.Start()
	kvcclient.MaybeStartZmqPullServer()
	hp := detect.GetHostPort()
	h := server.Default(server.WithHostPorts(hp))

	h.GET("/ping", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, utils.H{"message": "pong"})
	})
	h.POST("/register_node", handler.RegisterNode)
	h.POST("/register_model", handler.RegisterModel)
	h.NoRoute(handler.RequestForward)
	h.POST("/get_router_info", handler.GetRouterInfo)
	// Test APIs, not used in production
	h.POST("/create_instance_test", handler.CreateInstanceTest)
	h.POST("/delete_instance_test", handler.DeleteInstanceTest)
	h.POST("/set_instance_needed_test", handler.SetInstanceNeededTest)

	h.Spin()
}
