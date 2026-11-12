package config

// SSHConfig represents SSH-related configurations
// Modify this struct according to your specific SSH settings
type SSHConfig struct {
	// Define SSH-related fields if needed
	User           string `json:"user"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	PrivateKeyPath string `json:"private_key_path"`
}

// Config that is gonna to be sweeped in each run
type Sweep = map[string][]interface{}

type ModelConfig struct {
	ModelName            string   `json:"model_name"`
	Cli                  string   `json:"cli"`
	Env                  []string `json:"env"`
	Args                 []string `json:"args"`
	TPArg                string   `json:"tp_arg"`
	HostArg              string   `json:"host_arg"`
	PortArg              string   `json:"port_arg"`
	AutoScalerConfigPath string   `json:"autoscaler_config_path,omitempty"`
	// Following attributes will be inited afterwards
	AutoScalerConfig map[string]interface{} `json:"autoscaler_config"`
	TrafficMonitor   bool                   `json:"traffic_monitor"`
}

type LLMLoadgenConfig struct {
	LLMLoadgenPath string            `json:"LLMLoadgen_path"`
	SamplingRate   int               `json:"sampling_rate"`
	TraceFilePath  string            `json:"trace_file_path"`
	Model          string		     `json:"model"`
	InputLengthRatio float64		 `json:"input_length_ratio"`
	OutputLengthRatio float64		 `json:"output_length_ratio"`
	ExtraParams    map[string]string `json:"extra_params"`
}

type CommonConfig struct {
	RouterPort              int               `json:"router_port"`
	RouterMetricsPort       int               `json:"router_metrics_port"`
	PromWebPort             int               `json:"prom_web_port"`
	ZMQPort                 int               `json:"zmq_port"`
	RoutePolicy             string            `json:"route_policy"`
	RouterTmpDataDirPath    string            `json:"router_tmp_data_dir_path"`
	SifImagesPath           string            `json:"sif_images_path"`
	SqshImagesPath          string            `json:"sqsh_images_path"`
	ModelPath               string            `json:"model_path"`
	MemoryPoolSize          int               `json:"memory_pool_size"`
	LogPath                 string            `json:"log_path"`
	WorkerPort              int               `json:"worker_port"`
	ForkServerPort          int               `json:"fork_server_port"`
	WaitMin                 int               `json:"wait_min"`
	NSCCNodes               int               `json:"nscc_nodes"`
	Hostnames               []string          `json:"hostnames"`
	NSCCPrivateKeyPath      string            `json:"nscc_private_key_path"`
	UpdateTargetRPS         string            `json:"update_target_rps"`
	RouterDisableAutoscaler string            `json:"router_disable_autoscaler"`
	RouterExtraEnv          map[string]string `json:"router_extra_envs"`
	ProvisionRatio          float64           `json:"provision_ratio"`
	GPUPerNode			  	int               `json:"gpu_per_node"`	
	ServingMode				string			  `json:"serving_mode"`
	// will be inherited from index
	ProjectRootPath string `json:"project_root_path"`
}

type PDConfig struct {
	PDMode                      string `json:"pd_mode"`
	MooncakeMasterArgs          string `json:"mooncake_master_args"`
	MooncakePrefillConfigPath   string `json:"mooncake_prefill_config_path"`
	MooncakeDecodeConfigPath    string `json:"mooncake_decode_config_path"`
	LmcacheNixlRecvBufferSizeGB int    `json:"lmcache_nixl_recv_buffer_size_GB"`
	LmcacheNixlSendBufferSizeGB int    `json:"lmcache_nixl_send_buffer_size_GB"`
}

type SidecarConfig struct {
	SidecarType string   `json:"sidecar_type"`
	SidecarArgs []string `json:"args"`
}

// Config for each experiment run
type RunConfig struct {
	CommonConfig      CommonConfig     `json:"common_config"`
	LLMLoadgenConfig  LLMLoadgenConfig `json:"LLMLoadgen_config"`
	PDConfig          PDConfig         `json:"pd_config"`
	SidecarConfigList []SidecarConfig  `json:"sidecar_config"`
	ModelConfigList   []*ModelConfig   `json:"model_config_list"`
}

func NewRunConfig(commonConfig CommonConfig, LLMLoadgenConfig LLMLoadgenConfig, pdConfig PDConfig, sidecarConfigList []SidecarConfig, modelConfigList []*ModelConfig) RunConfig {
	runConfig := RunConfig{
		CommonConfig:      commonConfig,
		LLMLoadgenConfig:  LLMLoadgenConfig,
		PDConfig:          pdConfig,
		SidecarConfigList: sidecarConfigList,
		ModelConfigList:   modelConfigList,
	}
	return runConfig
}

// Config represents the structure of the JSON file
type Index struct {
	ProjectRootPath      string   `json:"project_root_path"`
	ConfigDirPath        string   `json:"config_dir_path"`
	ModelConfigPathList  []string `json:"model_config_path_list"`
	LLMLoadgenConfigPath string   `json:"LLMLoadgen_config_path"`
	PDConfigPath         string   `json:"pd_config_path"`
	SidecarConfigPath    string   `json:"sidecar_config_path"`
	CommonConfigPath     string   `json:"common_config_path"`
}
