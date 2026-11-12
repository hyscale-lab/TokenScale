package model

type RegisterReq struct {
	GPUUUIDs []string `json:"gpu_uuids"`
	Host     string   `json:"host"`
	Port     int      `json:"port"`
}
