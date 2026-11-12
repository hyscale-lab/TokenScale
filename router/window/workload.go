package window

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

var (
	WorkloadWindow = NewWindow[ReqInfo](300)
)

type ReqInfo struct {
	Arrival      float64 `json:"arrival"`
	PromptLength int64   `json:"prefill_len"`
	DecodeLength int64   `json:"decode_len"`
}

func (r ReqInfo) String() string {
	b, err := json.Marshal(r)
	if err != nil {
		return fmt.Sprintf("ReqInfo<error: %v>", err)
	}
	return string(b)
}

func ConstructReqInfo(ctx context.Context, c *app.RequestContext) (ReqInfo, error) {
	// 1. Copy body
	bodyCopy := make([]byte, len(c.Request.Body()))
	copy(bodyCopy, c.Request.Body())

	// 2. Parse using flexible JSON structure
	var payload struct {
		Prompt    json.RawMessage `json:"prompt"`
		MaxTokens int             `json:"max_tokens"`
	}

	if err := json.Unmarshal(bodyCopy, &payload); err != nil {
		return ReqInfo{}, fmt.Errorf("failed to parse body: %w", err)
	}

	// 3. Parse prompt length based on type
	var promptLength int64

	// First try: list of int
	var intList []int
	if err := json.Unmarshal(payload.Prompt, &intList); err == nil {
		promptLength = int64(len(intList))
	} else {
		// Second try: string prompt
		var strPrompt string
		if err := json.Unmarshal(payload.Prompt, &strPrompt); err == nil {
			// Split on whitespace to count words
			words := strings.Fields(strPrompt)
			promptLength = int64(len(words))
		} else {
			return ReqInfo{}, fmt.Errorf("prompt must be []int or string")
		}
	}

	// 4. Construct ReqInfo
	return ReqInfo{
		Arrival:      float64(time.Now().UnixNano()) / 1e9,
		PromptLength: promptLength,
		DecodeLength: int64(payload.MaxTokens),
	}, nil
}

func GetWorkloads[T ReqInfo](w *Window[T], now time.Time) ([]ReqInfo, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.trimStaleLocked(now)
	workloads := []ReqInfo{}

	if len(w.buffer) == 0 {
		return workloads, nil
	}

	// Assert first element to ReqInfo
	first, ok := any(w.buffer[0].Value).(ReqInfo)
	if !ok {
		return workloads, fmt.Errorf("type assertion to ReqInfo failed")
	}
	baseArrival := first.Arrival

	adjusted := make([]ReqInfo, len(w.buffer))
	for i, item := range w.buffer {
		ri, ok := any(item.Value).(ReqInfo)
		if !ok {
			return workloads, fmt.Errorf("type assertion to ReqInfo failed at index %d", i)
		}
		adjusted[i] = ReqInfo{
			Arrival:      (ri.Arrival - baseArrival) * 1000.0,
			PromptLength: ri.PromptLength,
			DecodeLength: ri.DecodeLength,
		}
	}

	return adjusted, nil
}
