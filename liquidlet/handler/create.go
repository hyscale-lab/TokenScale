package handler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"liquidlet/executor"
	"liquidlet/forkclient"
	"liquidlet/model"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

func CreateInstance(ctx context.Context, c *app.RequestContext) {
	var request model.CliReq
	if err := c.BindJSON(&request); err != nil {
		hlog.CtxErrorf(ctx, "bind request failed: %v", err)
		c.JSON(400, utils.H{"error": err.Error()})
		return
	}
	hlog.CtxInfof(ctx, "create instance: %v", request)
	resp, err := forkclient.SendCreate(request)
	if err != nil {
		hlog.CtxErrorf(ctx, "start instance via forkserver failed: %v", err)
		c.JSON(500, utils.H{"error": err.Error()})
		return
	}
	exe := model.Exe{
		UUID: request.UUID,
		PID:  resp.PID,
	}
	executor.AddExecutor(exe)
	hlog.CtxInfof(ctx, "create instance success: %v", request.UUID)

	c.JSON(200, utils.H{"message": "create instance success"})
}

func DeleteInstance(ctx context.Context, c *app.RequestContext) {
	var request model.CliDel
	if err := c.BindJSON(&request); err != nil {
		hlog.CtxErrorf(ctx, "bind request failed: %v", err)
		c.JSON(400, utils.H{"error": err.Error()})
	}
	executor.DeleteExecutor(ctx, request.UUID)
	c.JSON(200, utils.H{"message": "delete instance success"})
}

func PreloadModel(ctx context.Context, c *app.RequestContext) {
	var request model.PreloadReq
	if err := c.BindJSON(&request); err != nil {
		hlog.CtxErrorf(ctx, "bind request failed: %v", err)
		c.JSON(400, utils.H{"error": err.Error()})
	}
	hlog.CtxInfof(ctx, "preload model %s from disk to cpu memory", request.ModelPath)
	// Get PreloadScriptPath from environment or default
	scriptPath := os.Getenv("PreloadScriptPath")
	if scriptPath == "" {
		scriptPath = "/app/preload.py"
	}

	// Construct the command
	cmd := exec.CommandContext(ctx, "python", scriptPath, "--model-path", request.ModelPath, "--tensor-parallel-size", fmt.Sprintf("%d", request.TPSize))
	output, err := cmd.CombinedOutput()

	hlog.CtxInfof(ctx, "preload script output:\n%s", string(output))

	if err != nil {
		hlog.CtxErrorf(ctx, "preload failed: %v", err)
		c.JSON(http.StatusInternalServerError, utils.H{
			"error":  err.Error(),
			"output": string(output),
		})
		return
	}

	c.JSON(http.StatusOK, utils.H{"message": "Preload successful"})
}

func CreateFile(ctx context.Context, c *app.RequestContext) {
	var request model.CreateFileReq
	if err := c.BindJSON(&request); err != nil {
		hlog.CtxErrorf(ctx, "Failed to parse JSON: %v", err)
		c.JSON(400, map[string]string{"error": fmt.Sprintf("Invalid request format: %v", err)})
		return
	}

	// Check if directory exists
	dir := filepath.Dir(request.FilePath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		hlog.CtxErrorf(ctx, "Directory does not exist: %s", dir)
		c.JSON(400, map[string]string{"error": fmt.Sprintf("Directory does not exist: %s", dir)})
		return
	}

	// Write file (create if not exist, overwrite if exists)
	if err := os.WriteFile(request.FilePath, []byte(request.FileContent), 0644); err != nil {
		hlog.CtxErrorf(ctx, "Failed to write file: %v", err)
		c.JSON(500, map[string]string{"error": fmt.Sprintf("Failed to write file: %v", err)})
		return
	}

	hlog.CtxInfof(ctx, "File content successfully written to %s", request.FilePath)
	c.JSON(200, map[string]string{"message": "File created successfully"})
}
