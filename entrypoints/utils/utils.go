package utils

import (
	"bytes"
	"encoding/json"
	"entrypoints/config"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// var (
// 	CommonLog *log.Logger = nil // Common logs
// 	ErrorLog  *log.Logger = nil // Error logs

// Define log levels
const (
	DEBUG = iota
	INFO
	WARN
	ERROR
)
// )
const (
	NSCC_2A_PLATFORM_STR = "NSCC_2A"
	NSCC_2P_PLATFORM_STR = "NSCC_2P"
	LOCAL_TEST_PLATFORM_STR = "LOCAL_TEST"
)

var (
	_colorReset  = "\033[0m"
	_colorRed    = "\033[31m"
	_colorGreen  = "\033[32m"
	_colorYellow = "\033[33m"
	_colorBlue   = "\033[34m"
	// _colorPurple = "\033[35m"
	// _colorCyan   = "\033[36m"
	// _colorGray   = "\033[37m"
	_colorWhite  = "\033[97m"
	logLevel = INFO
	Platform string = ""
	ExperimentName string = ""
)

func InitLogger() {
	// Read LOGLEVEL from environment variable
	logLevelStr := os.Getenv("LOGLEVEL")

	// Convert LOGLEVEL string to log level constant
	switch strings.ToUpper(logLevelStr) {
	case "DEBUG":
		logLevel = DEBUG
	case "INFO", "":
		logLevel = INFO
	case "WARN":
		logLevel = WARN
	case "ERROR":
		logLevel = ERROR
	default:
		InfoPrintf("Invalid LOGLEVEL: %s, defaulting to INFO", logLevelStr)
		logLevel = INFO
	}
}


type ShellError struct {
	Msg      string
	ExitCode int
}

func (err *ShellError) Error() string {
	return fmt.Sprintf("[exit %d] -> %s", err.ExitCode, err.Msg)
}

// Execute Shell Command
func ExecLocalShellCmd(cmd string, pars ...any) (string, error) {
	// Allocate bytes buffer
	bashCmd := new(bytes.Buffer)
	cmdStdout := new(bytes.Buffer)
	cmdStderr := new(bytes.Buffer)
	fmt.Fprintf(bashCmd, cmd, pars...)
	bashProcess := exec.Command("bash", "-c", bashCmd.String())
	// Redirect stdout & stderr
	bashProcess.Stdout = cmdStdout
	bashProcess.Stderr = cmdStderr

	// Execute command
	err := bashProcess.Run()

	// remove suffix "\n" in Stdout & Stderr
	var trimmedStdout string
	var trimmedStderr string
	if cmdStdout.Len() > 0 {
		trimmedStdout = strings.TrimSuffix(cmdStdout.String(), "\n")
	} else {
		trimmedStdout = ""
	}
	if cmdStderr.Len() > 0 {
		trimmedStderr = strings.TrimSuffix(cmdStderr.String(), "\n")
	} else {
		trimmedStderr = ""
	}

	// Rewrite error message
	if err != nil {
		err = &ShellError{Msg: trimmedStderr, ExitCode: bashProcess.ProcessState.ExitCode()}
	}

	DebugPrintf("Executing shell command: %s\n", bashCmd.String())
	DebugPrintf("Stdout from shell: %s\n", trimmedStdout)
	DebugPrintf("Stderr from shell: %s\n", trimmedStderr)

	return trimmedStdout, err
}

// Execute Shell Command
func ExecLocalShellCmdAsync(stdoutFile string, stderrFile string, cmd string, pars ...any) {
	// Allocate bytes buffer
	bashCmd := new(bytes.Buffer)
	fmt.Fprintf(bashCmd, cmd, pars...)
	go func() {
		bashProcess := exec.Command("bash", "-c", bashCmd.String())
		if stdoutFile != "" {
			stdout, err := os.OpenFile(stdoutFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
			CheckErrorWithMsg(err, "Failed to open stdout file!\n")
			defer stdout.Close()
			bashProcess.Stdout = stdout	
		} else {
			bashProcess.Stdout = os.Stdout
		}

		if stderrFile != "" {
			stderr, err := os.OpenFile(stderrFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
			CheckErrorWithMsg(err, "Failed to open stderr file!\n")
			defer stderr.Close()
			bashProcess.Stderr = stderr
		} else {
			bashProcess.Stderr = os.Stderr
		}
		bashProcess.Run()
	}()
	DebugPrintf("Executing shell command: %s\n", bashCmd.String())
}

// Print colored text in terminal
func coloredPrintf(color string, format string, pars ...any) {
	fmt.Print(color)
	fmt.Printf(format, pars...)
	fmt.Print(_colorReset)
}

func ErrorPrintf(format string, pars ...any) {
	currentTime := time.Now().Local()
	// For output
	coloredPrintf(_colorRed, "[%02d:%02d:%02d] [Error] ", currentTime.Hour(), currentTime.Minute(), currentTime.Second())
	coloredPrintf(_colorRed, format, pars...)
}

func FatalPrintf(format string, pars ...any) {
	ErrorPrintf(format, pars...)
	ExitProgram(ExperimentName)
}

func SuccessPrintf(format string, pars ...any) {
	currentTime := time.Now().Local()
	// For output
	coloredPrintf(_colorGreen, "[%02d:%02d:%02d] [Success] ", currentTime.Hour(), currentTime.Minute(), currentTime.Second())
	coloredPrintf(_colorGreen, format, pars...)
}

func DebugPrintf(format string, pars ...any) {
	if logLevel <= DEBUG {
		currentTime := time.Now().Local()
		// For output
		coloredPrintf(_colorWhite, "[%02d:%02d:%02d] [Debug] ", currentTime.Hour(), currentTime.Minute(), currentTime.Second())
		coloredPrintf(_colorWhite, format, pars...)
	}

}

func InfoPrintf(format string, pars ...any) {
	if logLevel <= INFO {
		currentTime := time.Now().Local()
		// For output
		coloredPrintf(_colorBlue, "[%02d:%02d:%02d] [Info] ", currentTime.Hour(), currentTime.Minute(), currentTime.Second())
		coloredPrintf(_colorBlue, format, pars...)
	}
}

func WaitPrintf(format string, pars ...any) {
	SuccessPrintf(format+" >>>>> \n", pars...)
}

func CheckErrorWithMsg(err error, format string, pars ...any) bool {
	if err != nil {
		ErrorPrintf("%v\n", err)
		FatalPrintf(format, pars...)
		return false
	}
	return true
}

func ExitRun(jobID string) {
	LocalProxy.StopPBSJobsByID(jobID)
}

func ExitProgram(experimentName string) {
	// Kill router
	switch Platform {
		case NSCC_2A_PLATFORM_STR, NSCC_2P_PLATFORM_STR:
			LocalProxy.StopPBSJobsByName(fmt.Sprintf("%s*", experimentName))
		case LOCAL_TEST_PLATFORM_STR:
			LocalProxy.KillProcessesByName("router", false)
			LocalProxy.KillContainerByName("prometheus", false)
			LocalProxy.KillContainerByName("liquidlet", false)
			
			LocalProxy.KillProcessesByName("LLMLoadgen", false)
			LocalProxy.KillGpu()
	}
}

type ScalerInfo struct {
	ReqCounter		int64				`json:"req_counter"`
	InstanceCounter int64				`json:"instance_counter"`
}

type RouterInfo struct {
	RegisteredNodeNumber int      `json:"registered_node_number"`
	RegisteredModels     []string `json:"registered_models"`
	ScalerInfoMap		 map[string]ScalerInfo `json:"scaler_info_map"`
}


func WaitRouterReady(url string) {
	for {
		// Prepare request body (if needed, modify this to match the server's expected format)
		requestBody, _ := json.Marshal(map[string]string{})

		// Send HTTP POST request
		_, err := http.Post(url, "application/json", bytes.NewBuffer(requestBody))
		if err != nil {
			fmt.Printf("Failed to send request: %v\n", err)
			time.Sleep(1 * time.Second)
			continue
		}
		break
	}
}

func WaitNodesReady(expectedNodeNumber int, url string) {
	for {
		// Prepare request body (if needed, modify this to match the server's expected format)
		requestBody, _ := json.Marshal(map[string]string{})

		// Send HTTP POST request
		resp, err := http.Post(url, "application/json", bytes.NewBuffer(requestBody))
		if err != nil {
			fmt.Printf("Failed to send request: %v\n", err)
			time.Sleep(1 * time.Second)
			continue
		}

		// Read response body
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		CheckErrorWithMsg(err, "Failed to read http response!\n")

		// Parse JSON response
		var routerInfo RouterInfo
		if len(body) == 0 {
			DebugPrintf("Waiting for nodes... Expected: %d, Current: %d\n", expectedNodeNumber, routerInfo.RegisteredNodeNumber)
			time.Sleep(1 * time.Second)
			continue
		}		
		err = json.Unmarshal(body, &routerInfo)
		CheckErrorWithMsg(err, "Failed to unmarshal json\n")

		// Check if registered nodes match expected count
		if routerInfo.RegisteredNodeNumber == expectedNodeNumber {
			SuccessPrintf("All nodes are ready!\n")
			return
		}

		// Otherwise, wait and retry
		DebugPrintf("Waiting for nodes... Expected: %d, Current: %d\n", expectedNodeNumber, routerInfo.RegisteredNodeNumber)
		time.Sleep(1 * time.Second)
	}
}

func WaitModelInstancesReady(modelConfig *config.ModelConfig, url string) {
	modelName := modelConfig.ModelName
	val, ok := modelConfig.AutoScalerConfig["min_instance"]
	if !ok {
		InfoPrintf("%s doesn't have \"min_instance\" in its autoscaler config, use 0!\n", modelName)
		val = 0
	}
	expectedInstanceNumberFloat, ok := val.(float64)
	if !ok {
		err := fmt.Errorf("\"min_instance\" is not a number, got: %T", val)
		CheckErrorWithMsg(err, "Failed to transfer min_instance to float, err: %v\n", err)	
	}
	expectedInstanceNumber := int64(expectedInstanceNumberFloat)

	WaitPrintf("Waiting for %s  to have at least %d instance(s)",modelName, expectedInstanceNumber)
	for {
		// Prepare request body (if needed, modify this to match the server's expected format)
		requestBody, _ := json.Marshal(map[string]string{})

		// Send HTTP POST request
		resp, err := http.Post(url, "application/json", bytes.NewBuffer(requestBody))
		if err != nil {
			fmt.Printf("Failed to send request: %v\n", err)
			time.Sleep(1 * time.Second)
			continue
		}

		// Read response body
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		CheckErrorWithMsg(err, "Failed to read http response!\n")

		// Parse JSON response
		var routerInfo RouterInfo

		if len(body) == 0 {
			DebugPrintf("Waiting for model:%s instances. Expected: %d, got empty response body", modelName, expectedInstanceNumber)
			time.Sleep(1 * time.Second)
			continue
		}		
		err = json.Unmarshal(body, &routerInfo)
		CheckErrorWithMsg(err, "Failed to unmarshal json\n")

		scalerInfo, ok := routerInfo.ScalerInfoMap[modelName]
		if !ok {
			DebugPrintf("Model %s not registered in the router!", modelName)
			time.Sleep(1 * time.Second)
			continue
		}

		// Check if current model instances >= expected count
		if scalerInfo.InstanceCounter >= expectedInstanceNumber {
			SuccessPrintf("Model %s instances ready\n", modelName)
			return
		}

		// Otherwise, wait and retry
		DebugPrintf("Waiting for model:%s instances. Expected: %d, Current: %d\n", modelName, expectedInstanceNumber, scalerInfo.InstanceCounter)
		time.Sleep(1 * time.Second)
	}

}

func ExtractHostnames(input string) []string {
	hostsPart := strings.TrimSpace(strings.Split(input, "=")[1])
	rawHosts := strings.Split(hostsPart, "+")

	hostnames := make([]string, 0, len(rawHosts))
	for _, host := range rawHosts {
		cleanHost := strings.Split(host, "/")[0]
		hostnames = append(hostnames, cleanHost)
	}

	return hostnames
}
