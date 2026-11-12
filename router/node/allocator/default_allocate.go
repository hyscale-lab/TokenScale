package allocator

import (
	"context"
	"sync"

	"router/model"

	"github.com/cloudwego/hertz/pkg/common/hlog"
)

type DefaultAllocator struct {
	nodeList         map[string]*model.Node
	nodeStatusList   map[string]*model.NodeAllocStatus
	allocationResult map[string]*model.AllocateResponse
	resultLock       sync.RWMutex
	nodeLock         sync.RWMutex
}

func (alloc *DefaultAllocator) Register(ctx context.Context, node *model.Node) error {
	alloc.nodeLock.Lock()
	defer alloc.nodeLock.Unlock()
	alloc.nodeList[node.UUID] = node
	alloc.nodeStatusList[node.UUID] = &model.NodeAllocStatus{
		GPUOwnerShip:  make(map[string]string),
		GPUUUIDs:      node.GPUUUIDs,
		OwnerShipLock: sync.RWMutex{},
	}
	hlog.CtxInfof(ctx, "Register node %v", node)
	return nil
}

func (loc *DefaultAllocator) Allocate(ctx context.Context, req model.AllocateRequest) *model.AllocateResponse {
	loc.nodeLock.RLock()
	defer loc.nodeLock.RUnlock()
	resp := &model.AllocateResponse{
		NodeUUIDs: make([]string, 0),
		GPUUUIDs:  make(map[string][]string),
		GPUIDs:    make(map[string][]string),
	}
	for uuid := range loc.nodeList {
		hlog.CtxInfof(ctx, "Try to allocate on %v", uuid)
		for len(resp.NodeUUIDs) < req.NodeNum {
			ok, gpuUUID := loc.nodeStatusList[uuid].CheckOrAllocate(ctx, req.InstanceUUID, req.GPUNumPerNode)
			if !ok {
				break
			}
			resp.NodeUUIDs = append(resp.NodeUUIDs, uuid)
			resp.GPUUUIDs[uuid] = gpuUUID
			ids := make(chan string, len(gpuUUID))
			wg := sync.WaitGroup{}
			for _, id := range gpuUUID {
				wg.Add(1)
				go func(idx string) {
					defer wg.Done()
					ids <- loc.nodeStatusList[uuid].CheckGPUID(ctx, idx)
				}(id)
			}
			wg.Wait()
			close(ids)
			resp.GPUIDs[uuid] = make([]string, 0)
			for id := range ids {
				resp.GPUIDs[uuid] = append(resp.GPUIDs[uuid], id)
			}
			if len(resp.NodeUUIDs) == req.NodeNum {
				break
			}
		}
	}
	if len(resp.NodeUUIDs) < req.NodeNum {
		for _, uuid := range resp.NodeUUIDs {
			loc.nodeStatusList[uuid].DeAllocate(ctx, resp.GPUUUIDs[uuid])
		}
		hlog.CtxInfof(ctx, "No enough resource to allocate %v", req)
		return nil
	}
	hlog.CtxInfof(ctx, "Allocate %v", resp)
	loc.resultLock.Lock()
	defer loc.resultLock.Unlock()
	loc.allocationResult[req.InstanceUUID] = resp
	return resp
}

func (loc *DefaultAllocator) GetAllocationResult(ctx context.Context, instanceUUID string) *model.AllocateResponse {
	loc.resultLock.RLock()
	defer loc.resultLock.RUnlock()
	resp, ok := loc.allocationResult[instanceUUID]
	if !ok {
		return nil
	}
	return resp
}

func (loc *DefaultAllocator) Deallocate(ctx context.Context, instanceUUID string) error {
	loc.nodeLock.RLock()
	defer loc.nodeLock.RUnlock()
	loc.resultLock.Lock()
	defer loc.resultLock.Unlock()
	req := loc.allocationResult[instanceUUID]
	for _, uuid := range req.NodeUUIDs {
		loc.nodeStatusList[uuid].DeAllocate(ctx, req.GPUUUIDs[uuid])
	}
	delete(loc.allocationResult, instanceUUID)
	hlog.CtxInfof(ctx, "Deallocate %v", req)
	return nil
}

func (loc *DefaultAllocator) GetHostName(ctx context.Context, nodeUUID string) string {
	loc.nodeLock.RLock()
	defer loc.nodeLock.RUnlock()
	node, ok := loc.nodeList[nodeUUID]
	if !ok {
		return ""
	}
	return node.Host
}

func (loc *DefaultAllocator) GetHostPort(ctx context.Context, nodeUUID string) int {
	loc.nodeLock.RLock()
	defer loc.nodeLock.RUnlock()
	node, ok := loc.nodeList[nodeUUID]
	if !ok {
		return 0
	}
	return node.Port
}

func (loc *DefaultAllocator) GetAllocatedPorts(ctx context.Context, nodeUUID string, gpuUUID string) model.Ports {
	loc.nodeLock.RLock()
	defer loc.nodeLock.RUnlock()
	node, ok := loc.nodeList[nodeUUID]
	if !ok {
		return model.Ports{}
	}
	return node.IPorts[gpuUUID]
}

func (loc *DefaultAllocator) GetRegisteredNodeNumber(ctx context.Context) int {
	loc.nodeLock.RLock()
	defer loc.nodeLock.RUnlock()
	return len(loc.nodeList)
}
