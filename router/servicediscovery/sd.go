package servicediscovery

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"

	"router/detect"
	"router/model"
)

var (
	fileName string
	sd       = make([]model.ServiceDiscovery, 0)
	sdLock   = sync.RWMutex{}
)

func init() {
	fileName = os.Getenv("SD_FILE")
	sd = append(sd, model.ServiceDiscovery{
		Targets: []string{detect.GetMetricsPort()},
		Labels:  map[string]string{"job": "liquid_router"},
	})
	updateConfig()
}

func updateConfig() {
	sdLock.Lock()
	defer sdLock.Unlock()
	jsonData, _ := json.Marshal(sd)
	if fileName != "" {
		os.WriteFile(fileName, jsonData, 0644)
	}
}

func NewInstance(config model.InstanceStatus) {
	sdLock.Lock()
	gpuListStr := strings.Join(config.GPUIDs, ",")
	sd = append(sd, model.ServiceDiscovery{
		Targets: []string{config.HostName + ":" + strconv.Itoa(config.Port)},
		Labels:  map[string]string{"job": "liquid_instance", "liquid_model": config.ModelName, "liquid_instance_uuid": config.UUID, "hostname": config.HostName, "gpuIDs": gpuListStr, "is_convert": strconv.FormatBool(config.IsConvert)}},
	)
	sdLock.Unlock()
	go updateConfig()
}

func DelInstance(uuid string) {
	sdLock.Lock()
	for i, v := range sd {
		if v.Labels["liquid_instance_uuid"] == uuid {
			sd = append(sd[:i], sd[i+1:]...)
			break
		}
	}
	sdLock.Unlock()
	go updateConfig()
}
