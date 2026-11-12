package utils

import (
	"bytes"
	"encoding/json"
	"entrypoints/config"
	"fmt"
	"net/http"
	"strings"
)

func PreloadAllModels(preloadUrl string, modelConfigs []*config.ModelConfig) error {
	preloaded := make(map[string]bool)

	for _, modelConfig := range modelConfigs {
		// Find modelPath after "serve"
		var modelPath string
		for i := 0; i < len(modelConfig.Args)-1; i++ {
			if modelConfig.Args[i] == "serve" {
				modelPath = strings.TrimSpace(modelConfig.Args[i+1])
				break
			}
		}

		if modelPath == "" {
			return fmt.Errorf("could not find model path in args: %v", modelConfig.Args)
		}
		// get the tp level of the model in autoscale configs
		tpVal, ok := modelConfig.AutoScalerConfig["tp_level"]
		if !ok {
			return fmt.Errorf("tp_level not found in AutoScalerConfig")
		}

		tpFloat, ok := tpVal.(float64)
		if !ok {
			return fmt.Errorf("tp_level must be a number, got %T", tpVal)
		}

		tpSize := int(tpFloat)

		// Skip if already preloaded
		if preloaded[modelPath] {
			InfoPrintf("%s has already been preloaded!\n", modelPath)
			continue
		}
		InfoPrintf("Going to preload model %s with TP=%d\n", modelPath, tpSize)

		// Preload model
		if err := preloadModel(preloadUrl, modelPath, tpSize); err != nil {
			return fmt.Errorf("failed to preload model %s: %w", modelPath, err)
		}

		// Record as preloaded
		preloaded[modelPath] = true
	}

	return nil
}

// preloadModel sends a POST request to preloadUrl with a JSON body containing the model path.
func preloadModel(preloadUrl string, modelPath string, tpSize int) error {
	// Create the request body as a map
	body := map[string]any{
		"model_path": modelPath,
		"tensor_parallel_size": tpSize,
	}

	// Encode to JSON
	jsonData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// Send the POST request
	resp, err := http.Post(preloadUrl, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send POST request: %w", err)
	}
	defer resp.Body.Close()

	// Return error if status is not 2xx
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("preload request failed with status code %d", resp.StatusCode)
	}

	return nil
}