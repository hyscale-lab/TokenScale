package main

import (
	"encoding/json"
	"entrypoints/config"
	"entrypoints/deploy"
	"entrypoints/utils"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

var (
	experimentLogPath = ""
)

func main() {
	// Init logger
	loc, err := time.LoadLocation("Asia/Singapore")
	utils.CheckErrorWithMsg(err, "Load time zone wrong!\n")
	formattedTime := time.Now().In(loc).Format("2006-01-02-15-04-05")
	shortFormatterTime := time.Now().In(loc).Format("150405")

	experimentName := fmt.Sprintf("e%s", shortFormatterTime)
	utils.ExperimentName = experimentName

	utils.InitLogger()
	platform := os.Getenv("PLATFORM")
	if platform == "" {
		utils.CheckErrorWithMsg(nil, "Must set PLATFORM")
	}
	utils.Platform = platform

	_, ok := utils.PLATFORM_TO_CONTAINER_MAP[platform]
	if !ok {
		utils.CheckErrorWithMsg(nil, "PLATFORM %s not supported yet!", platform)
	}

	// Define command-line flag
	indexPath := flag.String("index_path", "configs/index.json", "Path to the JSON index file")
	sweepPath := flag.String("sweep_path", "configs/sweep.json", "Path to the JSON sweep file")
	flag.Parse()

	// Read the JSON file
	file, err := os.ReadFile(*indexPath)
	utils.CheckErrorWithMsg(err, "Read json file failed!\n")

	// Parse JSON data
	var index config.Index
	err = json.Unmarshal(file, &index)
	utils.CheckErrorWithMsg(err, "Unmarshal json file failed!\n")
	if index.ConfigDirPath == "" {
		index.ConfigDirPath = path.Join(index.ProjectRootPath, "entrypoints/configs")
	}
	// Expand environment env ($USER)
	index.ProjectRootPath = os.ExpandEnv(index.ProjectRootPath)
	index.ConfigDirPath = os.ExpandEnv(index.ConfigDirPath)

	// Print the parsed index (for debugging)
	utils.InfoPrintf("Parsed Index: %+v\n", index)

	file, err = os.ReadFile(*sweepPath)
	utils.CheckErrorWithMsg(err, "Read the sweep file failed!\n")

	// Parse sweep JSON data
	var sweep config.Sweep
	err = json.Unmarshal(file, &sweep)
	utils.CheckErrorWithMsg(err, "Unmarhsal sweep json file failed!\n")

	// Generate runConfigs with sweep file
	runConfigs := utils.BuildRunConfigs(index, sweep)
	commonConfig := runConfigs[0].CommonConfig

	plotsPath := filepath.Join(runConfigs[0].LLMLoadgenConfig.LLMLoadgenPath, "plots/wasserstein_tokens.png")
	err = os.Remove(plotsPath)
	if err != nil {
		utils.InfoPrintf("Failed to remove plots file! %v\n", err)
	}


	experimentLogPath = filepath.Join(commonConfig.LogPath, fmt.Sprintf("logs_%s", formattedTime))
	err = os.Mkdir(experimentLogPath, 0755)
	utils.CheckErrorWithMsg(err, "Failed to mkdir on path %s:%v", experimentLogPath, err)
	// Write sweep.json into experiment log path
	experimentSweepPath := path.Join(experimentLogPath, "sweep.json")
	jsonData, err := json.MarshalIndent(sweep, "", " ")
	utils.CheckErrorWithMsg(err, "Failed to marshalize sweep into json!\n")
	err = os.WriteFile(experimentSweepPath, jsonData, 0644)
	utils.CheckErrorWithMsg(err, "Failed to write to file: %s!\n", experimentSweepPath)
	// Create host proxys for local node(login node) and worker nodes
	utils.LocalProxy, err = utils.NewHostProxy("localhost", nil)
	utils.CheckErrorWithMsg(err, "Create router host proxy failed!")

	// Start to run experiments
	numRuns := len(runConfigs)
	utils.InfoPrintf("Going to run %d runs in the experiment\n", numRuns)

	var wg sync.WaitGroup
	wg.Add(numRuns)

	// Channel to listen for signals
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	done := make(chan struct{})

	// Signal listener
	go func() {
		<-sigs
		utils.InfoPrintf("Received Ctrl+C! Exiting...")
		utils.ExitProgram(experimentName)
		close(done)
	}()
	for runIndex, runConfig := range runConfigs {
		utils.InfoPrintf("Going to start the %d'th run\n", runIndex)
		go func(runConfig config.RunConfig, runIndex int) {
			defer wg.Done()
			Run(runConfig, runIndex, platform, experimentName)
		}(runConfig, runIndex)
		time.Sleep(60*time.Second)
	}
	// Another goroutine: wait until all runs finished
	go func() {
		wg.Wait()
		utils.InfoPrintf("All %d runs finished!\n", numRuns)
		utils.InfoPrintf("Experiments results are saved to: %s\n", experimentLogPath)
		utils.ExitProgram(experimentName)
		close(done)
	}()

	// Block main thread until done
	<-done

}

func ReserveNodes(runConfig config.RunConfig, iter int, platform string, experimentName string) ([]string, string) {
	utils.LocalProxy.Chdir(runConfig.CommonConfig.ProjectRootPath)
	var out string
	var err error
	runName := fmt.Sprintf("%sr%d", experimentName, iter)
	if platform == utils.NSCC_2A_PLATFORM_STR {
		out, err = utils.LocalProxy.ExecShellCmd("qsub -l select=%d:ngpus=%d -N %s scripts/reserve_nodes.pbs", runConfig.CommonConfig.NSCCNodes, runConfig.CommonConfig.GPUPerNode, runName)
		utils.CheckErrorWithMsg(err, "Failed to reserve nodes on %s!\n", platform)
	} else if platform == utils.NSCC_2P_PLATFORM_STR {
		out, err = utils.LocalProxy.ExecShellCmd("qsub -l select=%d:ngpus=%d:container_engine=enroot -N %s scripts/reserve_nodes_2p.pbs", runConfig.CommonConfig.NSCCNodes, runConfig.CommonConfig.GPUPerNode, runName)
		utils.CheckErrorWithMsg(err, "Failed to reserve nodes on %s!\n", platform)
	} else if platform == utils.LOCAL_TEST_PLATFORM_STR {
		return runConfig.CommonConfig.Hostnames, ""
	} else {
		err = errors.New("failed to reserve nodes on platform")
		utils.CheckErrorWithMsg(err, "Platform %s unsupported!", platform)
	}
	pbsJobID := out
	utils.WaitPrintf("Waiting for %d nodes from NSCC! PBS job id: %s\n", runConfig.CommonConfig.NSCCNodes, pbsJobID)
	// Wait for the job to be executed
	jobReady := false
	for jobReady != true {
		time.Sleep(1 * time.Second)
		out, err = utils.LocalProxy.ExecShellCmd("qstat -f %s 2>/dev/null | awk -F' = ' '/job_state/ {print $2}'", pbsJobID)
		if out == "R" {
			jobReady = true
		}
	}
	os.Setenv("PBS_JOBID", pbsJobID)
	utils.SuccessPrintf("Successfully rent %d nodes from NSCC!\n", runConfig.CommonConfig.NSCCNodes)
	// Get the node's host names
	out, err = utils.LocalProxy.ExecShellCmd(`qstat -f %s | awk '
		/exec_host/ { capture=1; out=$0; next }
		/exec_vnode/ { capture=0 }
		capture { out = out $0 }
		END {
		sub(/^exec_host = /, "", out)
		gsub(/[[:space:]]+/, "", out)
		print out
		}'`, pbsJobID)
	hostnames := utils.ExtractHostnames(out)
	return hostnames, pbsJobID
}

func Run(runConfig config.RunConfig, iter int, platform string, experimentName string) {
	runLogPath := filepath.Join(experimentLogPath, fmt.Sprintf("run_%d", iter))
	err := os.Mkdir(runLogPath, 0755)
	utils.CheckErrorWithMsg(err, "Mkdir %s failed!\n", runLogPath)

	hostnames, pbsJobID := ReserveNodes(runConfig, iter, platform, experimentName)
	utils.DebugPrintf("Reserved hostnames: [")
	for _, hostname := range hostnames {
		utils.DebugPrintf("%s, ", hostname)
	}
	utils.DebugPrintf("]\n")
	time.Sleep(10 * time.Second) // Sleep for 15 seconds to wait for sshd to start
	// Create host proxy on every rented nodes
	workerProxys := []*utils.HostProxy{}
	for i, hostname := range hostnames {
		sshConfig := config.SSHConfig{
			User:           os.Getenv("USER"),
			Host:           hostname,
			Port:           22,
			PrivateKeyPath: runConfig.CommonConfig.NSCCPrivateKeyPath,
		}

		workerProxy, err := utils.NewHostProxy(hostname, &sshConfig)
		workerProxy.SetpbsJobID(pbsJobID)
		if platform == "NSCC_2P" {
			workerProxy.Setenv("NO_PROXY", "*")
		}
		utils.CheckErrorWithMsg(err, "Create the %d'th worker proxy failed!\n", i)
		workerProxys = append(workerProxys, workerProxy)
	}
	// Determine container type
	containerType, ok := utils.PLATFORM_TO_CONTAINER_MAP[platform]
	if !ok {
		utils.CheckErrorWithMsg(nil, "PLATFORM %s not supported yet!", platform)
	}
	// select the firs worker nodes as router node
	routerProxy := workerProxys[0]

	// Start router on router host
	utils.WaitPrintf("Starting router...")
	deploy.StartRouter(runConfig, runLogPath, routerProxy)
	utils.SuccessPrintf("Router started!\n")

	// Start prometheus
	utils.WaitPrintf("Starting prometheus...")
	deploy.StartPrometheus(runConfig, runLogPath, routerProxy, containerType)
	utils.SuccessPrintf("Prometheus started!\n")

	// Start router sidecar (simulator, optimizer ...)
	deploy.StartSidecar(runConfig, runLogPath, routerProxy, containerType)
	utils.SuccessPrintf("Sidecar started!\n")

	// Wait for router to be started
	utils.WaitPrintf("Wait for router to be ready...")
	routerInfoURL := fmt.Sprintf("http://%s:%d/get_router_info", routerProxy.IP, runConfig.CommonConfig.RouterPort)
	utils.WaitRouterReady(routerInfoURL)
	utils.SuccessPrintf("Router ready!\n")

	// Prepare node (copy model weights to local dir, etc)
	utils.WaitPrintf("Starting prepare nodes ...")
	prepareNodeResponseChan := make(chan deploy.PrepareNodeResponse, len(workerProxys))
	for index, workerProxy := range workerProxys {
		utils.WaitPrintf("Going to prepare node %s...", workerProxy.IP)
		go func(proxy *utils.HostProxy) {
			prepareNodeResponse := deploy.PrepareNode(runConfig, proxy, platform, index)
			prepareNodeResponseChan <- prepareNodeResponse
		}(workerProxy)
	}
	// Wait for all workers to be prepared and start liquidlet on worker hosts
	utils.WaitPrintf("Starting liquidlet on workers ...") 
	for i:= 0; i<len(workerProxys); i++ {
		prepareNodeResponse := <- prepareNodeResponseChan
		index := prepareNodeResponse.Index
		// get the corresponding worker proxy, althrough currently no use, but in the future, can pass information
		workerProxy := workerProxys[index]
		utils.InfoPrintf("Get prepare node resposne from %s, going to start liquidlet on it...\n", workerProxy.IP)
		deploy.StartLiquidlet(runConfig, runLogPath, workerProxy, containerType, routerProxy.IP, i, platform, prepareNodeResponse)
	}
	utils.SuccessPrintf("Liquidlet all started!\n")

	// Wait for all worker nodes are connected
	utils.WaitPrintf("Waiting for %d worker nodes to be ready...\n", len(workerProxys))
	utils.WaitNodesReady(len(workerProxys), routerInfoURL)
	utils.SuccessPrintf("All worker nodes are connected!\n")

	time.Sleep(15 * time.Second)

	// Preload all models from disk to cpu memory
	utils.WaitPrintf("Preload all models on all liquidlet...\n")
	deploy.StartPreloadModels(runConfig, workerProxys)
	utils.SuccessPrintf("Preload finished!\n")

	// Register model
	utils.WaitPrintf("Register models...")
	routerProxy.Chdir(runConfig.CommonConfig.ProjectRootPath)

	for _, modelConfig := range runConfig.ModelConfigList {
		deploy.RegisterModel(modelConfig, runConfig, routerProxy.IP)
	}
	utils.SuccessPrintf("Register models success!\n")

	// Wait for all models to have min_instance
	for _, modelConfig := range runConfig.ModelConfigList {
		utils.WaitModelInstancesReady(modelConfig, routerInfoURL)
	}
	// time.Sleep(300 * time.Second)
	// Start LLMLoadgen
	utils.WaitPrintf("Starting LLMLoadgen...")
	deploy.StartLLMLoadgen(runConfig, runLogPath, routerProxy)
	utils.SuccessPrintf("LLMLoadgen started! Run %d is waiting for %d minutes.\n", iter, runConfig.CommonConfig.WaitMin)

	// Wait for wait_min minutes
	waitDuration := time.Duration(runConfig.CommonConfig.WaitMin) * time.Minute
	// Create a channel to listen for OS signals (Ctrl+C)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Create a timer for sleeping
	timer := time.NewTimer(waitDuration)

	// Wait for the timer to be finished
	select {
	case <-timer.C:
		// Timer completed
		utils.SuccessPrintf("Run %d finished!\n", iter)
		routerProxy.Setenv("MOONCAKE_MASTER_ARGS",  "0")
		prestopScriptPath := filepath.Join(runConfig.CommonConfig.ProjectRootPath, "prometheus/pre-stop.sh")
		routerProxy.ExecShellCmd(prestopScriptPath)
		utils.ExitRun(pbsJobID)
	}
}
