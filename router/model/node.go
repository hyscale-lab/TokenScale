package model

type Ports struct {
	ServePort          int
	NixlPeerInitPort   int
	NixlPeerAllocPort  int
	NixlPeerDeletePort int
}

type LMCachePorts struct {
	ServePort              int
	NixlPeerInitPortList   []int
	NixlPeerAllocPortList  []int
	NixlPeerDeletePortList []int
}

type Node struct {
	UUID     string
	GPUUUIDs []string
	Host     string
	Port     int
	IPorts   map[string]Ports
}
