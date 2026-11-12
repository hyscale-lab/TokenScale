package deploy

import (
	"entrypoints/config"
	"entrypoints/utils"
	"fmt"
	"path/filepath"
	"strings"
)

func StartSidecar(runConfig config.RunConfig, runLogPath string, routerProxy *utils.HostProxy, containerType utils.ContainerType) {
	for i, sc := range runConfig.SidecarConfigList {
		if sc.SidecarType == "" {
			utils.InfoPrintf("Sidecar[%d] type not set, skipping...", i)
			continue
		}

		utils.WaitPrintf("Launching sidecar[%d] of type %s...", i, sc.SidecarType)

		var (
			sidecarImagePath string
			params           string
		)

		containerCMD := ""
		switch sc.SidecarType {
		case "distsim":
			// Select image
			switch containerType {
			case utils.SINGULARITY:
				sidecarImagePath = filepath.Join(runConfig.CommonConfig.SifImagesPath, "distsim.sif")
			case utils.ENROOT:
				sidecarImagePath = filepath.Join(runConfig.CommonConfig.SqshImagesPath, "distsim.sqsh")
			}
			// Set params
			params = strings.Join(sc.SidecarArgs, " ")
			containerCMD = "distsim"

		case "aibrix":
			// Select image
			switch containerType {
			case utils.SINGULARITY:
				sidecarImagePath = filepath.Join(runConfig.CommonConfig.SifImagesPath, "aibrix.sif")
			case utils.ENROOT:
				sidecarImagePath = filepath.Join(runConfig.CommonConfig.SqshImagesPath, "aibrix.sqsh")
			}
			// Set params
			params = strings.Join(sc.SidecarArgs, " ")
			containerCMD = "aibrix"

		case "custom_scaler":
			switch containerType {
			case utils.SINGULARITY:
				sidecarImagePath = filepath.Join(runConfig.CommonConfig.SifImagesPath, "custom_scaler.sif")
			case utils.ENROOT:
				sidecarImagePath = filepath.Join(runConfig.CommonConfig.SqshImagesPath, "custom_scaler.sqsh")
			case utils.DOCKER:
				sidecarImagePath = filepath.Join("lrq619/custom_scaler:latest")
			}
			// Set params
			params = strings.Join(sc.SidecarArgs, " ")
			containerCMD = "custom_scaler"
			

		default:
			utils.InfoPrintf("Unknown sidecar type %s, skipping...", sc.SidecarType)
			continue
		}

		sidecarContainerManager, err := utils.NewContainerManager(containerType, sidecarImagePath, params)
		utils.CheckErrorWithMsg(err, "Failed to create sidecar[%d] container!\n", i)

		sidecarContainerCmd := sidecarContainerManager.GetContainerStartCmd(containerCMD)
		sidecarStdoutFilePath := filepath.Join(runLogPath, fmt.Sprintf("sidecar_%d.out", i))
		sidecarStderrFilePath := filepath.Join(runLogPath, fmt.Sprintf("sidecar_%d.err", i))

		utils.InfoPrintf("paras are %s", params)
		utils.InfoPrintf("Starting sidecar[%d] type %s with command: %s\n", i, sc.SidecarType, sidecarContainerCmd)
		routerProxy.ExecShellCmdAsync(sidecarStdoutFilePath, sidecarStderrFilePath, sidecarContainerCmd)
	}
}
