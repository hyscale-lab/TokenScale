package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"router/model"
	"router/node"
	"router/route_policy"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
)

type MooncakeGateway struct{}

func (r *MooncakeGateway) preparePrefillRequestCtxInPlace(c *app.RequestContext) (model.ScalerRequest, error) {
	originalBody := c.Request.Body()
	var bodyMap map[string]interface{}
	scalerRequestInfo := model.ScalerRequest{RequestType: "decode", PromptLength: 0}

	if err := json.Unmarshal(originalBody, &bodyMap); err != nil {
		return scalerRequestInfo, err
	}

	var promptLength int
	switch v := bodyMap["prompt"].(type) {
	case string:
		promptLength = 0
	case []interface{}:
		promptLength = len(v) + 1
	}
	scalerRequestInfo.PromptLength = promptLength

	// Set min_tokens and max_tokens to 1
	bodyMap["min_tokens"] = 1
	bodyMap["max_tokens"] = 1

	updatedBody, err := json.Marshal(bodyMap)
	if err != nil {
		return scalerRequestInfo, err
	}

	// Set updated body back to the original request context
	c.Request.SetBody(updatedBody)

	return scalerRequestInfo, nil
}

func (r *MooncakeGateway) Route(ctx context.Context, modelName string, c *app.RequestContext) {
	// Announce request arrival to two scalers
	prefillModelName := fmt.Sprintf("%s::prefill", modelName)
	decodeModelName := fmt.Sprintf("%s::decode", modelName)
	scalerLock.RLock()
	defer scalerLock.RUnlock()
	prefillScaler, ok := scalerMap[prefillModelName]
	if !ok {
		hlog.CtxInfof(ctx, "model %v not registered", prefillModelName)
		return
	}

	decodeScaler, ok := scalerMap[decodeModelName]
	if !ok {
		hlog.CtxInfof(ctx, "model %v not registered", decodeModelName)
		return
	}

	var prefillInstanceUUID string
	var decodeInstanceUUID string
	// First, try to route request to prefill instances
	routeReply, err := route_policy.RouteDecision(ctx, prefillModelName, model.RouterRequest{})
	routeInstance := routeReply.InstanceUUID
	if err == nil {
		prefillInstanceUUID = routeInstance
	} else {
		hlog.CtxInfof(ctx, "route failed: %v", err)
		ticker := time.NewTicker(1 * time.Second)
		for range ticker.C {
			routeReply, err := route_policy.RouteDecision(ctx, prefillModelName, model.RouterRequest{})
			routeInstance = routeReply.InstanceUUID
			if err != nil {
				hlog.CtxInfof(ctx, "route failed: %v", err)
				continue
			}
			prefillInstanceUUID = routeInstance
			break
		}
	}
	// Then, try to route request to decode instances
	routeReply, err = route_policy.RouteDecision(ctx, decodeModelName, model.RouterRequest{})
	routeInstance = routeReply.InstanceUUID
	if err == nil {
		decodeInstanceUUID = routeInstance
	} else {
		hlog.CtxInfof(ctx, "route failed: %v", err)
		ticker := time.NewTicker(1 * time.Second)
		for range ticker.C {
			routeReply, err := route_policy.RouteDecision(ctx, decodeModelName, model.RouterRequest{})
			routeInstance = routeReply.InstanceUUID
			if err != nil {
				hlog.CtxInfof(ctx, "route failed: %v", err)
				continue
			}
			decodeInstanceUUID = routeInstance
			break
		}
	}

	// pd disagregation serving
	prefillProxy, err := node.GetInstanceReverseProxy(ctx, prefillInstanceUUID)
	if err != nil {
		hlog.CtxErrorf(ctx, "get prefill proxy failed: %v", err)
		return
	}
	decodeProxy, err := node.GetInstanceReverseProxy(ctx, decodeInstanceUUID)
	if err != nil {
		hlog.CtxErrorf(ctx, "get decode proxy failed: %v", err)
		return
	}

	prefillCtx := c.Copy()
	prefillScalerRequestInfo, err := r.preparePrefillRequestCtxInPlace(prefillCtx)
	if err != nil {
		hlog.CtxErrorf(ctx, "failed to prepare prefill ctx: %v", err)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	prefillScaler.NewRequest <- prefillScalerRequestInfo

	requestArrival := float64(time.Now().UnixNano()) / 1e9
	prefillFinished := float64(0)
	decodeFinished := float64(0)

	// after prefill request finished, annouced prefill scaler and router
	prefillProxy.ServeHTTP(ctx, prefillCtx)
	prefillScaler.FinishedRequest <- prefillScalerRequestInfo
	route_policy.FininshedRequest(ctx, prefillModelName, prefillInstanceUUID, model.RouterRequest{})
	// Check if prefill succeeds, if not, simply abort this request
	if prefillCtx.Response.StatusCode() < 200 || prefillCtx.Response.StatusCode() > 300 {
		// If prefill request failed, simply set the status code and resposne body, then return
		c.Response.SetStatusCode(prefillCtx.Response.StatusCode())
		c.Response.SetBody(prefillCtx.Response.Body())
		return
	}

	prefillFinished = float64(time.Now().UnixNano()) / 1e9

	if prefillCtx.Response.StatusCode() != 200 {
		hlog.CtxErrorf(ctx, "Failed to perform prefill request, error: %s", string(prefillCtx.Response.Body()))
	}

	// Wait for prefill response to finish, then execute decode. This can be optimized
	decodeScalerRequestInfo := prefillScalerRequestInfo
	decodeScalerRequestInfo.RequestType = "decode"
	decodeScalerRequestInfo.PromptLength = decodeScalerRequestInfo.PromptLength + 1
	decodeScaler.NewRequest <- decodeScalerRequestInfo
	decodeProxy.ServeHTTP(ctx, c)
	decodeFinished = float64(time.Now().UnixNano()) / 1e9
	// prefillResponse := <- prefillResponseChannel
	routerTimestamps := map[string]interface{}{
		"request_arrival": requestArrival,
		"prefillFinished": prefillFinished,
		"decodeFinished":  decodeFinished,
	}
	err = mergeResponse(c, &prefillCtx.Response, routerTimestamps)
	if err != nil {
		hlog.CtxErrorf(ctx, "Error in merging response: %v", err)
	}

	route_policy.FininshedRequest(ctx, decodeModelName, decodeInstanceUUID, model.RouterRequest{})
	decodeScaler.FinishedRequest <- decodeScalerRequestInfo
}
