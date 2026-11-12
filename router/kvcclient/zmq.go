package kvcclient

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	zmq "github.com/go-zeromq/zmq4"
	"github.com/vmihailenco/msgpack/v5"
)

// ======================
// Real Implementation
// ======================

type ZmqKVClient struct {
	mu   sync.Mutex
	reqs map[string]chan bool
}

func NewZmqKVClient() *ZmqKVClient {
	return &ZmqKVClient{
		reqs: make(map[string]chan bool),
	}
}

type NixlMsg struct {
	ReqID string `msgpack:"req_id"`
}

type NixlAllocRequest struct {
	Keys          []string `msgpack:"keys"`
	Fmt           int      `msgpack:"fmt"`
	Shape         []int    `msgpack:"shape"`
	Dtype         string   `msgpack:"dtype"`
	LastChunkToks int      `msgpack:"last_chunk_toks"`
	ReqID         string   `msgpack:"req_id"`
	Delete        bool     `msgpack:"delete"`
	Type          string   `msgpack:"type"`
}

func (c *ZmqKVClient) SendDeleteKV(receiverHost string, receiverAllocPortList []int, reqID string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := NixlAllocRequest{
		Keys:          []string{},
		Fmt:           0,
		Shape:         []int{},
		Dtype:         "",
		LastChunkToks: 0,
		ReqID:         reqID,
		Delete:        true,
		Type:          "NixlAllocRequest",
	}

	data, err := msgpack.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal msgpack: %w", err)
	}

	for _, allocPort := range receiverAllocPortList {
		endpoint := fmt.Sprintf("tcp://%s:%d", receiverHost, allocPort)
		socket := zmq.NewReq(ctx)
		if err := socket.Dial(endpoint); err != nil {
			socket.Close()
			return fmt.Errorf("failed to dial %s: %w", endpoint, err)
		}

		if err := socket.Send(zmq.NewMsg(data)); err != nil {
			socket.Close()
			return fmt.Errorf("failed to send delete KV request: %w", err)
		}

		_, err = socket.Recv()
		socket.Close()
		if err != nil {
			return fmt.Errorf("failed to receive response: %w", err)
		}
	}

	return nil
}

func (c *ZmqKVClient) WaitForKVReady(ctx context.Context, reqID string, timeout time.Duration, TPLevel int) bool {
	c.mu.Lock()
	ch, exists := c.reqs[reqID]
	if !exists {
		ch = make(chan bool, 8)
		c.reqs[reqID] = ch
	}
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	successCount := 0
	for i := 0; i < TPLevel; i++ {
		select {
		case ok := <-ch:
			hlog.CtxDebugf(ctx, "req %s prefill kv ready (%d/%d, ok=%v)", reqID, i+1, TPLevel, ok)
			if !ok {
				// 只要有一个失败，就直接结束
				c.mu.Lock()
				delete(c.reqs, reqID)
				c.mu.Unlock()
				return false
			}
			successCount++

		case <-ctx.Done():
			hlog.CtxWarnf(ctx, "req %s prefill kv timeout (%d/%d)", reqID, successCount, TPLevel)
			c.mu.Lock()
			delete(c.reqs, reqID)
			c.mu.Unlock()
			return false
		}
	}
	hlog.CtxDebugf(ctx, "req %s prefill kv ready , ok=%v) for all tp ranks", reqID, true)
	c.mu.Lock()
	delete(c.reqs, reqID)
	c.mu.Unlock()
	return true
}

func (c *ZmqKVClient) MarkKVReady(reqID string, ok bool) {
	c.mu.Lock()
	ch, exists := c.reqs[reqID]
	if !exists {
		ch = make(chan bool, 8)
		c.reqs[reqID] = ch
	}
	c.mu.Unlock()
	ch <- ok
}

func (c *ZmqKVClient) MaybeStartZmqPullServer() {
	ctx := context.Background()
	if os.Getenv("PD_MODE") != "lmcache" {
		hlog.CtxInfof(ctx, "PD_MODE is not 'lmcache', skipping ZMQ server.")
		return
	}

	zmqPort := os.Getenv("ZMQ_PORT")
	if zmqPort == "" {
		zmqPort = "7500"
	}

	bindURL := "tcp://0.0.0.0:" + zmqPort
	go func() {
		if err := c.zmqPullServer(ctx, bindURL); err != nil {
			hlog.CtxErrorf(ctx, "ZMQ server failed: %v", err)
		}
	}()
}

func (c *ZmqKVClient) zmqPullServer(ctx context.Context, bindURL string) error {
	socket := zmq.NewPull(ctx)
	defer socket.Close()

	if err := socket.Listen(bindURL); err != nil {
		return err
	}

	hlog.CtxInfof(ctx, "ZMQ proxy server started on %s", bindURL)

	for {
		msg, err := socket.Recv()
		if err != nil {
			hlog.CtxInfof(ctx, "ZMQ Recv error: %v", err)
			time.Sleep(10 * time.Millisecond)
			continue
		}

		var decoded NixlMsg
		if err := msgpack.Unmarshal(msg.Frames[0], &decoded); err != nil {
			hlog.CtxInfof(ctx, "Failed to decode MsgPack: %v", err)
			continue
		}

		reqID := decoded.ReqID
		isError := false

		if strings.HasSuffix(reqID, "::error") {
			isError = true
			reqID = strings.TrimSuffix(reqID, "::error")
		}

		hlog.CtxInfof(ctx, "Gets kv notification for %s (error=%v)", reqID, isError)
		c.MarkKVReady(reqID, !isError)
	}
}
