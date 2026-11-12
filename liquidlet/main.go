package main

import (
	"context"

	"liquidlet/handler"
	"liquidlet/register"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func main() {
	hp := register.GetHostPort()
	h := server.Default(server.WithHostPorts(hp))

	h.GET("/ping", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, utils.H{"message": "pong"})
	})
	h.POST("/create", handler.CreateInstance)
	h.POST("/delete", handler.DeleteInstance)
	h.POST("/preload", handler.PreloadModel)
	h.POST("/create_file", handler.CreateFile)

	go register.Register()

	h.Spin()
}
