package manager

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"router/model"
	"router/node/traffic_monitor"
	"router/tool"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/client"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/hertz-contrib/reverseproxy"
)

type DefaultManager struct {
	modelConfig              map[string]model.RegisterModel
	modelConfigLock          sync.RWMutex
	instanceStatus           map[string]model.InstanceStatus
	instanceStatusLock       sync.RWMutex
	instanceReverseProxy     map[string]model.ReverseProxy
	instanceReverseProxyLock sync.RWMutex
	instanceMonitorChannel   chan model.InstanceStatus
	trafficMonitors          map[string]*traffic_monitor.TrafficMonitor
	trafficMonitorsLock      sync.RWMutex
}

func (m *DefaultManager) InitBackground() {
	m.modelConfig = make(map[string]model.RegisterModel)
	m.instanceStatus = make(map[string]model.InstanceStatus)
	m.instanceStatusLock = sync.RWMutex{}
	m.instanceReverseProxy = make(map[string]model.ReverseProxy)
	m.instanceReverseProxyLock = sync.RWMutex{}
	m.modelConfigLock = sync.RWMutex{}
	m.instanceMonitorChannel = make(chan model.InstanceStatus, 100)
	m.trafficMonitors = make(map[string]*traffic_monitor.TrafficMonitor)
	m.trafficMonitorsLock = sync.RWMutex{}
	go m.instanceInfoMonitor(context.Background())
}

func (m *DefaultManager) instanceInfoMonitor(ctx context.Context) {
	for instanceStatus := range m.instanceMonitorChannel {
		hlog.CtxInfof(ctx, "Start Monitor Instance: %v", instanceStatus)
		go m.monitorInstance(ctx, instanceStatus)
	}
}

func (m *DefaultManager) GetInstanceInfo(ctx context.Context, uuid string) (model.InstanceStatus, error) {
	m.instanceStatusLock.RLock()
	defer m.instanceStatusLock.RUnlock()
	instanceStatus, ok := m.instanceStatus[uuid]
	if !ok {
		hlog.CtxInfof(ctx, "instance %v not found", uuid)
		return model.InstanceStatus{}, fmt.Errorf("instance %v not found", uuid)
	}
	return instanceStatus, nil
}

func (m *DefaultManager) GetInstanceReverseProxy(ctx context.Context, uuid string) (model.ReverseProxy, error) {
	m.instanceReverseProxyLock.RLock()
	defer m.instanceReverseProxyLock.RUnlock()
	instanceReverseProxy, ok := m.instanceReverseProxy[uuid]
	if !ok {
		hlog.CtxInfof(ctx, "instance %v reverse proxy not found", uuid)
		return nil, fmt.Errorf("instance %v reverse proxy %v not found", uuid)
	}
	return instanceReverseProxy, nil
}

func (m *DefaultManager) GetTrafficMonitor(ctx context.Context, modelName string) (*traffic_monitor.TrafficMonitor, error) {
	m.trafficMonitorsLock.RLock()
	defer m.trafficMonitorsLock.RUnlock()
	tm, ok := m.trafficMonitors[modelName]
	if !ok {
		return nil, fmt.Errorf("traffic monitor for model %s not found", modelName)
	}
	return tm, nil
}

func extractDisaggInfo(body map[string]any) (reqID, receiverHost string, receiverDeletePort int) {
	kv, ok := body["kv_transfer_params"].(map[string]any)
	if !ok {
		return
	}
	disagg, ok := kv["disagg_spec"].(map[string]any)
	if !ok {
		return
	}
	reqID, _ = disagg["req_id"].(string)
	receiverHost, _ = disagg["receiver_host"].(string)
	if v, ok := disagg["receiver_delete_port"].(float64); ok {
		receiverDeletePort = int(v)
	}
	return
}

