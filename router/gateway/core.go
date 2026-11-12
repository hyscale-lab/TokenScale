package gateway

import (
	"context"
	"os"
	"strconv"
	"sync"
	"time"

	"router/autoscaler"
	"router/model"
	"router/node"
	"router/route_policy"
	"router/tool"
	"router/window"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/google/uuid"
)

var (
	scalerMap  = make(map[string]*model.ScalerChan)
	scalerLock sync.RWMutex
)

type RouterInfo struct {
	RegisteredNodeNumber int                              `json:"registered_node_number"`
	RegisteredModels     []string                         `json:"registered_models"`
	ScalerInfoMap        map[string]autoscaler.ScalerInfo `json:"scaler_info_map"`
}

func portDiscovery(ctx context.Context, startPort int, host string, GPUUUIDS []string) map[string]int {
	result := make(map[string]int)
	if os.Getenv("DOCKER") == "1" {
		// In Docker, skip detection and assign sequential ports.
		for i, gpuUUID := range GPUUUIDS {
			result[gpuUUID] = startPort + i
		}
		hlog.CtxInfof(ctx, "[DOCKER=1] Skipping port detection, using ports: %v", result)
		return result
	}
	resultChan := make(chan int, len(GPUUUIDS))
	wg := sync.WaitGroup{}
	for range GPUUUIDS {
		wg.Add(1)
		go func(port int) {
			defer wg.Done()
			// do some work
			for {
				if !tool.PortDetect(ctx, host, port) {
					resultChan <- port
					return
				} else {
					port += len(GPUUUIDS)
				}
			}

		}(startPort)
		startPort++
	}
	wg.Wait()
	close(resultChan)
	ports := []int{}
	for port := range resultChan {
		ports = append(ports, port)
	}
	hlog.CtxInfof(ctx, "Detect ports: %v", ports)
	for i, gpuUUID := range GPUUUIDS {
		result[gpuUUID] = ports[i]
	}
	return result
}

func portsDiscovery(ctx context.Context, host string, GPUUUIDS []string) map[string]model.Ports {
	portstr := os.Getenv("PORT_DISCOVERY_START")
	if portstr == "" {
		portstr = "23333"
	}
	servePortStart, err := strconv.Atoi(portstr)
	if err != nil {
		hlog.CtxErrorf(ctx, "failed to obtain serve port start: %v", err)
	}
	// nixl_peer_init_port
	portstr = os.Getenv("NIXL_PEER_INIT_PORT_START")
	if portstr == "" {
		portstr = "7300"
	}
	nixlPeerInitPortStart, err := strconv.Atoi(portstr)
	if err != nil {
		hlog.CtxErrorf(ctx, "failed to obtain nixl_peer_init_port start: %v", err)
	}
	// nixl_peer_alloc_port
	portstr = os.Getenv("NIXL_PEER_ALLOC_PORT_START")
	if portstr == "" {
		portstr = "7400"
	}
	nixlPeerAllocPortStart, err := strconv.Atoi(portstr)
	if err != nil {
		hlog.CtxErrorf(ctx, "failed to obtain nixl_alloc_init_port start: %v", err)
	}
	// nixl_peer_delete_port
	portstr = os.Getenv("NIXL_PEER_DELETE_PORT_START")
	if portstr == "" {
		portstr = "7450"
	}
	nixlPeerDeletePortStart, err := strconv.Atoi(portstr)
	if err != nil {
		hlog.CtxErrorf(ctx, "failed to obtain nixl_delete_init_port start: %v", err)
	}

	servePorts := portDiscovery(ctx, servePortStart, host, GPUUUIDS)
	nixlPeerInitPorts := portDiscovery(ctx, nixlPeerInitPortStart, host, GPUUUIDS)
	nixlPeerAllocPorts := portDiscovery(ctx, nixlPeerAllocPortStart, host, GPUUUIDS)
	nixlPeerDeletePorts := portDiscovery(ctx, nixlPeerDeletePortStart, host, GPUUUIDS)
	portsMap := map[string]model.Ports{}
	for gpuUUID, servePort := range servePorts {
		ports := model.Ports{
			ServePort:          servePort,
			NixlPeerInitPort:   nixlPeerInitPorts[gpuUUID],
			NixlPeerAllocPort:  nixlPeerAllocPorts[gpuUUID],
			NixlPeerDeletePort: nixlPeerDeletePorts[gpuUUID],
		}
		portsMap[gpuUUID] = ports

	}
	return portsMap
}

func RegisterModel(ctx context.Context, req model.RegisterModel) error {
	err := route_policy.RegisterModel(ctx, req.ModelName, nil)
	if err != nil {
		hlog.CtxInfof(ctx, "register model failed: %v", err)
		return err
	}
	info, err := autoscaler.RegisterModel(ctx, req.ModelName, req.AutoScalerConfig)
	if err != nil {
		hlog.CtxInfof(ctx, "register model failed: %v", err)
		return err
	}
	err = node.RegisterModel(ctx, req)
	if err != nil {
		hlog.CtxInfof(ctx, "register model failed: %v", err)
		return err
	}
	hlog.CtxInfof(ctx, "register model success: %v", info.ModelName)
	scalerLock.Lock()
	defer scalerLock.Unlock()
	scalerMap[req.ModelName] = info
	go node.AllocateChan(context.Background(), info.ScaleUpDecision, info.ScaleUpResult, req.ModelName)
	go node.DeallocateChan(context.Background(), info.ScaleDownDecision, info.ScaleDownResult, req.ModelName)
	return nil
}

func RegisterNode(ctx context.Context, req model.RegisterNode) {
	nodeUUID := uuid.New().String()

	nd := model.Node{
		UUID:     nodeUUID,
		GPUUUIDs: req.GPUUUIDs,
		Host:     req.Host,
		Port:     req.Port,
		IPorts:   portsDiscovery(ctx, req.Host, req.GPUUUIDs),
	}
	node.RegisterNode(ctx, &nd)
	autoscaler.RegisterNode(ctx, &nd)
}

func Route(ctx context.Context, modelName string, c *app.RequestContext) {
	router := getGateway()
	// record incoming traffic
	reqInfo, err := window.ConstructReqInfo(ctx, c)
	if err != nil {
		hlog.CtxErrorf(ctx, "failed to construct req info: %v", err)
	}
	window.WorkloadWindow.Add(time.Now(), reqInfo)
	hlog.CtxInfof(ctx, "add req info into the workload window")
	router.Route(ctx, modelName, c)
}

func GetRouterInfo(ctx context.Context) RouterInfo {
	registeredNodeNumber := node.GetRegisteredNodeNumber(ctx)
	registeredModels := node.GetRegisteredModels(ctx)
	scalerInfoMap := autoscaler.GetScalerInfoMap(ctx)
	routerInfo := RouterInfo{
		RegisteredNodeNumber: registeredNodeNumber,
		RegisteredModels:     registeredModels,
		ScalerInfoMap:        scalerInfoMap,
	}
	return routerInfo
}

func GetScalerChan(ctx context.Context, modelName string) *model.ScalerChan {
	return scalerMap[modelName]
}
