package executor

import (
	"context"
	"liquidlet/forkclient"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/cloudwego/hertz/pkg/common/hlog"
)

func ProcessStarter(ctx context.Context, uuid string, cli string, args []string, env []string) (*exec.Cmd, int, error) {
	cmd := exec.Command(cli, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Start()
	if err != nil {
		hlog.CtxErrorf(ctx, "start instance %v failed: %v", uuid, err)
		return nil, 0, err
	}

	pid := cmd.Process.Pid
	hlog.CtxInfof(ctx, "start instance %v success with PID %v", uuid, pid)
	return cmd, pid, nil
}

func ProcessMonitor(ctx context.Context, uuid string, pid int) {
	hlog.CtxInfof(ctx, "instance %v monitor start", uuid)

	defer func() {
		if r := recover(); r != nil {
			hlog.CtxErrorf(ctx, "instance %v panic: %v, killing process with PID %v", uuid, r, pid)
		}
		if err := forkclient.SendDelete(uuid); err != nil {
			hlog.CtxErrorf(ctx, "delete instance via forkserver failed: %v", err)
			return
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-ctx.Done():
		hlog.CtxInfof(ctx, "instance %v monitor exit", uuid)
		return
	case <-sigChan:
		hlog.CtxInfof(ctx, "instance %v monitor exit", uuid)
		return
	}
}
