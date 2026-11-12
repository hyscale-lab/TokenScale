package manager

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"router/model"
)

// RemoteWriteFile sends a POST request to create a remote file with given content.
// It returns an error if the write fails.
func RemoteWriteFile(uri string, filePath string, content string) error {
	payload := map[string]string{
		"file_path":    filePath,
		"file_content": content,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode JSON payload: %w", err)
	}

	resp, err := http.Post(uri, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned non-OK status: %s", resp.Status)
	}

	return nil
}

func createTmpPrefillLmcacheConfigFile(zmqProxyHost string, zmqProxyPort int, uuid string, createFileURI string) (string, error) {
	// Step 1: Get the tmp dir from env or default to /tmp
	tmpDir := os.Getenv("ROUTER_TMP_DIR_PATH")
	if tmpDir == "" {
		tmpDir = "/tmp"
	}

	// Step 2: Construct file path
	fileName := fmt.Sprintf("lmcache_prefill_%s.yaml", uuid)
	filePath := filepath.Join(tmpDir, fileName)

	bufferSizeGB := 1
	if val := os.Getenv("NIXL_SEND_BUFFER_SIZE_GB"); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			bufferSizeGB = parsed
		}
	}
	bufferSizeBytes := bufferSizeGB * 1024 * 1024 * 1024
	// Step 3: Prepare file content
	content := fmt.Sprintf(`local_cpu: False
max_local_cpu_size: 0
max_local_disk_size: 0

enable_nixl: True
enable_xpyd: True
nixl_role: "sender"
nixl_proxy_host: %s
nixl_proxy_port: %d
nixl_buffer_size: %d # 1GB
nixl_buffer_device: "cuda"
`, zmqProxyHost, zmqProxyPort, bufferSizeBytes)

	// Step 4: Use createFileURI to create file in remote liquidlet
	err := RemoteWriteFile(createFileURI, filePath, content)
	if err != nil {
		return "", fmt.Errorf("failed to write config file to remote liquidlet: %v", err)
	}

	return filePath, nil
}

func createTmpDecodeLmcacheConfigFile(nixlPeerInitPortList []int, nixlPeerAllocPortList []int, uuid string, createFileURI string) (string, error) {
	// Step 1: Get the tmp dir from env or default to /tmp
	tmpDir := os.Getenv("ROUTER_TMP_DIR_PATH")
	if tmpDir == "" {
		tmpDir = "/tmp"
	}

	// Step 2: Construct file path
	fileName := fmt.Sprintf("lmcache_decode_%s.yaml", uuid)
	filePath := filepath.Join(tmpDir, fileName)

	// Step 3: Get buffer size from ENV or default to 4 GB
	bufferSizeGB := 4
	if val := os.Getenv("NIXL_RECV_BUFFER_SIZE_GB"); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			bufferSizeGB = parsed
		}
	}
	bufferSizeBytes := bufferSizeGB * 1024 * 1024 * 1024

	joinInts := func(ports []int) string {
		ss := make([]string, len(ports))
		for i, p := range ports {
			ss[i] = strconv.Itoa(p)
		}
		return strings.Join(ss, ",")
	}

	// Step 4: Prepare file content
	content := fmt.Sprintf(`local_cpu: False
max_local_cpu_size: 0

enable_nixl: True
enable_xpyd: True
nixl_role: "receiver"
nixl_peer_host: "0.0.0.0"
nixl_peer_init_port: %s
nixl_peer_alloc_port: %s
nixl_buffer_size: %d
nixl_buffer_device: "cuda"
`, joinInts(nixlPeerInitPortList), joinInts(nixlPeerAllocPortList), bufferSizeBytes)

	// Step 4: Create file in remote liquidlet
	err := RemoteWriteFile(createFileURI, filePath, content)
	if err != nil {
		return "", fmt.Errorf("failed to write config file to liquidlet: %v", err)
	}

	return filePath, nil
}

func joinIntsCSV(v []int) string {
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = strconv.Itoa(x)
	}
	return strings.Join(parts, ",")
}