func (m *DefaultManager) monitorInstance(ctx context.Context, instanceStatus model.InstanceStatus) {
	hlog.CtxInfof(ctx, "Start Monitor Instance: %v", instanceStatus)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if tool.VllmServiceDetect(ctx, instanceStatus.HostName, instanceStatus.Port) {
			hlog.CtxInfof(ctx, "Instance %v is running", instanceStatus.UUID)
			break
		}
		hlog.CtxInfof(ctx, "Instance %v is starting, waiting...", instanceStatus.UUID)
	}
	m.instanceStatusLock.Lock()
	defer m.instanceStatusLock.Unlock()
	m.instanceStatus[instanceStatus.UUID] = instanceStatus
	instanceStatus.Waiting <- struct{}{}

	// create instance reverse proxy
	m.instanceReverseProxyLock.Lock()
	defer m.instanceReverseProxyLock.Unlock()
	cri := fmt.Sprintf("http://%s:%d", instanceStatus.HostName, instanceStatus.Port)
	proxy, err := reverseproxy.NewSingleHostReverseProxy(cri, client.WithMaxConnsPerHost(2048), client.WithClientReadTimeout(time.Duration(instanceStatus.Timeout)*time.Second))
	if err != nil {
		hlog.CtxErrorf(ctx, "create proxy failed: %v", err)
		return
	}
	proxy.SetModifyResponse(func(rsp *protocol.Response) error {
		tm, err := m.GetTrafficMonitor(ctx, instanceStatus.ModelName)
		if err != nil {
			hlog.Errorf("traffic monitor not found for model %s: %v", instanceStatus.ModelName, err)
			return nil
		}
		tm.RawRecord(ctx, instanceStatus.ModelName, rsp)
		return nil
	})
	proxyErrorHandler := func(c *app.RequestContext, err error) {
		// just record metadata
		c.Set("proxy_error", err)

		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() ||
			errors.Is(err, context.DeadlineExceeded) ||
			strings.Contains(strings.ToLower(err.Error()), "timeout") {
			c.Set("proxy_error_type", "timeout")
		} else {
			c.Set("proxy_error_type", "upstream_error")
		}

		c.Response.SetStatusCode(http.StatusGatewayTimeout)
		c.Response.SetBodyString(fmt.Sprintf("prefill upstream error: %v", err))
	}

	proxy.SetErrorHandler(proxyErrorHandler)

	m.instanceReverseProxy[instanceStatus.UUID] = proxy
}

func (m *DefaultManager) RegisterModel(ctx context.Context, req model.RegisterModel) error {
	if req.TPArg == "" {
		req.TPArg = "--tensor-parallel-size"
	}
	if req.PortArg == "" {
		req.PortArg = "--port"
	}
	if req.HostArg == "" {
		req.HostArg = "--host"
	}
	// if req.RouteAPI == "" {
	// 	req.RouteAPI = "/v1/completions"
	// }
	m.modelConfigLock.Lock()
	defer m.modelConfigLock.Unlock()
	if _, ok := m.modelConfig[req.ModelName]; ok {
		hlog.CtxInfof(ctx, "model %v already registered", req.ModelName)
		return fmt.Errorf("model %v already registered", req.ModelName)
	}
	m.modelConfig[req.ModelName] = req
	if req.TrafficMonitor {
		m.trafficMonitorsLock.Lock()
		defer m.trafficMonitorsLock.Unlock()
		if _, ok := m.trafficMonitors[req.ModelName]; !ok {
			redisAddr := os.Getenv("REDIS_ADDR")
			if redisAddr == "" {
				redisAddr = "localhost:6379"
			}
			redisPassword := os.Getenv("REDIS_PASSWORD")
			m.trafficMonitors[req.ModelName] = traffic_monitor.NewTrafficMonitor(ctx, req.ModelName, redisAddr, redisPassword)
			hlog.CtxInfof(ctx, "Traffic monitor created for model %s", req.ModelName)
		}
	}
	return nil
}

func (m *DefaultManager) CheckModel(ctx context.Context, modelName string) bool {
	m.modelConfigLock.RLock()
	defer m.modelConfigLock.RUnlock()
	_, ok := m.modelConfig[modelName]
	return ok
}

