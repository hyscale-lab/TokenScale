package model

type ExceedCapacityQuery struct {
	PrefillInstanceNum int64
	RespChan           chan bool
}

type InstanceNumQuery struct {
	RespChan chan int64
}

type PrefillTokenVelocityQuery struct {
	RespChan chan int64
}

type ScalerChan struct {
	ModelName          string
	ScalerType         string
	ScaleUpDecision    chan AllocateRequest
	ScaleUpResult      chan interface{}
	ScaleDownDecision  chan string
	ScaleDownResult    chan interface{}
	NewRequest         chan ScalerRequest
	FinishedRequest    chan ScalerRequest
	ExceedCapacityChan chan ExceedCapacityQuery
	PrefillTokenVelocityChan chan  PrefillTokenVelocityQuery
	InstanceNumChan    chan InstanceNumQuery
}

func (s *ScalerChan) GetInstanceNum() int64 {
	respChan := make(chan int64)
	query := InstanceNumQuery{RespChan: respChan}
	s.InstanceNumChan <- query
	ins := <-respChan
	return ins
}

func (s *ScalerChan) CheckPrefillCapacityExceed(prefillInstanceNum int64) bool {
	respChan := make(chan bool)
	query := ExceedCapacityQuery{
		PrefillInstanceNum: prefillInstanceNum,
		RespChan:           respChan,
	}
	s.ExceedCapacityChan <- query
	isExceed := <-respChan
	return isExceed
}

func (s *ScalerChan) GetPrefillTokenVelocity() int64 {
	respChan := make(chan int64)
	query := PrefillTokenVelocityQuery{
		RespChan: respChan,
	}
	s.PrefillTokenVelocityChan <- query
	prefillTokenVelocity := <- respChan
	return prefillTokenVelocity
}
