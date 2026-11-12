package config

type GlobalConfig struct {
	RouterHost *string `json:"router_host"`
	RouterPort *int    `json:"router_port"`
}

var Config *GlobalConfig
