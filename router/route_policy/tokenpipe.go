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

type TokenpipeComm struct {
	PrefillRouterRequestInfoChan chan model.RouterRequest
	DecodeRouteRequestInfoChan   chan model.RouterRequest
	RouteDecision                chan string
	FinishedRequest              chan FinishedRequestInfo
	NewInstance                  chan string
	FinishedInstance             chan string
	minTokNumReqChan             chan model.MinTokNumRequest
	minTokNumReplyChan           chan model.MinTokNumReply
}

type Tokenpipe struct {
	instanceMap       map[string]*TokenpipeComm
	instanceStatusMap map[string]model.InstanceStatus
	instanceMapLock   sync.RWMutex
	instanceDelChan   sync.Map
}

func NewTokenpipeRoutePolicy() *Tokenpipe {
	return &Tokenpipe{
		instanceMap:       make(map[string]*TokenpipeComm),
		instanceStatusMap: make(map[string]model.InstanceStatus),
		instanceMapLock:   sync.RWMutex{},
		instanceDelChan:   sync.Map{},
	}
}

func (r *Tokenpipe) mainProcess(ctx context.Context, communicate *TokenpipeComm) {
	instanceList := make([]string, 0)
	finishedInstance := sync.Map{}
	instanceReqNum := sync.Map{} // notice this can be very different with scaler's request counter, especially in PD disaggregation
	instanceTokNum := sync.Map{}
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
		case routeRequest := <-communicate.PrefillRouterRequestInfoChan:
			if len(instanceList) == 0 {
				communicate.RouteDecision <- ""
				continue
			}
			// find the instance with least tokens
			minTok := int64(-1)
			minInstance := ""

			for _, instanceUUID := range instanceList {
				tokNum, _ := instanceTokNum.LoadOrStore(instanceUUID, new(int64))
				tokCounter := tokNum.(*int64)
				currTok := atomic.LoadInt64(tokCounter)
				if minTok == -1 || currTok < minTok {
					minTok = currTok
					minInstance = instanceUUID
				}
			}
			if minInstance != "" {
				hlog.CtxInfof(ctx, "req %s is route to instance %v with min tokens %d", routeRequest.ReqID, minInstance, minTok)
				communicate.RouteDecision <- minInstance
				cnt, _ := instanceReqNum.LoadOrStore(minInstance, new(int64))
				counter := cnt.(*int64)
				atomic.AddInt64(counter, 1)

				tokNum, _ := instanceTokNum.LoadOrStore(minInstance, new(int64))
				tokCounter := tokNum.(*int64)
				atomic.AddInt64(tokCounter, int64(routeRequest.NumPrefillTokens))
			} else {
				communicate.RouteDecision <- ""
				continue
			}

		case routeRequest := <-communicate.DecodeRouteRequestInfoChan:
			if len(instanceList) == 0 {
				communicate.RouteDecision <- ""
				continue
			}

			// --- Case 1: PrefillCapacityExceed, this means this request is going to skip prefiller ---
			if routeRequest.CapacityExceed {
				var routed bool
				for _, instanceUUID := range instanceList {
					ok, err := r.CheckInstanceConvert(instanceUUID)
					if err != nil {
						hlog.CtxWarnf(ctx, "CheckInstanceConvert error for %s: %v", instanceUUID, err)
						continue
					}
					if ok {
						hlog.CtxDebugf(ctx, "Prefill exceed: routed to convertible instance %s", instanceUUID)
						communicate.RouteDecision <- instanceUUID
						cnt, _ := instanceReqNum.LoadOrStore(instanceUUID, new(int64))
						counter := cnt.(*int64)
						atomic.AddInt64(counter, 1)
						routed = true
						break
					}
				}
				if !routed {
					hlog.CtxWarnf(ctx, "Prefill exceed: no convertible instance found, routing to default")
					// fallback: normal round-robin
					instanceIndex = instanceIndex % len(instanceList)
					instanceUUID := instanceList[instanceIndex]
					instanceIndex++
					communicate.RouteDecision <- instanceUUID
					cnt, _ := instanceReqNum.LoadOrStore(instanceUUID, new(int64))
					counter := cnt.(*int64)
					atomic.AddInt64(counter, 1)
				}
				continue
			}

			// --- Case 2: Normal round-robin with maxRequest check ---
			var chosen string
			attempts := 0
			for attempts < len(instanceList) {
				instanceIndex = instanceIndex % len(instanceList)
				instanceUUID := instanceList[instanceIndex]
				instanceIndex++

				maxReq, err := r.GetInstanceMaxRequest(instanceUUID)
				if err != nil {
					hlog.CtxWarnf(ctx, "GetInstanceMaxRequest error for %s: %v", instanceUUID, err)
					attempts++
					continue
				}

				cnt, _ := instanceReqNum.LoadOrStore(instanceUUID, new(int64))
				counter := cnt.(*int64)
				curr := atomic.LoadInt64(counter)

				if maxReq <= 0 || curr < maxReq {
					chosen = instanceUUID
					atomic.AddInt64(counter, 1)
					hlog.CtxDebugf(ctx, "Route to decode instance %v, curr req: %d, max req: %d", chosen, curr, maxReq)
					break
				}

				hlog.CtxDebugf(ctx, "Instance %s is at max capacity (curr=%d, max=%d)", instanceUUID, curr, maxReq)
				attempts++
			}

			if chosen == "" {
				hlog.CtxWarnf(ctx, "All instances at max capacity, returning empty route")
				communicate.RouteDecision <- ""
				continue
			}

			communicate.RouteDecision <- chosen

		case finishRequestInfo := <-communicate.FinishedRequest:
			instanceUUID := finishRequestInfo.InstanceUUID
			cnt, _ := instanceReqNum.LoadOrStore(instanceUUID, new(int64))
			counter := cnt.(*int64)
			atomic.AddInt64(counter, -1)

			tokNum, _ := instanceTokNum.LoadOrStore(instanceUUID, new(int64))
			tokCounter := tokNum.(*int64)
			atomic.AddInt64(tokCounter, -1*int64(finishRequestInfo.RouteRequestInfo.NumPrefillTokens))

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
		case _ = <-communicate.minTokNumReqChan:
			minTok := int64(-1)
			minInstance := ""

			for _, instanceUUID := range instanceList {
				tokNum, _ := instanceTokNum.LoadOrStore(instanceUUID, new(int64))
				tokCounter := tokNum.(*int64)
				currTok := atomic.LoadInt64(tokCounter)
				if minTok == -1 || currTok < minTok {
					minTok = currTok
					minInstance = instanceUUID
				}
			}
			communicate.minTokNumReplyChan <- model.MinTokNumReply{
				NumTok:       minTok,
				InstanceUUID: minInstance,
			}
		}
	}
}

