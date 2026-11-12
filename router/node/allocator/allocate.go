package allocator

import (
	"context"
	"os"
	"sync"

	"router/model"
)

type Allocator interface {
	Register(ctx context.Context, node *model.Node) error
	Allocate(ctx context.Context, req model.AllocateRequest) *model.AllocateResponse
	Deallocate(ctx context.Context, instanceUUID string) error
	GetHostName(ctx context.Context, nodeUUID string) string
	GetHostPort(ctx context.Context, nodeUUID string) int
	GetAllocatedPorts(ctx context.Context, nodeUUID string, gpuUUID string) model.Ports
	GetAllocationResult(ctx context.Context, instanceUUID string) *model.AllocateResponse
	GetRegisteredNodeNumber(ctx context.Context) int
}

func New() Allocator {
	allocator := os.Getenv("ALLOCATOR")
	switch allocator {
	default:
		return &DefaultAllocator{
			nodeList:         make(map[string]*model.Node),
			nodeStatusList:   make(map[string]*model.NodeAllocStatus),
			allocationResult: make(map[string]*model.AllocateResponse),
			resultLock:       sync.RWMutex{},
			nodeLock:         sync.RWMutex{},
		}
	}
}
