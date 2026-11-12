package deploy

import (
	"encoding/json"
	"entrypoints/config"
	"entrypoints/utils"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

func StartRouter(runConfig config.RunConfig, runLogPath string, routerProxy *utils.HostProxy) {
	// Start router on router host
	var err error
	serviceDiscoveryFilePath := filepath.Join(runLogPath, "sd.json")
	_, err = os.Create(serviceDiscoveryFilePath)
	// write runConfig to the json file under runLogPath
	runConfigFilePath := filepath.Join(runLogPath, "config.json")
	jsonData, err := json.MarshalIndent(runConfig, "", " ")
	utils.CheckErrorWithMsg(err, "Marshalize runConfig failed!\n")
	err = os.WriteFile(runConfigFilePath, jsonData, 0644)
	utils.CheckErrorWithMsg(err, "Failed to write runConfig to %s!\n", runConfigFilePath)

	utils.CheckErrorWithMsg(err, "Cannot create service discovery file: %s\n", serviceDiscoveryFilePath)

	utils.WaitPrintf("Starting router...")
	routerPath := filepath.Join(runConfig.CommonConfig.ProjectRootPath, "router")
	routerProxy.Chdir(routerPath)
	routerProxy.Setenv("PROJECT_ROOT_PATH", runConfig.CommonConfig.ProjectRootPath)
	routerProxy.Setenv("PD_MODE", runConfig.PDConfig.PDMode)
	routerProxy.Setenv("ZMQ_PORT", fmt.Sprintf("%d",runConfig.CommonConfig.ZMQPort))
	routerProxy.Setenv("ROUTER_TMP_DIR_PATH", runConfig.CommonConfig.RouterTmpDataDirPath)
	routerProxy.Setenv("DISABLE_AUTOSCALER", runConfig.CommonConfig.RouterDisableAutoscaler)
	routerProxy.Setenv("RUN_LOG_DIR", runLogPath)
	routerProxy.Setenv("SERVING_MODE", runConfig.CommonConfig.ServingMode)
	// enable coldstart profile
	if runConfig.PDConfig.PDMode == "lmcache" {
		routerProxy.Setenv("NIXL_RECV_BUFFER_SIZE_GB", fmt.Sprintf("%d", runConfig.PDConfig.LmcacheNixlRecvBufferSizeGB))
		routerProxy.Setenv("NIXL_SEND_BUFFER_SIZE_GB", fmt.Sprintf("%d", runConfig.PDConfig.LmcacheNixlSendBufferSizeGB))
	}


	routerProxy.Setenv("UPDATE_TARGET_RPS", runConfig.CommonConfig.UpdateTargetRPS)
	if runConfig.CommonConfig.UpdateTargetRPS == "distserve" {
		distSimSingularityPath := filepath.Join(runConfig.CommonConfig.SifImagesPath, "distsim.sif")
		routerProxy.Setenv("DIST_SIM_SINGULARITY_PATH", distSimSingularityPath)
	}
	// convert float64 -> string
	val := strconv.FormatFloat(runConfig.CommonConfig.ProvisionRatio, 'f', -1, 64)
	routerProxy.Setenv("PROVISION_RATIO", val)
	for key := range runConfig.CommonConfig.RouterExtraEnv {
		val := runConfig.CommonConfig.RouterExtraEnv[key]
		routerProxy.Setenv(key, val)
	}

	routerStdoutFilePath := filepath.Join(runLogPath, "router.out")
	routerStderrFilePath := filepath.Join(runLogPath, "router.err")
	webListenAddress := fmt.Sprintf("0.0.0.0:%d", runConfig.CommonConfig.PromWebPort)
	routerProxy.ExecShellCmdAsync(routerStdoutFilePath, routerStderrFilePath, "ROUTER_HOST=%s ROUTER_PORT=%d SD_FILE=%s METRICS_PORT=%d PROMETHEUS_ADDR=%s ROUTE_POLICY=%s ./router", routerProxy.IP, runConfig.CommonConfig.RouterPort, serviceDiscoveryFilePath, runConfig.CommonConfig.RouterMetricsPort, webListenAddress, runConfig.CommonConfig.RoutePolicy)
}


