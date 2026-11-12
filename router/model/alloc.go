package model

import (
	"context"
	"strconv"
	"sync"

	"github.com/cloudwego/hertz/pkg/common/hlog"
)

type AllocateRequest struct {
	InstanceUUID  string `json:"instance_uuid"`
	GPUNumPerNode int    `json:"gpu_num_per_node"`
	NodeNum       int    `json:"node_num"`
	IsConvert	  bool   `json:"is_convert"`
	MaxRequest 	  int64	 `json:"max_request"`
}

type AllocateResponse struct {
	NodeUUIDs []string
	GPUIDs    map[string][]string
	GPUUUIDs  map[string][]string
}

type NodeAllocStatus struct {
	GPUOwnerShip  map[string]string
	GPUUUIDs      []string
	OwnerShipLock sync.RWMutex
}

func (p *NodeAllocStatus) CheckOrAllocate(ctx context.Context, InstanceUUID string, GPUNum int) (bool, []string) {
	p.OwnerShipLock.Lock()
	defer p.OwnerShipLock.Unlock()
	hlog.CtxInfof(ctx, "CheckOrAllocate %v (%v) for %v", InstanceUUID, p.GPUUUIDs, GPUNum)
	freeGPU := make([]string, 0)
	for _, uuid := range p.GPUUUIDs {
		if _, ok := p.GPUOwnerShip[uuid]; !ok {
			freeGPU = append(freeGPU, uuid)
		}
	}
	hlog.CtxInfof(ctx, "freeGPU %v", freeGPU)
	if len(freeGPU) < GPUNum {
		return false, nil
	}
	for i := 0; i < GPUNum; i++ {
		p.GPUOwnerShip[freeGPU[i]] = InstanceUUID
	}
	hlog.CtxInfof(ctx, "Ownership %v", p.GPUOwnerShip)
	return true, freeGPU[0:GPUNum]
}

func (p *NodeAllocStatus) CheckGPUID(ctx context.Context, gpuUUID string) string {
	p.OwnerShipLock.RLock()
	defer p.OwnerShipLock.RUnlock()
	for i := range p.GPUUUIDs {
		if p.GPUUUIDs[i] == gpuUUID {
			return strconv.Itoa(i)
		}
	}
	return ""
}

func (p *NodeAllocStatus) DeAllocate(ctx context.Context, gpuUUIDs []string) {
	p.OwnerShipLock.Lock()
	defer p.OwnerShipLock.Unlock()
	hlog.CtxInfof(ctx, "DeAllocate %v", gpuUUIDs)
	for _, uuid := range gpuUUIDs {
		delete(p.GPUOwnerShip, uuid)
	}
	hlog.CtxInfof(ctx, "Owernship after deAllocate %v", p.GPUOwnerShip)
}

type CliReq struct {
	UUID string   `json:"uuid"`
	CLI  string   `json:"cli"`
	Env  []string `json:"env"`
	Args []string `json:"args"`
}
