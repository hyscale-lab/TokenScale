package route_policy

import (
	"context"
	"os"
	"router/model"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
)

var (
	router Router
)

type FinishedRequestInfo struct {
	InstanceUUID     string
	RouteRequestInfo model.RouterRequest
}

func init() {
	routePolicy := os.Getenv("ROUTE_POLICY")
	if routePolicy == "RR" || routePolicy == "" {
		router = &RoundRobin{
			instanceMap:     make(map[string]*communication),
			instanceMapLock: sync.RWMutex{},
			instanceDelChan: sync.Map{},
		}
	} else if routePolicy == "LocalityAware" {
		router = &LocalityAware{
			instanceMap:     make(map[string]*localityAwareComm),
			instanceMapLock: sync.RWMutex{},
			instanceDelChan: sync.Map{},
			instanceToHost:  sync.Map{},
			reqToHost:       sync.Map{},
		}
	} else if routePolicy == "TP" {
		router = &Tokenpipe{
			instanceMap:       make(map[string]*TokenpipeComm),
			instanceStatusMap: make(map[string]model.InstanceStatus),
			instanceMapLock:   sync.RWMutex{},
			instanceDelChan:   sync.Map{},
		}
	} else {
		hlog.Errorf("Unsupported route policy: %s", routePolicy)
		return
	}
}

type Router interface {
	RegisterModel(ctx context.Context, modelName string, modelInfo interface{}) error
	RegisterInstance(ctx context.Context, instanceUUID string, modelName string, instanceStatus model.InstanceStatus) error
	UnregisterInstance(ctx context.Context, instanceUUID string, modelName string) error
	RouteDecision(ctx context.Context, modelName string, routerRequestInfo model.RouterRequest) (model.RouterReply, error)
	FininshedRequest(ctx context.Context, modelName string, instanceUUID string, routerRequestInfo model.RouterRequest) error
	GetMinTokInstance(ctx context.Context, modelName string) (model.MinTokNumReply, error)
}

func RegisterModel(ctx context.Context, modelName string, modelInfo interface{}) error {
	return router.RegisterModel(ctx, modelName, modelInfo)
}

func RegisterInstance(ctx context.Context, instanceUUID string, modelName string, instanceStatus model.InstanceStatus) error {
	return router.RegisterInstance(ctx, instanceUUID, modelName, instanceStatus)
}

func UnregisterInstance(ctx context.Context, instanceUUID string, modelName string) error {
	return router.UnregisterInstance(ctx, instanceUUID, modelName)
}

func RouteDecision(ctx context.Context, modelName string, requestContext model.RouterRequest) (model.RouterReply, error) {
	return router.RouteDecision(ctx, modelName, requestContext)
}

func FininshedRequest(ctx context.Context, modelName string, instanceUUID string, routerRequestInfo model.RouterRequest) error {
	return router.FininshedRequest(ctx, modelName, instanceUUID, routerRequestInfo)
}

func GetMinTokInstance(ctx context.Context, modelName string) (model.MinTokNumReply, error) {
	return router.GetMinTokInstance(ctx, modelName)
}

func MustRoute(ctx context.Context, modelName string, routerRequestInfo model.RouterRequest) model.RouterReply {
	for {
		routeInstance, err := RouteDecision(ctx, modelName, routerRequestInfo)
		if err == nil {
			return routeInstance
		}
		hlog.CtxInfof(ctx, "route failed for %s: %v, retrying in 1s...", modelName, err)
		time.Sleep(1 * time.Second)
	}
}


// For testing purpose
func SetRoutePolicy(r Router) {
	router = r
}