func (m *DefaultManager) getModelConfig(ctx context.Context, modelName string) (model.RegisterModel, error) {
	m.modelConfigLock.RLock()
	defer m.modelConfigLock.RUnlock()
	modelConfig, ok := m.modelConfig[modelName]
	if !ok {
		hlog.CtxInfof(ctx, "model %v not registered", modelName)
		return model.RegisterModel{}, fmt.Errorf("model %v not registered", modelName)
	}
	return modelConfig, nil
}

func (m *DefaultManager) createInstanceSend(ctx context.Context, cliReq model.CliReq, registerURI string) error {
	hlog.CtxInfof(ctx, "send create instance request: %v to URI %v", cliReq, registerURI)
	return tool.Post(ctx, cliReq, registerURI)
}

func (m *DefaultManager) CreateInstance(ctx context.Context, uuid string, modelName string, host string, ports model.LMCachePorts, gpus []string, registerURI string, createFileURI string, isConvert bool, maxRequest int64) error {
	config, err := m.getModelConfig(ctx, modelName)
	if err != nil {
		hlog.CtxInfof(ctx, "get model config failed: %v", err)
		return err
	}
	_, modelType, err := model.GetModelNameAndType(config.ModelName)
	if err != nil {
		hlog.CtxErrorf(ctx, "invalid modelName %s", modelName)
		return err
	}
	pdMode := os.Getenv("PD_MODE")
	extraEnvs := []string{}
	extraParams := []string{}
	if pdMode == "lmcache" {
		extraEnvs, extraParams, err = constructLmcacheExtraParamsAndEnvs(config, uuid, ports, createFileURI)
		if err != nil {
			hlog.CtxErrorf(ctx, "failed to construct lmcache envs and params! %v", err)
			return err
		}
	}
	cliReq, err := constructCliReq(config, uuid, host, ports.ServePort, gpus, extraEnvs, extraParams)
	if err != nil {
		hlog.CtxInfof(ctx, "construct cli request failed: %v", err)
		return err
	}
	if err := m.createInstanceSend(ctx, cliReq, registerURI); err != nil {
		hlog.CtxInfof(ctx, "send create instance request failed: %v", err)
		return err
	}
	ServingMode := os.Getenv("SERVING_MODE")
	timeout := 0
	if ServingMode == "offline" {
		timeout = 3600
	} else {
		timeout = 120 // default 120s timeout value
		if modelType == "prefill" {
			timeout = 10 // prefill request shoud be finished within a short time
		}

	}

	startTime := time.Now()
	waitingChan := make(chan struct{})
	instanceInfo := model.InstanceStatus{
		UUID:      uuid,
		ModelName: modelName,
		HostName:  host,
		Port:      ports.ServePort,
		GPUIDs:    gpus,
		Timeout:   timeout,
		Ports:     ports,
		IsConvert: isConvert,
		MaxRequest: maxRequest,
		Waiting:   waitingChan,
	}
	m.instanceMonitorChannel <- instanceInfo
	<-waitingChan
	duration := time.Since(startTime)
	createInstanceDurationSummary.WithLabelValues(modelName, strconv.Itoa(len(gpus))).Observe(duration.Seconds())
	return nil
}

func (m *DefaultManager) StopInstance(ctx context.Context, uuid string, registerURI string) error {
	type CliDel struct {
		UUID string `json:"uuid"`
	}
	var cliDel CliDel
	cliDel.UUID = uuid
	hlog.CtxInfof(ctx, "send stop instance request: %v to URI %v", cliDel, registerURI)
	return tool.Post(ctx, cliDel, registerURI)
}

func (m *DefaultManager) GetRegisteredModels(ctx context.Context) []string {
	m.modelConfigLock.Lock()
	defer m.modelConfigLock.Unlock()
	// Slice to store keys
	modelNames := make([]string, 0, len(m.modelConfig)) // Pre-allocate for efficiency
	for modelName := range m.modelConfig {
		modelNames = append(modelNames, modelName)
	}
	return modelNames
}
