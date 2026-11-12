package deploy

import (
	"bytes"
	"encoding/json"
	"entrypoints/config"
	"entrypoints/utils"
	"fmt"
	"io"
	"net/http"
)
func RegisterModel(modelConfig *config.ModelConfig, runConfig config.RunConfig, routerIP string) {
	utils.InfoPrintf("Register and preload model: %s\n", modelConfig.ModelName)
	routerRegisterUrl := fmt.Sprintf("http://%s:%d/register_model", routerIP, runConfig.CommonConfig.RouterPort)
	// Set the autoscaler config path to empty to prevent it from marshal
	modelConfig.AutoScalerConfigPath = ""
	jsonData, err := json.Marshal(modelConfig)
	utils.CheckErrorWithMsg(err, "Failed to marshal model JSON: %v\n", err)

	// Create the POST request with JSON body
	resp, err := http.Post(routerRegisterUrl, "application/json", bytes.NewBuffer(jsonData))
	utils.CheckErrorWithMsg(err, "Failed to send POST requests to %s: %v", routerRegisterUrl, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	// Optional: read response
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("model registration failed for %s: status %d, response: %s",
			modelConfig.ModelName, resp.StatusCode, string(body))
		utils.CheckErrorWithMsg(err, "Failed to register model %s: %v\n", modelConfig.ModelName, err)
	}
}