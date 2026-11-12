package utils

import (
	"encoding/json"
	"entrypoints/config"
	"os"
	"testing"
)

var indexFilePath = "test_configs/index.json"
var sweepFilePath = "test_configs/sweep.json"

func TestBuildRunConfig(t *testing.T) {
	var index config.Index
	file, err := os.ReadFile(indexFilePath)
	if err != nil {
		t.Fatalf("Failed to read %s:%v!", indexFilePath, err)
		return
	}
	err = json.Unmarshal(file, &index)
	if err != nil {
		t.Fatalf("Failed to unmarshal %s:%v!", indexFilePath, err)
		return
	}
	updateParamsMap := make(map[string]interface{})
	updatedCli := "sglang"
	updateParamsMap["model_configs/Llama-3.1-8B.json::cli"] = updatedCli
	runConfig := BuildRunConfig(index, updateParamsMap)
	got := runConfig.ModelConfigList[0].Cli

	if got != updatedCli {
		t.Fatalf("Got %s, expect %s", got, updatedCli)
	}
}

func TestBuildRunConfigs(t *testing.T) {
	var index config.Index
	file, err := os.ReadFile(indexFilePath)
	if err != nil {
		t.Fatalf("Failed to read %s:%v!", indexFilePath, err)
		return
	}
	err = json.Unmarshal(file, &index)
	if err != nil {
		t.Fatalf("Failed to unmarshal %s:%v!", indexFilePath, err)
		return
	}

	
	var sweep config.Sweep
	file, err = os.ReadFile(sweepFilePath)
	if err != nil {
		t.Fatalf("Failed to read %s:%v!", sweepFilePath, err)
		return
	}
	err = json.Unmarshal(file, &sweep)
	if err != nil {
		t.Fatalf("Failed to unmarshal %s:%v!", sweepFilePath, err)
		return
	}

	runConfigs := BuildRunConfigs(index, sweep)
	numRunConfigs := len(runConfigs)

	if numRunConfigs != 3 {
		t.Fatalf("Expected 3 runConfigs, got %d", numRunConfigs)
		return
	}

	samplingRate0 := runConfigs[0].LLMLoadgenConfig.SamplingRate
	samplingRate1 := runConfigs[1].LLMLoadgenConfig.SamplingRate
	samplingRate2 := runConfigs[2].LLMLoadgenConfig.SamplingRate

	if samplingRate0 != 10 {
		t.Fatalf("Expecetd sampling rate to be 10, got %d", samplingRate0)
		return
	}
	if samplingRate1 != 20 {
		t.Fatalf("Expecetd sampling rate to be 20, got %d", samplingRate1)
		return
	}
	if samplingRate2 != 30 {
		t.Fatalf("Expecetd sampling rate to be 30, got %d", samplingRate2)
		return
	}

}
