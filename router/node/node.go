package node

import (
	"context"
	"fmt"

	"router/metrics"
	"router/model"
	"router/node/allocator"
	"router/node/manager"
	"router/node/traffic_monitor"
	"router/route_policy"
	sd "router/servicediscovery"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	alloc                 allocator.Allocator
	mgr                   manager.Manager
	createInstanceCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "router:create_instance_total",
			Help: "The total number of create instance",
		},
		[]string{"model"},
	)
)

func init() {
	alloc = allocator.New()
	mgr = manager.New()
	metrics.Register(createInstanceCounter)
}

func RegisterNode(ctx context.Context, node *model.Node) {
	alloc.Register(ctx, node)
}

func GetRegisteredNodeNumber(ctx context.Context) int {
	return alloc.GetRegisteredNodeNumber(ctx)
}

func RegisterModel(ctx context.Context, req model.RegisterModel) error {
	return mgr.RegisterModel(ctx, req)
}

func GetRegisteredModels(ctx context.Context) []string {
	return mgr.GetRegisteredModels(ctx)
}

func OnAllocateSuccess(ctx context.Context, instanceUUID string, modelName string) {
	createInstanceCounter.WithLabelValues(modelName).Inc()
	info, err := GetInstanceInfo(ctx, instanceUUID)
	if err != nil {
		hlog.CtxErrorf(ctx, "Failed to get instance info for %v: %v", instanceUUID, err)
		return
	}
	route_policy.RegisterInstance(ctx, instanceUUID, modelName, info)
	config, _ := GetInstanceInfo(ctx, instanceUUID)
	sd.NewInstance(config)

}

func AllocateChan(ctx context.Context, reqChan chan model.AllocateRequest, respChan chan interface{}, modelName string) {
	for req := range reqChan {
		go func() {
			hlog.CtxInfof(ctx, "try to allocate %v", req)
			err := Allocate(ctx, req, modelName)
			var result bool
			if err != nil {
				hlog.CtxInfof(ctx, "allocate failed %v", err)
				result = false
			} else {
				result = true
			}
			respChan <- model.Pair{
				First:  result,
				Second: req.InstanceUUID,
			}
			if result {
				OnAllocateSuccess(ctx, req.InstanceUUID, modelName)
			}
		}()
	}
}

func Allocate(ctx context.Context, req model.AllocateRequest, modelName string) error {
	if req.NodeNum != 1 {
		hlog.CtxErrorf(ctx, "Only support allocate one node currently")
		return fmt.Errorf("ONLY ONE NODE")
	}
	if !mgr.CheckModel(ctx, modelName) {
		return fmt.Errorf("model %v not registered", modelName)
	}
	resp := alloc.Allocate(ctx, req)
	if resp == nil {
		hlog.CtxInfof(ctx, "No enough resource to allocate %v", req)
		return fmt.Errorf("NO enough resource to allocate %v", req)
	}
	hlog.CtxInfof(ctx, "Allocate %v", resp)
	for _, uuid := range resp.NodeUUIDs {
		gpuIDs, ok := resp.GPUIDs[uuid]
		if !ok {
			return fmt.Errorf("no gpu allocated for node %v", uuid)
		}
		gpuUUIDs, ok := resp.GPUUUIDs[uuid]
		if !ok {
			return fmt.Errorf("no gpu allocated for node %v", uuid)
		}
		host := alloc.GetHostName(ctx, uuid)
		port := alloc.GetHostPort(ctx, uuid)
		var lmcachePorts model.LMCachePorts
		for idx, gpuUUID := range gpuUUIDs {
			ap := alloc.GetAllocatedPorts(ctx, uuid, gpuUUID)

			if idx == 0 {
				lmcachePorts.ServePort = ap.ServePort
			}

			lmcachePorts.NixlPeerAllocPortList = append(lmcachePorts.NixlPeerAllocPortList, ap.NixlPeerAllocPort)
			lmcachePorts.NixlPeerInitPortList = append(lmcachePorts.NixlPeerInitPortList, ap.NixlPeerInitPort)
			lmcachePorts.NixlPeerDeletePortList = append(lmcachePorts.NixlPeerDeletePortList, ap.NixlPeerDeletePort)
		}
		registerURI := fmt.Sprintf("http://%v:%v/create", host, port)
		createFileURI := fmt.Sprintf("http://%v:%v/create_file", host, port)
		err := mgr.CreateInstance(ctx, req.InstanceUUID, modelName, host, lmcachePorts, gpuIDs, registerURI, createFileURI, req.IsConvert, req.MaxRequest)
		if err != nil {
			hlog.CtxErrorf(ctx, "create instance failed: %v", err)
			return err
		}
	}
	return nil
}

func DeallocateChan(ctx context.Context, reqChan chan string, respChan chan interface{}, modelName string) {
	for instanceUUID := range reqChan {
		go func() {
			hlog.CtxInfof(ctx, "try to deallocate %v for %v", instanceUUID, modelName)
			route_policy.UnregisterInstance(ctx, instanceUUID, modelName)
			hlog.CtxInfof(ctx, "Instance %v can be deleted", instanceUUID)
			err := Deallocate(ctx, instanceUUID)
			if err != nil {
				hlog.CtxInfof(ctx, "deallocate failed %v", err)
			}
			hlog.CtxInfof(ctx, "deallocate %v", instanceUUID)
			respChan <- instanceUUID
			sd.DelInstance(instanceUUID)
		}()
	}
}

func Deallocate(ctx context.Context, instanceUUID string) error {
	req := alloc.GetAllocationResult(ctx, instanceUUID)
	if req == nil {
		return fmt.Errorf("instance %v not allocated", instanceUUID)
	}
	for _, uuid := range req.NodeUUIDs {
		host := alloc.GetHostName(ctx, uuid)
		port := alloc.GetHostPort(ctx, uuid)
		deleteURI := fmt.Sprintf("http://%v:%v/delete", host, port)
		err := mgr.StopInstance(ctx, instanceUUID, deleteURI)
		hlog.CtxInfof(ctx, "stop instance %v on node %v", instanceUUID, uuid)
		if err != nil {
			hlog.CtxErrorf(ctx, "stop instance failed: %v", err)
		}
	}
	hlog.CtxInfof(ctx, "delete instance %v in allocation map", instanceUUID)
	return alloc.Deallocate(ctx, instanceUUID)
}

func GetInstanceInfo(ctx context.Context, instanceUUID string) (model.InstanceStatus, error) {
	return mgr.GetInstanceInfo(ctx, instanceUUID)
}

func GetInstanceReverseProxy(ctx context.Context, instanceUUID string) (model.ReverseProxy, error) {
	return mgr.GetInstanceReverseProxy(ctx, instanceUUID)
}

func GetTrafficMonitor(ctx context.Context, modelName string) (*traffic_monitor.TrafficMonitor, error) {
	return mgr.GetTrafficMonitor(ctx, modelName)
}

func SetManagerForTest(m manager.Manager) {
	mgr = m
}
