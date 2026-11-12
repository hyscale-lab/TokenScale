package gateway

import (
	"context"
	"encoding/json"
	"router/model"
	"router/node"
	"router/route_policy"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
)

type DefaultGateway struct{}

func GetRequestInfo(c *app.RequestContext) model.ScalerRequest {
	originalBody := c.Request.Body()
	var bodyMap map[string]interface{}
	scalerRequestInfo := model.ScalerRequest{RequestType: "default", PromptLength: 0}

	if err := json.Unmarshal(originalBody, &bodyMap); err != nil {
		return scalerRequestInfo
	}
	var promptLength int
	switch v := bodyMap["prompt"].(type) {
	case string:
		promptLength = 0
	case []interface{}:
		promptLength = len(v)
	}
	scalerRequestInfo.PromptLength = promptLength
	return scalerRequestInfo
}

func (r *DefaultGateway) Route(ctx context.Context, modelName string, c *app.RequestContext) {
	scalerLock.RLock()
	defer scalerLock.RUnlock()
	scaler, ok := scalerMap[modelName]
	if !ok {
		hlog.CtxInfof(ctx, "model %v not registered", modelName)
		return
	}
	scalerRequestInfo := GetRequestInfo(c)
	scaler.NewRequest <- scalerRequestInfo
	var instanceUUID string
	routeReply, err := route_policy.RouteDecision(ctx, modelName, model.RouterRequest{})
	routeInstance := routeReply.InstanceUUID
	if err == nil {
		instanceUUID = routeInstance
	} else {
		// if fail, retry every 1 second
		hlog.CtxInfof(ctx, "route failed: %v", err)
		ticker := time.NewTicker(1 * time.Second)
		for range ticker.C {
			routeReply, err := route_policy.RouteDecision(ctx, modelName, model.RouterRequest{})
			routeInstance = routeReply.InstanceUUID
			if err != nil {
				hlog.CtxInfof(ctx, "route failed: %v", err)
				continue
			}
			instanceUUID = routeInstance
			break
		}
	}
	proxy, err := node.GetInstanceReverseProxy(ctx, instanceUUID)
	if err != nil {
		hlog.CtxErrorf(ctx, "get proxy failed: %v", err)
		return
	}
	proxy.ServeHTTP(ctx, c)
	route_policy.FininshedRequest(ctx, modelName, instanceUUID, model.RouterRequest{})
	scaler.FinishedRequest <- scalerRequestInfo
}
