package window

import (
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
)

// Sample holds one (timestamp, value) data point.
type Sample[T any] struct {
	TS    time.Time
	Value T // negative or zero for scale-in demand
}

type Window[T any] struct {
	span   time.Duration
	mu     sync.Mutex
	buffer []Sample[T] // oldest … newest
}

// New returns an empty sliding window with the desired span.
func NewWindow[T any](seconds int64) *Window[T] {
	return &Window[T]{span: time.Duration(seconds) * time.Second}
}

// Add inserts a sample and discards any that are now outside the span.
func (w *Window[T]) Add(now time.Time, value T) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.trimStaleLocked(now)

	// append new
	w.buffer = append(w.buffer, Sample[T]{TS: now, Value: value})
}

func (w *Window[T]) GetLength() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.trimStaleLocked(time.Now())
	return len(w.buffer)
}

// trimStaleLocked removes all samples older than now - span.
// Must be called with w.mu locked.
func (w *Window[T]) trimStaleLocked(now time.Time) {
	cut := now.Add(-w.span)
	hlog.Infof("[Window] now = %v, cut (now - span) = %v, span = %v\n", now.Format(time.RFC3339Nano), cut.Format(time.RFC3339Nano), w.span)
	i := 0
	for ; i < len(w.buffer) && w.buffer[i].TS.Before(cut); i++ {
	}
	w.buffer = w.buffer[i:]
}
