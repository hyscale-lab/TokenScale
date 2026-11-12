package deploy

import (
	"entrypoints/config"
	"entrypoints/utils"
	"fmt"
	"path/filepath"
	"strings"
)

func StartLLMLoadgen(runConfig config.RunConfig, runLogPath string, routerProxy *utils.HostProxy) {
	routerProxy.Chdir(runConfig.LLMLoadgenConfig.LLMLoadgenPath)
	loadgenStdoutFile := filepath.Join(runLogPath, "loadgen.out")
	loadgenStderrFile := filepath.Join(runLogPath, "loadgen.err")
	loadgenResultFile := filepath.Join(runLogPath, "loadgen_result.json")
	// Perform trace transformation first
	llmLoadgenConfig := runConfig.LLMLoadgenConfig
	traceFilePath := llmLoadgenConfig.TraceFilePath
	traceTransformerPath := filepath.Join(llmLoadgenConfig.LLMLoadgenPath, "offline/trace_transformer.py") 
	traceTransformerCmd := ""
	if llmLoadgenConfig.Model == "" {
		traceTransformerCmd = fmt.Sprintf(
			"cd %s && %s --src_path %s --input_length_ratio %f --output_length_ratio %f",
			llmLoadgenConfig.LLMLoadgenPath,
			traceTransformerPath,
			traceFilePath,
			llmLoadgenConfig.InputLengthRatio,
			llmLoadgenConfig.OutputLengthRatio,
		)

	} else {
		traceTransformerCmd = fmt.Sprintf(
			"cd %s && %s --src_path %s --model %s --input_length_ratio %f --output_length_ratio %f",
			llmLoadgenConfig.LLMLoadgenPath,
			traceTransformerPath,
			traceFilePath,
			llmLoadgenConfig.Model,
			llmLoadgenConfig.InputLengthRatio,
			llmLoadgenConfig.OutputLengthRatio,
		)
	}
	out, err := routerProxy.ExecShellCmd(traceTransformerCmd)
	utils.CheckErrorWithMsg(err, "Failed to execute trace transformer: %v\n", err)
	lines := strings.Split(out, "\n")

	// Loop backwards to skip empty lines (common when output ends with a newline)
	lastLine := lines[len(lines)-1]
	utils.SuccessPrintf("Last line for trace transformer is: [%s]\n", lastLine)
	const marker = "dst trace file is: "
	idx := strings.Index(lastLine, marker)
	dstTraceFilePath := ""
	if idx == -1 {
		// Handle unexpected format
		utils.FatalPrintf("ERROR: cannot find trace file path in output")
	} else {
		dstTraceFilePath = strings.TrimSpace(lastLine[idx+len(marker):])
	}


	
	// Construct LLMLoadgen commands
	baseLLMLoadgenCmd := fmt.Sprintf(
		"cd %s && ./LLMLoadgen -dataset_path %s -sampling_rate %d -url http://%s:%d/v1/completions -result_path %s",
		runConfig.LLMLoadgenConfig.LLMLoadgenPath,
		dstTraceFilePath,
		runConfig.LLMLoadgenConfig.SamplingRate,
		routerProxy.IP,
		runConfig.CommonConfig.RouterPort,
		loadgenResultFile,
	)
	for k, v := range runConfig.LLMLoadgenConfig.ExtraParams {
		baseLLMLoadgenCmd += fmt.Sprintf(" %s %s", k, v)
	}
	routerProxy.ExecShellCmdAsync(loadgenStdoutFile, loadgenStderrFile, baseLLMLoadgenCmd)
}