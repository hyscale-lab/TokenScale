package gateway

import (
	"context"
	"encoding/json"
	"os"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol"
)

func mergeResponse(c *app.RequestContext, prefillResp *protocol.Response, routerTimestamps map[string]interface{}) error {
	var bodyMap map[string]interface{}
	if err := json.Unmarshal(c.Response.Body(), &bodyMap); err != nil {
		return err
	}

	var prefillBodyMap map[string]interface{}
	if err := json.Unmarshal(prefillResp.Body(), &prefillBodyMap); err != nil {
		return err
	}
	// in case prefill reqeust failed, there will be no metrics_list in the prefillBodyMap
	if metricsList, ok := prefillBodyMap["metrics_list"]; ok {
		bodyMap["prefill_metrics_list"] = metricsList
	}

	bodyMap["router_timestamps"] = routerTimestamps

	updatedBody, err := json.Marshal(bodyMap)
	if err != nil {
		return err
	}
	c.Response.SetBody(updatedBody)
	return nil
}

type Router interface {
	Route(ctx context.Context, modelName string, c *app.RequestContext)
}

func getWSZMQURL() string {
	url := os.Getenv("WS_ZMQ_URL")
	if url == "" {
		url = "ws://0.0.0.0:60000/ws"
		os.Setenv("WS_ZMQ_URL", url) // Optional: set it for subprocesses
	}
	return url
}

func getGateway() Router {
	switch os.Getenv("PD_MODE") {
	case "mooncake":
		return &MooncakeGateway{}
	case "lmcache":
		return &TokenpipeGateway{}
	default:
		return &DefaultGateway{}
	}
}