func (r *Tokenpipe) RegisterModel(ctx context.Context, modelName string, modelInfo interface{}) error {
	r.instanceMapLock.Lock()
	defer r.instanceMapLock.Unlock()
	communicate := &TokenpipeComm{
		PrefillRouterRequestInfoChan: make(chan model.RouterRequest),
		DecodeRouteRequestInfoChan:   make(chan model.RouterRequest),
		RouteDecision:                make(chan string),
		FinishedRequest:              make(chan FinishedRequestInfo),
		NewInstance:                  make(chan string),
		FinishedInstance:             make(chan string),
		minTokNumReqChan:             make(chan model.MinTokNumRequest),
		minTokNumReplyChan:           make(chan model.MinTokNumReply),
	}
	r.instanceMap[modelName] = communicate
	go r.mainProcess(context.Background(), communicate)
	return nil
}

func (r *Tokenpipe) RegisterInstance(ctx context.Context, instanceUUID string, modelName string, instanceStatus model.InstanceStatus) error {
	r.instanceMapLock.RLock()
	defer r.instanceMapLock.RUnlock()
	r.instanceStatusMap[instanceUUID] = instanceStatus
	communicate, ok := r.instanceMap[modelName]
	if !ok {
		return fmt.Errorf("model %v not registered", modelName)
	}
	communicate.NewInstance <- instanceUUID
	return nil
}

