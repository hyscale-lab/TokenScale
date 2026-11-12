package route_policy

import (
	"context"
	"fmt"
	"router/model"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
)

type localityAwareComm struct {
	NewRequestPrefill chan model.RouterRequest
	NewRequestDecode  chan model.RouterRequest
	RouteDecision     chan string
	FinishedRequest   chan string
	NewInstance       chan string
	FinishedInstance  chan string
}

type LocalityAware struct {
	instanceMap     map[string]*localityAwareComm
	instanceMapLock sync.RWMutex
	instanceDelChan sync.Map

	instanceToHost sync.Map // instanceUUID -> hostName
	reqToHost      sync.Map // reqID -> hostName
}

func (r *LocalityAware) mainProcess(ctx context.Context, communicate *localityAwareComm) {
	instanceList := make([]string, 0)
	finishedInstance := sync.Map{}
	instanceReqNum := sync.Map{}
	instanceIndex := 0

	// background GC loop
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
					hlog.CtxWarnf(ctx, "Cannot delete instance %v, still has %d reqs", instanceUUID, atomic.LoadInt64(counter))
				}
				return true
			})
			for _, instanceUUID := range instanceToDel {
				ch, ok := r.instanceDelChan.Load(instanceUUID)
				if !ok {
					hlog.CtxErrorf(ctx, "GC: instance %v not found in delChan", instanceUUID)
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
		case reqCtx := <-communicate.NewRequestPrefill:
			reqID := reqCtx.ReqID
			if len(instanceList) == 0 {
				communicate.RouteDecision <- ""
				continue
			}
			instanceIndex = instanceIndex % len(instanceList)
			instanceUUID := instanceList[instanceIndex]
			instanceIndex++
			hlog.CtxDebugf(ctx, "Round-robin route to prefill instance %v", instanceUUID)
			communicate.RouteDecision <- instanceUUID

			cnt, _ := instanceReqNum.LoadOrStore(instanceUUID, new(int64))
			atomic.AddInt64(cnt.(*int64), 1)
			prefillHost, ok := r.instanceToHost.Load(instanceUUID)
			if !ok {
				continue
			}
			r.reqToHost.Store(reqID, prefillHost)

		case reqCtx := <-communicate.NewRequestDecode:
			reqID := reqCtx.ReqID
			prefillHost := ""
			if val, ok := r.reqToHost.Load(reqID); ok {
				prefillHost = val.(string)
			}

			selected := ""
			minLoad := int64(1<<62 - 1)

			for _, instanceUUID := range instanceList {
				hostVal, ok := r.instanceToHost.Load(instanceUUID)
				if !ok || hostVal.(string) != prefillHost {
					continue
				}
				cnt, _ := instanceReqNum.LoadOrStore(instanceUUID, new(int64))
				current := atomic.LoadInt64(cnt.(*int64))
				if current < minLoad {
					selected = instanceUUID
					minLoad = current
				}
			}

			if selected == "" && len(instanceList) > 0 {
				for _, instanceUUID := range instanceList {
					cnt, _ := instanceReqNum.LoadOrStore(instanceUUID, new(int64))
					current := atomic.LoadInt64(cnt.(*int64))
					if current < minLoad {
						selected = instanceUUID
						minLoad = current
					}
				}
			}

			hlog.CtxDebugf(ctx, "[Locality] decode req %v -> %v (host=%v load=%d)", reqID, selected, prefillHost, minLoad)
			communicate.RouteDecision <- selected
			if selected != "" {
				cnt, _ := instanceReqNum.LoadOrStore(selected, new(int64))
				atomic.AddInt64(cnt.(*int64), 1)
			}

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

func (r *LocalityAware) RegisterModel(ctx context.Context, modelName string, modelInfo interface{}) error {
	r.instanceMapLock.Lock()
	defer r.instanceMapLock.Unlock()
	communicate := &localityAwareComm{
		NewRequestPrefill: make(chan model.RouterRequest),
		NewRequestDecode:  make(chan model.RouterRequest),
		RouteDecision:     make(chan string),
		FinishedRequest:   make(chan string),
		NewInstance:       make(chan string),
		FinishedInstance:  make(chan string),
	}
	r.instanceMap[modelName] = communicate
	go r.mainProcess(context.Background(), communicate)
	return nil
}

func (r *LocalityAware) RegisterInstance(ctx context.Context, instanceUUID string, modelName string, instanceStatus model.InstanceStatus) error {
	info := instanceStatus
	r.instanceToHost.Store(instanceUUID, info.HostName)

	r.instanceMapLock.RLock()
	defer r.instanceMapLock.RUnlock()
	communicate, ok := r.instanceMap[modelName]
	if !ok {
		return fmt.Errorf("model %v not registered", modelName)
	}
	communicate.NewInstance <- instanceUUID
	return nil
}

func (r *LocalityAware) UnregisterInstance(ctx context.Context, instanceUUID string, modelName string) error {
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

func (r *LocalityAware) RouteDecision(ctx context.Context, modelName string, routerRequestInfo model.RouterRequest) (model.RouterReply, error) {
	r.instanceMapLock.RLock()
	defer r.instanceMapLock.RUnlock()

	comm, ok := r.instanceMap[modelName]
	if !ok {
		return model.RouterReply{}, fmt.Errorf("model %v not registered", modelName)
	}
	reqCtx := routerRequestInfo
	reqID := reqCtx.ReqID
	isPrefill := false
	if strings.HasSuffix(modelName, "::prefill") {
		isPrefill = true
	}
	isDecode := false
	if strings.HasSuffix(modelName, "::decode") {
		isDecode = true
	}

	hlog.CtxInfof(ctx, "New Request arrived")
	// ==== Prefill routing RR ====
	if isPrefill {
		comm.NewRequestPrefill <- model.RouterRequest{ReqID: reqID}
		instanceUUID := <-comm.RouteDecision
		if instanceUUID == "" {
			return model.RouterReply{}, fmt.Errorf("no instance available")
		}
		return model.RouterReply{InstanceUUID: instanceUUID}, nil
	}

	// ====== Decode routing ======
	if isDecode {
		comm.NewRequestDecode <- model.RouterRequest{ReqID: reqID}
		instanceUUID := <-comm.RouteDecision
		if instanceUUID == "" {
			return model.RouterReply{}, fmt.Errorf("no instance available")
		}
		return model.RouterReply{InstanceUUID: instanceUUID}, nil
	}

	return model.RouterReply{}, fmt.Errorf("no instance available")
}

func (r *LocalityAware) FininshedRequest(ctx context.Context, modelName string, instanceUUID string, routerRequestInfo model.RouterRequest) error {
	r.instanceMapLock.RLock()
	defer r.instanceMapLock.RUnlock()
	communicate, ok := r.instanceMap[modelName]
	if !ok {
		return fmt.Errorf("model %v not registered", modelName)
	}
	communicate.FinishedRequest <- instanceUUID
	return nil
}

func (r *LocalityAware) GetMinTokInstance(ctx context.Context, modelName string) (model.MinTokNumReply, error) {
	return model.MinTokNumReply{}, fmt.Errorf("Locality-aware router doesn't support querying minTokInstance")
}
