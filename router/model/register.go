package model

import (
	"errors"
	"fmt"
	"strings"
)

var (
	DEFAULTSTR      = "DEFAULT"
	HPASTR          = "HPA"
	KPASTR          = "KPA"
	RPSSTR          = "RPS"
	KPAOPTIMIZERSTR = "KPA_OPTIMIZER"
	CAPSTR          = "CAP"
)

type RegisterNode struct {
	GPUUUIDs []string `json:"gpu_uuids"`
	Host     string   `json:"host"`
	Port     int      `json:"port"`
}

type RegisterModel struct {
	ModelName string   `json:"model_name"`
	Cli       string   `json:"cli"`
	Env       []string `json:"env"`
	Args      []string `json:"args"`
	TPArg     string   `json:"tp_arg"`
	HostArg   string   `json:"host_arg"`
	PortArg   string   `json:"port_arg"`
	// RouteAPI  string          `json:"route_api"`
	AutoScalerConfig map[string]interface{} `json:"autoscaler_config"`
	TrafficMonitor   bool                   `json:"traffic_monitor"`
}

func GetModelNameAndType(modelNameRaw string) (string, string, error) {
	parts := strings.Split(modelNameRaw, "::")

	modelName := strings.TrimSpace(parts[0])
	if modelName == "" {
		return "", "", errors.New("model name cannot be empty")
	}

	// If not in <modelName>::<instanceType> format, default to "aggregation"
	if len(parts) != 2 {
		return modelName, "aggregation", nil
	}

	instanceType := strings.TrimSpace(parts[1])
	if instanceType != "prefill" && instanceType != "decode" {
		return "", "", fmt.Errorf("invalid instance type: %s (must be 'prefill' or 'decode')", instanceType)
	}

	return modelName, instanceType, nil

}

type DefaultAutoScalerModelConfig struct {
	Type                       string `json:"type"`
	TPLevel                    int    `json:"tp_level"`
	TargetMetric               string `json:"target_metric"`
	TargetValue                int64  `json:"target_value"`
	MinInstance                int64  `json:"min_instance"`
	MaxInstance                int64  `json:"max_instance"`
	MinLifeTime                int64  `json:"min_life_time"`
	ScaleDownToZeroGracePeriod int64  `json:"scale_down_to_zero_grace_period"`
}

type HPAAutoScalerModelConfig struct {
	Type                       string  `json:"type"`
	TPLevel                    int     `json:"tp_level"`
	PromMetric                 string  `json:"prom_metric"`
	MinInstance                int64   `json:"min_instance"`
	MaxInstance                int64   `json:"max_instance"`
	TargetUtilization          int64   `json:"target_utilization"`
	SamplingInterval           int64   `json:"sampling_interval"`
	StabilizationWindowSeconds int64   `json:"stabilization_window_seconds"`
	UpTolerance                float64 `json:"up_tolerance"`
	DownTolerance              float64 `json:"down_tolerance"`
}

type KPAAutoScalerModelConfig struct {
	DefaultAutoScalerModelConfig
	StabilizationWindowSeconds int64 `json:"stabilization_window_seconds"`
}

type KPAOptimizerAutoScalerModelConfig struct {
	Type                       string `json:"type"`
	TPLevel                    int    `json:"tp_level"`
	MaxRequestPerInstance      int64  `json:"max_request_per_instance"`
	MinInstance                int64  `json:"min_instance"`
	MaxInstance                int64  `json:"max_instance"`
	MinLifeTime                int64  `json:"min_life_time"`
	ScaleDownToZeroGracePeriod int64  `json:"scale_down_to_zero_grace_period"`
	StableWindowSeconds        int64  `json:"stable_window_seconds"`
	OptimizerHost              string `json:"optimizer_host"`
	OptimizerPort              int    `json:"optimizer_port"`
}

type RPSAutoScalerModelConfig struct {
	Type                       string  `json:"type"`
	TPLevel                    int     `json:"tp_level"`
	TargetRPS                  float64 `json:"target_rps"`
	MinInstance                int64   `json:"min_instance"`
	MaxInstance                int64   `json:"max_instance"`
	MinLifeTime                int64   `json:"min_life_time"`
	ScaleDownToZeroGracePeriod int64   `json:"scale_down_to_zero_grace_period"`
	DistsimUrl                 string  `json:"distsim_url"`
	PrefillTargetMs            int64   `json:"prefill_target_ms"`
	DecodeTargetMs             int64   `json:"decode_target_ms"`
}

type CAPAutoScalerModelConfig struct {
	Type                       string  `json:"type"`
	TPLevel                    int     `json:"tp_level"`
	MinInstance                int64   `json:"min_instance"`
	MaxInstance                int64   `json:"max_instance"`
	MinLifeTime                int64   `json:"min_life_time"`
	ScaleDownToZeroGracePeriod int64   `json:"scale_down_to_zero_grace_period"`
	StabilizationWindowSeconds int64   `json:"stabilization_window_seconds"`
	CapacityProfileFilePath    string  `json:"capacity_profile_file_path"`
	ConvertInstance			   int64   `json:"convert_instance"`
	ConvertMaxRequest		   int64   `json:"convert_max_request"`
}
