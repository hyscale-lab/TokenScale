package handler

import (
	"context"
	"fmt"
	"strconv"

	"router/autoscaler"
	"router/gateway"
	"router/model"
	"router/node"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

func CreateInstanceTest(ctx context.Context, c *app.RequestContext) {
	var request model.AllocateRequest
	if err := c.BindJSON(&request); err != nil {
		hlog.CtxErrorf(ctx, "bind request failed: %v", err)
		c.JSON(400, utils.H{"error": err.Error()})
		return
	}
	modelName := c.Query("model")
	// if err := node.Allocate(ctx, request, modelName); err != nil {
	// 	hlog.CtxErrorf(ctx, "create instance failed: %v", err)
	// 	c.JSON(400, utils.H{"error": err.Error()})
	// 	return
	// }
	scalerChan := gateway.GetScalerChan(ctx, modelName)
	scalerChan.ScaleUpDecision <- request

	// node.OnAllocateSuccess(ctx, request.InstanceUUID, modelName)
	c.JSON(200, utils.H{"message": "create instance success"})
}

func DeleteInstanceTest(ctx context.Context, c *app.RequestContext) {
	modelName := c.Query("model")
	instanceID := c.Query("instance")
	if err := node.Deallocate(ctx, instanceID); err != nil {
		hlog.CtxErrorf(ctx, "delete instance failed: %v", err)
		c.JSON(400, utils.H{"error": err.Error()})
		return
	}
	scalerChan := gateway.GetScalerChan(ctx, modelName)
	scalerChan.ScaleDownDecision <- instanceID
	c.JSON(200, utils.H{"message": "delete instance success"})
}

func SetInstanceNeededTest(ctx context.Context, c*app.RequestContext) {
	modelName := c.Query("model")
	instanceNeededStr := c.Query("instance_needed")

    instanceNeeded, err := strconv.Atoi(instanceNeededStr)
    if err != nil {
        hlog.CtxErrorf(ctx, "invalid instance_needed value: %v", err)
        c.JSON(400, map[string]string{"error": "invalid instance_needed value"})
        return
    }
	hlog.CtxInfof(ctx, "Received set instance needed request for model: %s, instanceNeeded: %d", modelName, instanceNeeded)
	scaler := autoscaler.GetAutoscaler(ctx, modelName)	
	scaler.UpdateInstanceNeeded(int64(instanceNeeded))
	c.JSON(200, utils.H{"message": fmt.Sprintf("set instance need to %d", instanceNeeded)})
	
}
