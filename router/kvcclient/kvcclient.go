package kvcclient

import (
	"context"
	"time"
)

// ======================
// Interfaces & Globals
// ======================

type KVClient interface {
	SendDeleteKV(receiverHost string, receiverAllocPortList []int, reqID string, timeout time.Duration) error
	WaitForKVReady(ctx context.Context, reqID string, timeout time.Duration, TPLevel int) bool
	MarkKVReady(reqID string, ok bool)
	MaybeStartZmqPullServer()
}

// Global instance, replaceable in tests
var kvcClient KVClient = NewZmqKVClient()

// ======================
// Public API wrappers
// ======================

func SendDeleteKV(receiverHost string, receiverAllocPortList []int, reqID string, timeout time.Duration) error {
	return kvcClient.SendDeleteKV(receiverHost, receiverAllocPortList, reqID, timeout)
}

func WaitForKVReady(ctx context.Context, reqID string, timeout time.Duration, TPLevel int) bool {
	return kvcClient.WaitForKVReady(ctx, reqID, timeout, TPLevel)
}

func MarkKVReady(reqID string, ok bool) {
	kvcClient.MarkKVReady(reqID, ok)
}

func MaybeStartZmqPullServer() {
	kvcClient.MaybeStartZmqPullServer()
}

func SetKVCClient(c KVClient) {
	kvcClient = c
}