func constructLmcacheExtraParamsAndEnvs(config model.RegisterModel, uuid string, ports model.LMCachePorts, createFileURI string) ([]string, []string, error) {
	extraEnvs := []string{}
	extraParams := []string{}
	_, modelType, err := model.GetModelNameAndType(config.ModelName)
	if err != nil {
		return extraEnvs, extraParams, err
	}
	// construct extra envs
	if modelType == "prefill" {
		routerHost := os.Getenv("ROUTER_HOST")
		if routerHost == "" {
			routerHost = "0.0.0.0"
		}

		zmqPortStr := os.Getenv("ZMQ_PORT")
		if zmqPortStr == "" {
			zmqPortStr = "7500"
		}
		zmqPort, err := strconv.Atoi(zmqPortStr)
		if err != nil {
			return extraEnvs, extraParams, fmt.Errorf("invalid ZMQ_PORT: %v", err)
		}
		tmpLmcacheConfigFile, err := createTmpPrefillLmcacheConfigFile(routerHost, zmqPort, uuid, createFileURI)
		if err != nil {
			return extraEnvs, extraParams, err
		}
		lmcacheConfigFileEnvStr := fmt.Sprintf("LMCACHE_CONFIG_FILE=%s", tmpLmcacheConfigFile)
		extraEnvs = append(extraEnvs, lmcacheConfigFileEnvStr)
	} else if modelType == "decode" {
		tmpLmcacheConfigFile, err := createTmpDecodeLmcacheConfigFile(ports.NixlPeerInitPortList, ports.NixlPeerAllocPortList, uuid, createFileURI)
		if err != nil {
			return extraEnvs, extraParams, err
		}
		lmcacheConfigFileEnvStr := fmt.Sprintf("LMCACHE_CONFIG_FILE=%s", tmpLmcacheConfigFile)
		
		deleteCSV := joinIntsCSV(ports.NixlPeerDeletePortList)
		nixlPeerDeletePortStr := fmt.Sprintf("NIXL_DELETE_PORT=%s", deleteCSV)
		fmt.Printf(">> Generated env: %s\n", nixlPeerDeletePortStr)
		// nixlPeerDeletePortStr := fmt.Sprintf("NIXL_DELETE_PORT=%d", ports.NixlPeerDeletePortList)
		extraEnvs = append(extraEnvs, nixlPeerDeletePortStr)
		extraEnvs = append(extraEnvs, lmcacheConfigFileEnvStr)

	}
	instanceUUIDStr := fmt.Sprintf("INSTANCE_UUID=%s", uuid)
	extraEnvs = append(extraEnvs, instanceUUIDStr)

	// Check if coldstart profiling is enabled
	enableColdstart := os.Getenv("ENABLE_COLDSTART_PROFILE")
	if enableColdstart != "" && enableColdstart != "0" {
		runLogDir := os.Getenv("RUN_LOG_DIR")
		if runLogDir == "" {
			return []string{}, []string{}, fmt.Errorf("ENABLE_COLDSTART_PROFILE is set but RUN_LOG_DIR not set!")
		} else {
			// Create the full output path
			reportPath := filepath.Join(runLogDir, fmt.Sprintf("coldstart_%s.json", uuid))
			coldstartEnv := fmt.Sprintf("COLDSTART_REPORT_FILEPATH=%s", reportPath)
			extraEnvs = append(extraEnvs, coldstartEnv)
		}
	}

	// construct extra params
	extraParams = append(extraParams, "--kv-transfer-config")
	kvRole := "kv_producer"
	if modelType == "decode" {
		kvRole = "kv_consumer"
	}
	rpcPort := ""
	if modelType == "prefill" {
		rpcPort = fmt.Sprintf("%s-%s", "producer", uuid)
	} else {
		rpcPort = fmt.Sprintf("%s-%s", "consumer", uuid)
	}
	kvTransferConfigStr := ""
	if modelType == "prefill" {
		kvTransferConfigStr = fmt.Sprintf(
			"{\"kv_connector\":\"LMCacheConnectorV1\",\"kv_role\":\"%s\", \"kv_connector_extra_config\": {\"discard_partial_chunks\": false, \"lmcache_rpc_port\": \"%s\"}}",
			kvRole,
			rpcPort,
		)
	} else {
		kvTransferConfigStr = fmt.Sprintf(
			"{\"kv_connector\":\"LMCacheConnectorV1\",\"kv_role\":\"%s\", \"kv_connector_extra_config\": {\"discard_partial_chunks\": false, \"lmcache_rpc_port\": \"%s\", \"skip_last_n_tokens\": 1}}",
			kvRole,
			rpcPort,
		)
	}
	extraParams = append(extraParams, kvTransferConfigStr)

	return extraEnvs, extraParams, nil
}

func constructCliReq(modelConfig model.RegisterModel, uuid string, host string, port int, gpus []string, extraEnvs []string, extraArgs []string) (model.CliReq, error) {
	envs := modelConfig.Env
	visibleStr := "CUDA_VISIBLE_DEVICES="
	for _, gpu := range gpus {
		visibleStr += gpu + ","
	}
	visibleStr = visibleStr[:len(visibleStr)-1]
	if envs != nil {
		envs = append(envs, visibleStr)
	} else {
		envs = []string{visibleStr}
	}
	// append extra envs
	envs = append(envs, extraEnvs...)
	args := modelConfig.Args
	if args == nil {
		args = make([]string, 0)
	}
	// tp:
	args = append(args, modelConfig.TPArg)
	args = append(args, strconv.Itoa(len(gpus)))
	// host:
	args = append(args, modelConfig.HostArg)
	// in cli, no need to specify host, just use 0.0.0.0
	args = append(args, "0.0.0.0")
	// args = append(args, host)
	// port:
	args = append(args, modelConfig.PortArg)
	args = append(args, strconv.Itoa(port))
	// append extra args
	args = append(args, extraArgs...)

	return model.CliReq{
		UUID: uuid,
		CLI:  modelConfig.Cli,
		Env:  envs,
		Args: args,
	}, nil
}
