package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"router/autoscaler"
	"router/kvcclient"
	"router/model"
	"router/node"
	"router/route_policy"
	// "sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
)


type SplitwiseGateway struct {
}

func (r *SplitwiseGateway) preparePrefillRequestCtxInPlace(c *app.RequestContext, decodeInstanceStatus model.InstanceStatus, reqID string) (model.ScalerRequest, error) {
	originalBody := c.Request.Body()
	var partialBody PartialOpenAIRequestBody
	scalerRequest := model.ScalerRequest{RequestType: "prefill", PromptLength: 0}

	if err := json.Unmarshal(originalBody, &partialBody); err != nil {
		return scalerRequest, err
	}
	scalerRequest.PromptLength = partialBody.Prompt.Length()
	scalerRequest.GtDecodeLength = partialBody.MaxTokens

	// Set min_tokens and max_tokens to 1
	partialBody.MinTokens = 1
	partialBody.MaxTokens = 1
	// Set disagg spec
	disaggSpec := map[string]interface{}{
		"req_id":               reqID,
		"receiver_host":        decodeInstanceStatus.HostName,
		"receiver_init_port":   decodeInstanceStatus.Ports.NixlPeerInitPortList,
		"receiver_alloc_port":  decodeInstanceStatus.Ports.NixlPeerAllocPortList,
		"receiver_delete_port": decodeInstanceStatus.Ports.NixlPeerDeletePortList,
		"receiver_uuid":        decodeInstanceStatus.UUID,
	}
	partialBody.KVTransferParams = map[string]interface{}{
		"ret_first_tok": true,
		"disagg_spec":   disaggSpec,
	}

	updatedBody, err := json.Marshal(partialBody)
	if err != nil {
		return scalerRequest, err
	}

	// Set updated body back to the original request context
	c.Request.SetBody(updatedBody)

	return scalerRequest, nil
}

func (r *SplitwiseGateway) prepareDecodeRequestCtxInPlace(c *app.RequestContext) (model.ScalerRequest, error) {
	originalBody := c.Request.Body()
	var partialBody PartialOpenAIRequestBody
	scalerRequest := model.ScalerRequest{RequestType: "decode", PromptLength: 0}

	if err := json.Unmarshal(originalBody, &partialBody); err != nil {
		return scalerRequest, err
	}

	scalerRequest.PromptLength = partialBody.Prompt.Length()
	scalerRequest.GtDecodeLength = partialBody.MaxTokens

	// append the original prompt with a random token
	partialBody.Prompt = AppendRandomToken(partialBody.Prompt)

	// Deduce max_tokens by 1 only when output length > 1, if =1, we simply do nothing (in this case, the output length is actuall 2, we do this to ensure that every request has a decoding phase)
	if partialBody.MaxTokens > 1 {
		partialBody.MaxTokens -= 1
		// Handle min_tokens
		partialBody.MinTokens -= 1
	}

	updatedBody, err := json.Marshal(partialBody)
	if err != nil {
		return scalerRequest, err
	}

	// Set updated body back to the original request context
	c.Request.SetBody(updatedBody)

	return scalerRequest, nil
}

