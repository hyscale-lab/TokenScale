package deploy

import (
	"entrypoints/config"
	"entrypoints/utils"
	"fmt"
	"os"
	"path/filepath"
)

func StartPrometheus(runConfig config.RunConfig, runLogPath string, routerProxy *utils.HostProxy, containerType utils.ContainerType) {
	serviceDiscoveryFilePath := filepath.Join(runLogPath, "sd.json")
	webListenAddress := fmt.Sprintf("0.0.0.0:%d", runConfig.CommonConfig.PromWebPort)

	var prometheusImagePath string
	switch containerType {
		case utils.SINGULARITY:
			prometheusImagePath = filepath.Join(runConfig.CommonConfig.SifImagesPath, "prometheus_v3.2.1.sif")
	
		case utils.ENROOT:
			prometheusImagePath = filepath.Join(runConfig.CommonConfig.SqshImagesPath, "prometheus_v3.2.1.sqsh")
		case utils.DOCKER:
			prometheusImagePath = "prom/prometheus:v3.2.1"	
	}
	prometheusConfigPath := filepath.Join(runConfig.CommonConfig.ProjectRootPath, "prometheus/prometheus.yml")
	tsdbFilePath := filepath.Join(runLogPath, "metrics")
	_ = os.Mkdir(tsdbFilePath, 0755)
	promStdoutFilePath := filepath.Join(runLogPath, "prom.out")
	promStderrFilePath := filepath.Join(runLogPath, "prom.err")

	params := fmt.Sprintf("--config.file=/etc/prometheus/prometheus.yml --storage.tsdb.path=%s --web.enable-admin-api --web.listen-address=%s", tsdbFilePath, webListenAddress)
	promContainerManager, err := utils.NewContainerManager(containerType, prometheusImagePath, params)
	utils.CheckErrorWithMsg(err, "Failed to create container manager for prometheus!\n")

	// promContainerManager.AddNewMount("/scratch/users/ntu/$USER","/scratch/users/ntu/$USER")
	promContainerManager.AddNewMount(tsdbFilePath, tsdbFilePath)
	promContainerManager.AddNewMount(prometheusConfigPath, "/etc/prometheus/prometheus.yml")
	promContainerManager.AddNewMount(serviceDiscoveryFilePath, "/etc/prometheus/sd.json")
	promContainerCmd := promContainerManager.GetContainerStartCmd(utils.PROMETHEUS_CONTAINER_NAME)
	utils.DebugPrintf("Starting prometheus commands: %s\n", promContainerCmd)
	
	routerProxy.ExecShellCmdAsync(promStdoutFilePath, promStderrFilePath, promContainerCmd)
} 