package deploy

import (
	"entrypoints/config"
	"entrypoints/utils"
	"fmt"
	"path/filepath"
)


func StartLiquidlet(runConfig config.RunConfig, runLogPath string, workerProxy *utils.HostProxy, containerType utils.ContainerType, routerIP string, i int, platform string, prepareNodeResponse PrepareNodeResponse) {
	daemonPath := filepath.Join(runConfig.CommonConfig.ProjectRootPath, "liquidlet")
	var liquidletImagePath string
	switch containerType {
	case utils.SINGULARITY:
		liquidletImagePath = filepath.Join(runConfig.CommonConfig.SifImagesPath, "liquidlet.sif")

	case utils.ENROOT:
		liquidletImagePath = filepath.Join(runConfig.CommonConfig.SqshImagesPath, "liquidlet.sqsh")
	case utils.DOCKER:
		liquidletImagePath = filepath.Join("lrq619/liquidlet:latest")
	}
	params := fmt.Sprintf("--mem-pool-size=%dGB", runConfig.CommonConfig.MemoryPoolSize)
	liquidletContainerManager, err := utils.NewContainerManager(containerType, liquidletImagePath, params)
	utils.CheckErrorWithMsg(err, "Failed to create new container manager for liquidlet")
	workerProxy.Chdir(daemonPath)


	// set env
	liquidletContainerManager.AddNewEnv("ROUTER", fmt.Sprintf("http://%s:%d/register_node", routerIP, runConfig.CommonConfig.RouterPort))
	liquidletContainerManager.AddNewEnv("STORAGE_PATH", "/models")
	liquidletContainerManager.AddNewEnv("DAEMON_HOST", workerProxy.IP)
	liquidletContainerManager.AddNewEnv("DAEMON_PORT", fmt.Sprintf("%d", runConfig.CommonConfig.WorkerPort))
	// liquidletContainerManager.AddNewEnv("PD_MODE", runConfig.PDConfig.PDMode)
	liquidletContainerManager.AddNewEnv("PD_MODE", "")
	liquidletContainerManager.AddNewEnv("FORKSERVER_PORT", fmt.Sprintf("%d", runConfig.CommonConfig.ForkServerPort))
	liquidletContainerManager.AddNewEnv("RUN_LOG_DIR", runLogPath)
	liquidletContainerManager.AddNewMount(runLogPath, "/run_log_dir")
	// set mount
	liquidletContainerManager.AddNewMount("/scratch/users/ntu/$USER", "/scratch/users/ntu/$USER")
	liquidletContainerManager.AddNewMount(prepareNodeResponse.ModelPath, "/models")
	// vllm debug scripts
	// liquidletContainerManager.AddNewMount("/path/to/Liquid/debug/vllm/uniproc_executor.py", "/opt/conda/lib/python3.11/site-packages/vllm/executor/uniproc_executor.py")
	// liquidletContainerManager.AddNewMount(filepath.Join(runConfig.CommonConfig.ProjectRootPath, "debug/vllm/gpu_worker.py"), "/opt/conda/lib/python3.11/site-packages/vllm/v1/worker/gpu_worker.py")
	// liquidletContainerManager.AddNewMount("/path/to/Liquid/debug/vllm/kv_transfer_state.py", "/opt/conda/lib/python3.11/site-packages/vllm/distributed/kv_transfer/kv_transfer_state.py")
	// liquidletContainerManager.AddNewMount("/path/to/Liquid/debug/vllm/__init__.py", "/opt/conda/lib/python3.11/site-packages/vllm/distributed/kv_transfer/__init__.py")
	// liquidletContainerManager.AddNewMount("/path/to/Liquid/debug/vllm/lmcache_connector.py", "/opt/conda/lib/python3.11/site-packages/vllm/distributed/kv_transfer/kv_connector/v1/lmcache_connector.py")
	// liquidletContainerManager.AddNewMount(filepath.Join(runConfig.CommonConfig.ProjectRootPath, "debug/vllm/arg_utils.py"), "/opt/conda/lib/python3.11/site-packages/vllm/engine/arg_utils.py")
	// liquidletContainerManager.AddNewMount(filepath.Join(runConfig.CommonConfig.ProjectRootPath, "debug/vllm/core.py"), "/opt/conda/lib/python3.11/site-packages/vllm/v1/engine/core.py")
	// liquidletContainerManager.AddNewMount(filepath.Join(runConfig.CommonConfig.ProjectRootPath, "debug/vllm/multiproc_executor.py"), "/opt/conda/lib/python3.11/site-packages/vllm/v1/executor/multiproc_executor.py")
	// liquidletContainerManager.AddNewMount(filepath.Join(runConfig.CommonConfig.ProjectRootPath, "debug/vllm/coldstart_profiler.py"), "/opt/conda/lib/python3.11/site-packages/vllm/coldstart_profiler.py")
	// liquidletContainerManager.AddNewMount(filepath.Join(runConfig.CommonConfig.ProjectRootPath, "debug/vllm/coldstart_utils.py"), "/opt/conda/lib/python3.11/site-packages/vllm/coldstart_utils.py")
	// liquidletContainerManager.AddNewMount(filepath.Join(runConfig.CommonConfig.ProjectRootPath, "debug/vllm/registry.py"), "/opt/conda/lib/python3.11/site-packages/vllm/model_executor/models/registry.py")
	// // lmcache debug scripts
	// liquidletContainerManager.AddNewMount(filepath.Join(runConfig.CommonConfig.ProjectRootPath, "debug/lmcache/vllm_v1_adapter.py"), "/opt/conda/lib/python3.11/site-packages/lmcache/integration/vllm/vllm_v1_adapter.py")
	// liquidletContainerManager.AddNewMount(filepath.Join(runConfig.CommonConfig.ProjectRootPath, "debug/lmcache/cache_engine.py"), "/opt/conda/lib/python3.11/site-packages/lmcache/v1/cache_engine.py")
	// liquidletContainerManager.AddNewMount(filepath.Join(runConfig.CommonConfig.ProjectRootPath, "debug/lmcache/gpu_connector.py"), "/opt/conda/lib/python3.11/site-packages/lmcache/v1/gpu_connector.py")
	// liquidletContainerManager.AddNewMount(filepath.Join(runConfig.CommonConfig.ProjectRootPath, "debug/lmcache/nixl_connector_v3.py"), "/opt/conda/lib/python3.11/site-packages/lmcache/v1/storage_backend/connector/nixl_connector_v3.py")
	// liquidletContainerManager.AddNewMount(filepath.Join(runConfig.CommonConfig.ProjectRootPath, "debug/lmcache/nixl_agent_pool.py"), "/opt/conda/lib/python3.11/site-packages/lmcache/v1/nixl_agent_pool.py")
	// liquidletContainerManager.AddNewMount(filepath.Join(runConfig.CommonConfig.ProjectRootPath, "debug/lmcache/nixl_backend_v3.py"), "/opt/conda/lib/python3.11/site-packages/lmcache/v1/storage_backend/nixl_backend_v3.py")
	// liquidletContainerManager.AddNewMount(filepath.Join(runConfig.CommonConfig.ProjectRootPath, "debug/lmcache/memory_management.py"), "/opt/conda/lib/python3.11/site-packages/lmcache/v1/memory_management.py")
	// // forkserver
	// liquidletContainerManager.AddNewMount(filepath.Join(runConfig.CommonConfig.ProjectRootPath, "debug/preload.py"), "/app/preload.py")
	// liquidletContainerManager.AddNewMount(filepath.Join(runConfig.CommonConfig.ProjectRootPath, "debug/forkserver.py"), "/app/liquidlet/forkserver/forkserver.py")
	// pd disagg
	// liquidletContainerManager.AddNewEnv("UCX_IB_FORK_INIT", "no")
	if runConfig.PDConfig.PDMode == "mooncake" {
		liquidletContainerManager.AddNewEnv("MOONCAKE_MASTER_ARGS", runConfig.PDConfig.MooncakeMasterArgs)
		etcdDataDir := filepath.Join(runLogPath, "etcd-data")
		liquidletContainerManager.AddNewEnv("ETCD_DATA_DIR", etcdDataDir)
		mooncakePrefillConfigPath := filepath.Join(runConfig.CommonConfig.ProjectRootPath, "entrypoints/configs", runConfig.PDConfig.MooncakePrefillConfigPath)
		mooncakeDecodeConfigPath := filepath.Join(runConfig.CommonConfig.ProjectRootPath, "entrypoints/configs", runConfig.PDConfig.MooncakeDecodeConfigPath)
		liquidletContainerManager.AddNewMount(mooncakePrefillConfigPath, "/app/mooncake_prefill.json")
		liquidletContainerManager.AddNewMount(mooncakeDecodeConfigPath, "/app/mooncake_decode.json")
	} else if runConfig.PDConfig.PDMode == "lmcache" {
		liquidletContainerManager.AddNewEnv("NIXL_RECV_BUFFER_SIZE_GB", fmt.Sprintf("%d", runConfig.PDConfig.LmcacheNixlRecvBufferSizeGB))
	}
	liquidletContainerCmd := liquidletContainerManager.GetContainerStartCmd(utils.LIQUIDLET_CONTAINER_NAME)
	utils.DebugPrintf("starting liquidlet commands: %s", liquidletContainerCmd)
	workerStdoutFilePath := filepath.Join(runLogPath, fmt.Sprintf("worker_%d.out", i))
	workerStderrFilePath := filepath.Join(runLogPath, fmt.Sprintf("worker_%d.err", i))
	workerProxy.ExecShellCmdAsync(workerStdoutFilePath, workerStderrFilePath, liquidletContainerCmd)
	utils.InfoPrintf("liquidlet started on %s!\n", workerProxy.IP)

}