func (r *Tokenpipe) UnregisterInstance(ctx context.Context, instanceUUID string, modelName string) error {
	r.instanceMapLock.RLock()
	defer r.instanceMapLock.RUnlock()
	communicate, ok := r.instanceMap[modelName]
	if !ok {
		return fmt.Errorf("model %v not registered", modelName)
	}
	delete(r.instanceStatusMap, instanceUUID)
	signalChan := make(chan struct{})
	r.instanceDelChan.Store(instanceUUID, signalChan)
	communicate.FinishedInstance <- instanceUUID
	hlog.CtxInfof(ctx, "Waiting for instance %v to be deleted", instanceUUID)
	<-signalChan
	hlog.CtxInfof(ctx, "Instance %v deleted", instanceUUID)
	return nil
}

func (r *Tokenpipe) RouteDecision(ctx context.Context, modelName string, routeRequest model.RouterRequest) (model.RouterReply, error) {
	r.instanceMapLock.RLock()
	defer r.instanceMapLock.RUnlock()
	communicate, ok := r.instanceMap[modelName]
	if !ok {
		return model.RouterReply{}, fmt.Errorf("model %v not registered", modelName)
	}
	if routeRequest.IsPrefill {
		communicate.PrefillRouterRequestInfoChan <- routeRequest
	} else {
		communicate.DecodeRouteRequestInfoChan <- routeRequest
	}
	hlog.CtxInfof(ctx, "New Request %s arrived (is_prefill: %v)", routeRequest.ReqID, routeRequest.IsPrefill)
	instanceUUID := <-communicate.RouteDecision
	if instanceUUID == "" {
		return model.RouterReply{}, fmt.Errorf("no instance available")
	}
	return model.RouterReply{InstanceUUID: instanceUUID}, nil
}

func (r *Tokenpipe) CheckInstanceConvert(instanceUUID string) (bool, error) {
	instanceStatus, ok := r.instanceStatusMap[instanceUUID]
	if !ok {
		return false, fmt.Errorf("%s not in tokenpipe router's instanceStatusMap!", instanceUUID)
	}
	return instanceStatus.IsConvert, nil
}

func (r *Tokenpipe) GetInstanceMaxRequest(instanceUUID string) (int64, error) {
	instanceStatus, ok := r.instanceStatusMap[instanceUUID]
	if !ok {
		return 0, fmt.Errorf("%s not in tokenpipe router's instanceStatusMap!", instanceUUID)
	}
	return instanceStatus.MaxRequest, nil

}

func (r *Tokenpipe) FininshedRequest(ctx context.Context, modelName string, instanceUUID string, routeRequest model.RouterRequest) error {
	r.instanceMapLock.RLock()
	defer r.instanceMapLock.RUnlock()
	communicate, ok := r.instanceMap[modelName]
	if !ok {
		return fmt.Errorf("model %v not registered", modelName)
	}
	finishedRequestInfo := FinishedRequestInfo{
		InstanceUUID:     instanceUUID,
		RouteRequestInfo: routeRequest,
	}
	communicate.FinishedRequest <- finishedRequestInfo
	return nil
}

func (r *Tokenpipe) GetMinTokInstance(ctx context.Context, modelName string) (model.MinTokNumReply, error) {
	r.instanceMapLock.RLock()
	defer r.instanceMapLock.RUnlock()
	communicate, ok := r.instanceMap[modelName]
	if !ok {
		return model.MinTokNumReply{}, fmt.Errorf("model %v not registered", modelName)
	}
	communicate.minTokNumReqChan <- model.MinTokNumRequest{}
	minTokNumReply := <-communicate.minTokNumReplyChan
	return minTokNumReply, nil
}
