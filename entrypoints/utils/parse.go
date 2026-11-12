package utils

import (
	"encoding/json"
	"entrypoints/config"
	"os"
	"path"
	"path/filepath"
)

func BuildRunConfigs(index config.Index, sweep config.Sweep) []config.RunConfig {

	runConfigs := []config.RunConfig{}
	updateParamsMaps := GenerateUpdateParamsMaps(sweep)
	if len(updateParamsMaps) == 0 {
		// No need to update, simply construct runConfigs from original cfg dir
		runConfig := buildRunConfigFromIndexAndCfgs(index, index.ConfigDirPath)
		runConfigs := []config.RunConfig{runConfig}
		return runConfigs
	}
	// Need to update
	for _, updateParamsMap := range updateParamsMaps {
		runConfig := BuildRunConfig(index, updateParamsMap)
		runConfigs = append(runConfigs, runConfig)
	}

	return runConfigs
}

func BuildRunConfig(index config.Index, updateParamsMap map[string]interface{}) config.RunConfig {
	// Create a tmp dir, copy all config files into it, modify parameters, then build a runConfig, delete the tmp dir
	tmpDirManager, err := NewTmpDirManager(index.ConfigDirPath)
	CheckErrorWithMsg(err, "Failed to init tmp dir manager!\n")
	defer tmpDirManager.DeleteTmpDir()
	err = tmpDirManager.UpdateParams(updateParamsMap)
	CheckErrorWithMsg(err, "Failed to update params!\n")
	runConfig := buildRunConfigFromIndexAndCfgs(index, tmpDirManager.TmpDirPath())
	return runConfig
}

func buildRunConfigFromIndexAndCfgs(index config.Index, cfgDirPath string) config.RunConfig {
	// overwrite index config with the config file in cfgDirPath, this is to enabling sweeping of index parameters
	updatedIndexFilePath := filepath.Join(cfgDirPath, "index.json")
	file, err := os.ReadFile(updatedIndexFilePath)
	CheckErrorWithMsg(err, "Read updated index file failed!\n")

	// Parse JSON data
	err = json.Unmarshal(file, &index)
	CheckErrorWithMsg(err, "Unmarshal updated index file failed!\n")
	// build common config
	var commonConfig config.CommonConfig
	absCommonConfigFilePath := path.Join(cfgDirPath, index.CommonConfigPath)
	file, err = os.ReadFile(absCommonConfigFilePath)
	CheckErrorWithMsg(err, "Failed to read common config file: %s!\n", absCommonConfigFilePath)
	err = json.Unmarshal(file, &commonConfig)
	CheckErrorWithMsg(err, "Failed to unmarshal common config file: %s!\n", absCommonConfigFilePath)
	commonConfig.ProjectRootPath = index.ProjectRootPath
	commonConfig.ModelPath = os.ExpandEnv(commonConfig.ModelPath)
	commonConfig.SifImagesPath = os.ExpandEnv(commonConfig.SifImagesPath)
	commonConfig.SqshImagesPath = os.ExpandEnv(commonConfig.SqshImagesPath)
	commonConfig.LogPath = os.ExpandEnv(commonConfig.LogPath)
	commonConfig.NSCCPrivateKeyPath = os.ExpandEnv(commonConfig.NSCCPrivateKeyPath)

	// build loadgen config
	var loadgenConfig config.LLMLoadgenConfig
	absLoadgenConfigFilePath := path.Join(cfgDirPath, index.LLMLoadgenConfigPath)
	file, err = os.ReadFile(absLoadgenConfigFilePath)
	CheckErrorWithMsg(err, "Failed to read loadgen config file: %s!\n", absLoadgenConfigFilePath)
	err = json.Unmarshal(file, &loadgenConfig)
	CheckErrorWithMsg(err, "Failed to unmarshal loadgen config file: %s!\n", absLoadgenConfigFilePath)
	loadgenConfig.TraceFilePath = os.ExpandEnv(loadgenConfig.TraceFilePath)
	loadgenConfig.LLMLoadgenPath = os.ExpandEnv(loadgenConfig.LLMLoadgenPath)

	// build pd config
	var pdConfig config.PDConfig
	absPDConfigFilePath := path.Join(cfgDirPath, index.PDConfigPath)
	file, err = os.ReadFile(absPDConfigFilePath)
	CheckErrorWithMsg(err, "Failed to read pd config file: %s\n", absPDConfigFilePath)
	err = json.Unmarshal(file, &pdConfig)
	CheckErrorWithMsg(err, "Failed to unmarshal pd config file: %s!\n", absPDConfigFilePath)

	// build sidecar config
	var sidecarConfigList []config.SidecarConfig
	absSidecarConfigFilePath := path.Join(cfgDirPath, index.SidecarConfigPath)
	file, err = os.ReadFile(absSidecarConfigFilePath)
	CheckErrorWithMsg(err, "Failed to read sidecar config file: %s\n", absSidecarConfigFilePath)
	err = json.Unmarshal(file, &sidecarConfigList)
	CheckErrorWithMsg(err, "Failed to unmarshal sidecar config file: %s", absSidecarConfigFilePath)

	// build model config list, notice that each model config corresponds to a autoscale config
	modelConfigList := []*config.ModelConfig{}
	for _, modelConfigPath := range index.ModelConfigPathList {
		var modelConfig config.ModelConfig
		absModelConfigFilePath := path.Join(cfgDirPath, modelConfigPath)
		file, err = os.ReadFile(absModelConfigFilePath)
		CheckErrorWithMsg(err, "Failed to read model config file: %s\n", absModelConfigFilePath)
		err = json.Unmarshal(file, &modelConfig)

		absAutoScalerConfigFilePath := path.Join(cfgDirPath, modelConfig.AutoScalerConfigPath)
		file, err = os.ReadFile(absAutoScalerConfigFilePath)
		CheckErrorWithMsg(err, "Failed to read autoscaler config file: %s\n", absAutoScalerConfigFilePath)
		json.Unmarshal(file, &modelConfig.AutoScalerConfig)
		CheckErrorWithMsg(err, "Failed to unmarshal autoscaler config file: %s\n", absAutoScalerConfigFilePath)
		modelConfigList = append(modelConfigList, &modelConfig)
	}

	// build runConfig
	runConfig := config.NewRunConfig(commonConfig, loadgenConfig, pdConfig, sidecarConfigList, modelConfigList)
	return runConfig
}

func GenerateUpdateParamsMaps(config config.Sweep) []map[string]interface{} {
	keys := []string{}
	for key := range config {
		keys = append(keys, key)
	}

	var results []map[string]interface{}

	var generate func(int, map[string]interface{})
	generate = func(depth int, current map[string]interface{}) {
		if depth == len(keys) {
			// Deep copy the map before appending
			copyMap := make(map[string]interface{})
			for k, v := range current {
				copyMap[k] = v
			}
			results = append(results, copyMap)
			return
		}

		key := keys[depth]
		for _, val := range config[key] {
			current[key] = val
			generate(depth+1, current)
		}
	}

	generate(0, make(map[string]interface{}))
	return results
}
