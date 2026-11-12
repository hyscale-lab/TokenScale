package executor

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"liquidlet/model"

	"github.com/cloudwego/hertz/pkg/common/hlog"
)

var (
	creationChan = make(chan model.Exe)
	cancelMap    = make(map[string]context.CancelFunc)
	lock         sync.Mutex
)

func init() {
	ctx := context.Background()
	go createExecutor(ctx)
	go exitMonitor(ctx)
}

func registerService(ctx context.Context, cliReq model.Exe) {
	ctx, cancel := context.WithCancel(ctx)
	lock.Lock()
	cancelMap[cliReq.UUID] = cancel
	lock.Unlock()
	go ProcessMonitor(ctx, cliReq.UUID, cliReq.PID)
}

func exitMonitor(ctx context.Context) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	hlog.CtxInfof(ctx, "daemon receive exit signal, cleaning up")
	lock.Lock()
	defer lock.Unlock()
	for _, cancel := range cancelMap {
		cancel()
	}
	hlog.CtxInfof(ctx, "daemon exit")
}

func createExecutor(ctx context.Context) {
	for exe := range creationChan {
		go registerService(ctx, exe)
	}
}

func AddExecutor(cliReq model.Exe) {
	creationChan <- cliReq
}

func DeleteExecutor(ctx context.Context, UUID string) {
	lock.Lock()
	cancel, ok := cancelMap[UUID]
	lock.Unlock()
	if ok {
		cancel()
	}
}
