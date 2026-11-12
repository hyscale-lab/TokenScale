package deploy

import (
	"entrypoints/config"
	"entrypoints/utils"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)
type PrepareNodeResponse struct {
	Index int
	ModelPath string
}

func PrepareNode(runConfig config.RunConfig, workerProxy *utils.HostProxy, platform string, index int) PrepareNodeResponse {
	remoteModelPath := runConfig.CommonConfig.ModelPath
	if os.Getenv("USE_REMOTE_MODEL") != "" {
		utils.SuccessPrintf("USE_REMOTE_MODEL is set, skipping copy on host %s.\n", workerProxy.IP)
		return PrepareNodeResponse{
			Index:     index,
			ModelPath: remoteModelPath,
		}
	}
	localPath := ""
	if platform == utils.NSCC_2P_PLATFORM_STR {
		localPath = "/raid/pbs.$PBS_JOBID"
		// localPath = "/dev/shm"
	} else if platform == utils.NSCC_2A_PLATFORM_STR {
		localPath = "/dev/shm"
	} else if platform == utils.LOCAL_TEST_PLATFORM_STR {
		localPath = "/dev/shm"
	}
	modelPath := filepath.Join(localPath, "models")

	// --- Check if modelPath exists and is owned by the current user ---
	checkCmd := fmt.Sprintf("if [ -d %s ]; then echo 'EXIST'; else echo 'NOT_EXIST'; fi", modelPath)
	output, err := workerProxy.ExecShellCmd(checkCmd)
	if err == nil {
		exist := strings.TrimSpace(output)

		if exist == "EXIST" {
			utils.SuccessPrintf("Model path %s already exists, skipping copy.\n", modelPath, workerProxy.IP)
			return PrepareNodeResponse{
				Index:     index,
				ModelPath: modelPath,
			}
		}
	}

	// --- Copy model if not owned or doesn't exist ---
	utils.InfoPrintf("Start copy from %s to %s on %s\n", remoteModelPath, localPath, workerProxy.IP)
	copyCmd := fmt.Sprintf("cp -r %s %s", remoteModelPath, localPath)
	start := time.Now()
	_, err = workerProxy.ExecShellCmd(copyCmd)
	utils.CheckErrorWithMsg(err, "failed to execute copy cmd: %s on %s\n", copyCmd, workerProxy.IP)
	elapsed := time.Since(start)
	utils.SuccessPrintf("Copy model weights from %s to %s on %s takes %.2f seconds\n", remoteModelPath, localPath, workerProxy.IP, elapsed.Seconds())

	return PrepareNodeResponse{
		Index:     index,
		ModelPath: modelPath,
	}
}