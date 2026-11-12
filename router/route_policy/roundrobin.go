package route_policy

import (
	"context"
	"fmt"
	"router/model"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
)

type communication struct {
	NewRequest       chan struct{}
	RouteDecision    chan string
	FinishedRequest  chan string
	NewInstance      chan string
	FinishedInstance chan string
}

type RoundRobin struct {
	instanceMap     map[string]*communication
	instanceMapLock sync.RWMutex
	instanceDelChan sync.Map
}

func (r *RoundRobin) mainProcess(ctx context.Context, communicate *communication) {
	instanceList := make([]string, 0)
	finishedInstance := sync.Map{}
	instanceReqNum := sync.Map{} // notice this can be very different with scaler's request counter, especially in PD disaggregation
	instanceIndex := 0
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		for range ticker.C {
			instanceToDel := make([]string, 0)
			finishedInstance.Range(func(key, value interface{}) bool {
				instanceUUID := key.(string)
				cnt, _ := instanceReqNum.LoadOrStore(instanceUUID, new(int64))
				counter := cnt.(*int64)
				if atomic.LoadInt64(counter) == 0 {
					instanceToDel = append(instanceToDel, instanceUUID)
				} else {
					hlog.CtxWarnf(ctx, "Cannot delete instance %v for now, it still has %d requests not finished", instanceUUID, atomic.LoadInt64(counter))
				}
				return true
			})
			for _, instanceUUID := range instanceToDel {
				ch, ok := r.instanceDelChan.Load(instanceUUID)
				if !ok {
					hlog.CtxErrorf(ctx, "instance %v not found in instanceDelChan", instanceUUID)
					continue
				}
				ch.(chan struct{}) <- struct{}{}
				hlog.CtxInfof(ctx, "Instance %v deleted", instanceUUID)
				finishedInstance.Delete(instanceUUID)
			}
		}
	}()
	for {
		select {
		case <-communicate.NewRequest:
			if len(instanceList) == 0 {
				communicate.RouteDecision <- ""
				continue
			}
			instanceIndex = instanceIndex % len(instanceList)
			instanceUUID := instanceList[instanceIndex]
			instanceIndex = instanceIndex + 1
			hlog.CtxDebugf(ctx, "Route to instance %v", instanceUUID)
			communicate.RouteDecision <- instanceUUID
			cnt, _ := instanceReqNum.LoadOrStore(instanceUUID, new(int64))
			counter := cnt.(*int64)
			atomic.AddInt64(counter, 1)
		case instanceUUID := <-communicate.FinishedRequest:
			cnt, _ := instanceReqNum.LoadOrStore(instanceUUID, new(int64))
			counter := cnt.(*int64)
			atomic.AddInt64(counter, -1)
		case instanceUUID := <-communicate.NewInstance:
			instanceList = append(instanceList, instanceUUID)
		case instanceUUID := <-communicate.FinishedInstance:
			for i, uuid := range instanceList {
				if uuid == instanceUUID {
					instanceList = append(instanceList[:i], instanceList[i+1:]...)
					finishedInstance.Store(instanceUUID, struct{}{})
					break
				}
			}
			hlog.CtxInfof(ctx, "Instance %v want to be deleted. Now instance list: %v", instanceUUID, instanceList)
		}
	}
}

func (r *RoundRobin) RegisterModel(ctx context.Context, modelName string, modelInfo interface{}) error {
	r.instanceMapLock.Lock()
	defer r.instanceMapLock.Unlock()
	communicate := &communication{
		NewRequest:       make(chan struct{}),
		RouteDecision:    make(chan string),
		FinishedRequest:  make(chan string),
		NewInstance:      make(chan string),
		FinishedInstance: make(chan string),
	}
	r.instanceMap[modelName] = communicate
	go r.mainProcess(context.Background(), communicate)
	return nil
}

func (r *RoundRobin) RegisterInstance(ctx context.Context, instanceUUID string, modelName string, instanceStatus model.InstanceStatus) error {
	r.instanceMapLock.RLock()
	defer r.instanceMapLock.RUnlock()
	communicate, ok := r.instanceMap[modelName]
	if !ok {
		return fmt.Errorf("model %v not registered", modelName)
	}
	communicate.NewInstance <- instanceUUID
	return nil
}

func (r *RoundRobin) UnregisterInstance(ctx context.Context, instanceUUID string, modelName string) error {
	r.instanceMapLock.RLock()
	defer r.instanceMapLock.RUnlock()
	communicate, ok := r.instanceMap[modelName]
	if !ok {
		return fmt.Errorf("model %v not registered", modelName)
	}
	signalChan := make(chan struct{})
	r.instanceDelChan.Store(instanceUUID, signalChan)
	communicate.FinishedInstance <- instanceUUID
	hlog.CtxInfof(ctx, "Waiting for instance %v to be deleted", instanceUUID)
	<-signalChan
	hlog.CtxInfof(ctx, "Instance %v deleted", instanceUUID)
	return nil
}

func (r *RoundRobin) RouteDecision(ctx context.Context, modelName string, routerRequestInfo model.RouterRequest) (model.RouterReply, error) {
	r.instanceMapLock.RLock()
	defer r.instanceMapLock.RUnlock()
	communicate, ok := r.instanceMap[modelName]
	if !ok {
		return model.RouterReply{}, fmt.Errorf("model %v not registered", modelName)
	}
	communicate.NewRequest <- struct{}{}
	hlog.CtxInfof(ctx, "New Request arrived")
	instanceUUID := <-communicate.RouteDecision
	if instanceUUID == "" {
		return model.RouterReply{}, fmt.Errorf("no instance available")
	}
	return model.RouterReply{
		InstanceUUID: instanceUUID,
	}, nil
}

func (r *RoundRobin) FininshedRequest(ctx context.Context, modelName string, instanceUUID string, routeRequest model.RouterRequest) error {
	r.instanceMapLock.RLock()
	defer r.instanceMapLock.RUnlock()
	communicate, ok := r.instanceMap[modelName]
	if !ok {
		return fmt.Errorf("model %v not registered", modelName)
	}
	communicate.FinishedRequest <- instanceUUID
	return nil
}

func (r *RoundRobin) GetMinTokInstance(ctx context.Context, modelName string) (model.MinTokNumReply, error) {
	return model.MinTokNumReply{}, fmt.Errorf("RR router doesn't support querying minTokInstance")
}