func (r *SplitwiseGateway) Route(ctx context.Context, modelName string, c *app.RequestContext) {
	// check if it's the first request, if it is, set the first request arrival time
	firstRequestArrivalLock.Lock()
	if firstRequestArrivalTime.IsZero() {
		firstRequestArrivalTime = time.Now()
	}
	firstRequestArrivalLock.Unlock()
	// increase request counter by 1
	requestCounterLock.Lock()
	reqID := fmt.Sprintf("%s-%d", modelName, requestCounter)
	requestCounter += 1
	requestCounterLock.Unlock()
	// Announce request arrival, check if scale exists
	hlog.CtxInfof(ctx, "[Splitwise Gateway] req: %s arrived!", reqID)
	prefillModelName := fmt.Sprintf("%s::prefill", modelName)
	decodeModelName := fmt.Sprintf("%s::decode", modelName)
	scalerLock.RLock()
	defer scalerLock.RUnlock()

	decodeScaler, ok := scalerMap[decodeModelName]
	if !ok {
		hlog.CtxInfof(ctx, "model %v not registered", decodeModelName)
		return
	}
	prefillScaler, ok := scalerMap[prefillModelName]
	if !ok {
		hlog.CtxInfof(ctx, "model %v not registered", prefillModelName)
		return
	}

	// first, prepare decode request's data
	// prepare decode request for scaler
	originalReqCtx := c.Copy() // saved for prefill request
	decodeScalerRequest, err := r.prepareDecodeRequestCtxInPlace(c)
	if err != nil {
		hlog.CtxErrorf(ctx, "failed to prepare decode reqeust: %v", err)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	decodeScaler.NewRequest <- decodeScalerRequest
	// check if current tps already exceeds the capacity
	// predictedTTFT := float64(0)

	prefillInstanceNum := prefillScaler.GetInstanceNum() // First, get prefill instance number
	prefillTargetValue := autoscaler.GetScalerInfoMap(ctx)[prefillModelName].TargetValue
	prefillReqCounter := autoscaler.GetScalerInfoMap(ctx)[prefillModelName].ReqCounter
	prefillCapacityExceed := (prefillReqCounter > prefillInstanceNum*prefillTargetValue)
	hlog.CtxInfof(ctx, "[Splitwise Gateway] for request: %s: prefiller instance number: %d, target value: %d, req counter: %d, capacity exceed: %v", reqID, prefillInstanceNum, prefillTargetValue, prefillReqCounter, prefillCapacityExceed)

	decodeInstanceNum := decodeScaler.GetInstanceNum() // First, get prefill instance number
	decodeTargetValue := autoscaler.GetScalerInfoMap(ctx)[decodeModelName].TargetValue
	decodeReqCounter := autoscaler.GetScalerInfoMap(ctx)[decodeModelName].ReqCounter
	decodeCapacityExceed := (decodeReqCounter > decodeInstanceNum*decodeTargetValue)
	hlog.CtxInfof(ctx, "[Splitwise Gateway] for request: %s, decoder instance number: %d, target value: %d, req counter: %d, capacity exceed: %v", reqID, decodeInstanceNum, decodeTargetValue, decodeReqCounter, decodeCapacityExceed)
	capacityExceed := prefillCapacityExceed || decodeCapacityExceed
	passedFirstMinute := time.Since(firstRequestArrivalTime) >= 60*time.Second // only skip prefill after the first minute (when traffic is stable)
	skipPrefill := capacityExceed && passedFirstMinute                         // only skip prefill, when prefill capacity exceeds, and route_policy decides to route it to a convertiable decoder

	// prepare decode request for router
	decodeRouterRequest := model.RouterRequest{ReqID: reqID, CapacityExceed: capacityExceed, IsPrefill: false}
	var decodeRouterReply model.RouterReply
	if skipPrefill {
		decodeRouterReply = route_policy.MustRoute(ctx, modelName, decodeRouterRequest)
	} else {
		decodeRouterReply = route_policy.MustRoute(ctx, decodeModelName, decodeRouterRequest)
	}
	decodeInstanceUUID := decodeRouterReply.InstanceUUID
	hlog.CtxInfof(ctx, "[Splitwise Gateway] req %s is routed to decode instance %s", reqID, decodeInstanceUUID)
	// defer clean up function for decode
	clearRequest := func() {
		// after prefill kv done, clear the route_policy request counter
		hlog.CtxInfof(ctx, "[Splitwise Gateway] req %s finished on decode instance: %s", reqID, decodeInstanceUUID)
		route_policy.FininshedRequest(ctx, decodeModelName, decodeInstanceUUID, decodeRouterRequest)
		decodeScaler.FinishedRequest <- decodeScalerRequest
	}
	defer clearRequest()

	// router metrics
	routerTimestamps := make(map[string]interface{})
	routerTimestamps["req_id"] = reqID
	routerTimestamps["request_arrival"] = float64(time.Now().UnixNano()) / 1e9

	// decode instance status, it contains whether the decode is a convertiable decoder
	decodeInstanceStatus, err := node.GetInstanceInfo(ctx, decodeInstanceUUID)
	if err != nil {
		hlog.CtxErrorf(ctx, "get decode instance status failed, err: %v", err)
		return
	}

	prefillCtx := &app.RequestContext{}
	if skipPrefill {
		// It's a convertiable decoder and prefill capacity already exceeds, simply skip prefilling, send to decode instance, complete prefill+decode there
		hlog.CtxInfof(ctx, "[Splitwise Gateway] req %s is routed to mixed pool %s to do prefill", reqID, decodeInstanceUUID)
		routerTimestamps["pd_same_node"] = float64(1)
		routerTimestamps["prefillFinished"] = -1 // assign negative values, better for data processing
		prefillScalerRequest, err := r.preparePrefillRequestCtxInPlace(originalReqCtx, decodeInstanceStatus, reqID)
		if err != nil {
			hlog.CtxErrorf(ctx, "[Splitwise Gateway] failed to prepare prefill ctx, err: %v", err)
			return
		}
		prefillScaler.NewRequest <- prefillScalerRequest
		defer func() {
			prefillScaler.FinishedRequest <- prefillScalerRequest
		}()
	} else {
		// perform prefill and wait for kv ready
		prefillCtx, err = r.handlePrefill(ctx, prefillModelName, reqID, decodeInstanceStatus, routerTimestamps, originalReqCtx)
		if err != nil {
			hlog.CtxErrorf(ctx, "[Splitwise Gateway] failed to do prefill for request %s, err: %v", reqID, err)
			return
		}
	}
	routerTimestamps["prefill_kv_ready"] = float64(time.Now().UnixNano()) / 1e9

	hlog.CtxInfof(ctx, "[Splitwise Gateway] req %s is sent to decode instance %s!", reqID, decodeInstanceUUID)
	// get prefiller and decoder's reverse proxy
	decodeProxy, err := node.GetInstanceReverseProxy(ctx, decodeInstanceUUID)
	if err != nil {
		hlog.CtxErrorf(ctx, "get decode proxy failed, err: %v", err)
		return
	}
	decodeProxy.ServeHTTP(ctx, c)
	if errVal, ok := c.Get("proxy_error"); ok {
		err := errVal.(error)
		hlog.CtxErrorf(ctx, "[Splitwise Gateway] failed to perform decode request: %s, err: %v", reqID, err)
		// If decode request failed, send delete kvc request
		kvcclient.SendDeleteKV(decodeInstanceStatus.HostName, decodeInstanceStatus.Ports.NixlPeerDeletePortList, reqID, 2*time.Second)
		return
	} else {
		hlog.CtxInfof(ctx, "[Splitwise Gateway] req %s finished decode on decode instance: %s", reqID, decodeInstanceUUID)
	}
	routerTimestamps["decodeFinished"] = float64(time.Now().UnixNano()) / 1e9
	// prefillResponse := <- prefillResponseChannel
	if !skipPrefill {
		err = mergeResponse(c, &prefillCtx.Response, routerTimestamps)
		if err != nil {
			hlog.CtxErrorf(ctx, "[Splitwise Gateway] Error in merging response: %v", err)
			return
		}
	}

}

// This function performs prefill and ensures kv ready on decode
func (r *SplitwiseGateway) handlePrefill(
	ctx context.Context, modelName string, reqID string,
	decodeInstanceStatus model.InstanceStatus,
	routerTimestamps map[string]interface{},
	c *app.RequestContext) (*app.RequestContext, error) {
	prefillCtx := c.Copy()
	// route to prefill
	prefillScalerRequest, err := r.preparePrefillRequestCtxInPlace(prefillCtx, decodeInstanceStatus, reqID)
	if err != nil {
		return prefillCtx, fmt.Errorf("failed to prepare prefill ctx, err: %v", err)
	}
	prefillRouterRequest := model.RouterRequest{ReqID: reqID, CapacityExceed: false, IsPrefill: true, NumPrefillTokens: prefillScalerRequest.PromptLength}
	prefillRouterReply := route_policy.MustRoute(ctx, modelName, prefillRouterRequest)

	instanceUUID := prefillRouterReply.InstanceUUID
	hlog.CtxInfof(ctx, "[Splitwise Gateway] req %s is routed to prefill instance %s", reqID, instanceUUID)
	// register clear function for prefill router
	clearRequestOnRouter := func() {
		// after prefill kv done, clear the route_policy request counter
		hlog.CtxInfof(ctx, "[Splitwise Gateway] req %s finished on prefill instance: %s", reqID, instanceUUID)
		route_policy.FininshedRequest(ctx, modelName, instanceUUID, prefillRouterRequest)
	}
	defer clearRequestOnRouter()
	prefillInstanceStatus, err := node.GetInstanceInfo(ctx, instanceUUID)
	if err != nil {
		return prefillCtx, fmt.Errorf("get prefill instance status fail! err: %v", err)
	}
	// check if perfiller and decoder are on the same node
	pdSameNode := 1
	if prefillInstanceStatus.HostName != decodeInstanceStatus.HostName {
		hlog.CtxInfof(ctx, "[Splitwise Gateway] req %s prefill and decode are not on the same node", reqID)
		pdSameNode = 0
	} else {
		hlog.CtxInfof(ctx, "[Splitwise Gateway] req %s prefill and decode are on the same node", reqID)
	}
	routerTimestamps["pd_same_node"] = float64(pdSameNode)

	// get prefill scaler
	prefillScaler, ok := scalerMap[modelName]
	if !ok {
		return prefillCtx, fmt.Errorf("model %v not registered", modelName)
	}
	// annouce scaler requst arrive
	prefillScaler.NewRequest <- prefillScalerRequest
	hlog.CtxInfof(ctx, "[Splitwise Gateway] req %s is sent to prefill instance %v!", reqID, instanceUUID)
	prefillProxy, err := node.GetInstanceReverseProxy(ctx, instanceUUID)
	if err != nil {
		return prefillCtx, fmt.Errorf("get prefill proxy failed, err: %v", err)
	}
	routerTimestamps["prefillStarted"] = float64(time.Now().UnixNano()) / 1e9
	prefillProxy.ServeHTTP(ctx, prefillCtx)
	routerTimestamps["prefillFinished"] = float64(time.Now().UnixNano()) / 1e9
	// annouce scaler request finishes
	prefillScaler.FinishedRequest <- prefillScalerRequest
	// after prefill is sent to instance, decoder might have memory allocated, need to clear it
	// Check if prefill succeeds, if not, simply abort this request
	if errVal, ok := prefillCtx.Get("proxy_error"); ok {
		// If prefill request failed, simply set the status code and resposne body, then return
		err := errVal.(error)
		kvcclient.SendDeleteKV(decodeInstanceStatus.HostName, decodeInstanceStatus.Ports.NixlPeerDeletePortList, reqID, 2*time.Second)
		return prefillCtx, fmt.Errorf("failed to perform prefill request: %s, err: %v", reqID, err)
	}
	hlog.CtxInfof(ctx, "[Splitwise Gateway] req %s finished prefill on prefill instance %s!", reqID, instanceUUID)
	// wait for prefill kv to be ready
	hlog.CtxInfof(ctx, "[Splitwise Gateway] req %s is waiting for kv to be ready on decode instance %s...", reqID, decodeInstanceStatus.UUID)
	ok = kvcclient.WaitForKVReady(ctx, reqID, 300*time.Second, len(prefillInstanceStatus.GPUIDs))
	if !ok {
		kvcclient.SendDeleteKV(decodeInstanceStatus.HostName, decodeInstanceStatus.Ports.NixlPeerDeletePortList, reqID, 2*time.Second)
		return prefillCtx, fmt.Errorf("failed to wait for kv ready request: %s, err: %v", reqID, err)
	}
	hlog.CtxInfof(ctx, "[Splitwise Gateway] req %s kv ready on decode instance! (ok=%v)", reqID, ok, decodeInstanceStatus.UUID)
	return prefillCtx, nil
}
