package model

type InstanceStatus struct {
	UUID       string
	ModelName  string
	HostName   string
	Port       int
	GPUIDs     []string
	Timeout    int
	IsConvert  bool
	MaxRequest int64
	Ports      LMCachePorts
	// RouteAPI  string
	Waiting chan struct{}
}
