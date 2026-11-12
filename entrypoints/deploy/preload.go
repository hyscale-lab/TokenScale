package deploy

import (
	"entrypoints/config"
	"entrypoints/utils"
	"fmt"
	"time"
)
type PreloadResponse struct {
    IP  string
    Err error
}

func StartPreloadModels(runConfig config.RunConfig, workerProxys []*utils.HostProxy) {
    responseChan := make(chan PreloadResponse, len(workerProxys))

    for _, workerProxy := range workerProxys {
        utils.WaitPrintf("Going to preload all models on %s...\n", workerProxy.IP)

        go func(proxy *utils.HostProxy) {
            preloadURL := fmt.Sprintf("http://%s:%d/preload",
                proxy.IP, runConfig.CommonConfig.WorkerPort)

            err := utils.PreloadAllModels(preloadURL, runConfig.ModelConfigList)

            responseChan <- PreloadResponse{IP: proxy.IP, Err: err}
        }(workerProxy)

        // Sleep between launches to avoid overwhelming storage/network
        time.Sleep(1 * time.Second)
    }

    // Collect results
    successCount := 0
    for i := 0; i < len(workerProxys); i++ {
        resp := <-responseChan
        if resp.Err != nil {
            utils.CheckErrorWithMsg(resp.Err, "Preload failed on %s: %v\n", resp.IP, resp.Err)
        } else {
            utils.InfoPrintf("Preload succeeded on %s(%d/%d)\n", resp.IP, i+1, len(workerProxys))
            successCount++
        }
    }

    utils.SuccessPrintf("Preload complete: %d/%d workers succeeded.\n",
        successCount, len(workerProxys))
}