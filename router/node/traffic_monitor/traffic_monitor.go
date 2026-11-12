package traffic_monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"router/autoscaler"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/redis/go-redis/v9"
)

type Record struct {
	Model        string
	InputTokens  int
	OutputTokens int
}

type TrafficMonitor struct {
	recordChan chan Record
	modelName  string
	redis      *redis.Client
	buffer     []Record
	autoscaler autoscaler.Autoscaler
}

func NewTrafficMonitor(ctx context.Context, modelName, redisAddr, redisPassword string) *TrafficMonitor {
	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       0,
	})

	atc := autoscaler.GetAutoscaler(ctx, modelName)
	if atc == nil {
		panic(fmt.Sprintf("autoscaler for model %s not found", modelName))
	}

	tm := &TrafficMonitor{
		modelName:  modelName,
		recordChan: make(chan Record, 10000),
		redis:      rdb,
		buffer:     make([]Record, 0, 1024),
		autoscaler: atc,
	}

	go tm.run(ctx)
	return tm
}

func (tm *TrafficMonitor) RawRecord(ctx context.Context, modelName string, rsp *protocol.Response) {
	var parsed struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	body := rsp.Body()
	bodyCopy := make([]byte, len(body))
	copy(bodyCopy, body)
	if err := json.Unmarshal(bodyCopy, &parsed); err != nil {
		hlog.Errorf("Failed to parse response body: %v\nBody: %s", err, string(bodyCopy))
		return
	}

	if parsed.Usage.PromptTokens == 0 {
		hlog.Warnf("Missing model or prompt_tokens in parsed response. Skipping.")
		return
	}

	hlog.Infof("TrafficMonitor: model=%s, prompt_tokens=%d, completion_tokens=%d", modelName, parsed.Usage.PromptTokens, parsed.Usage.CompletionTokens)

	tm.Record(ctx, modelName, parsed.Usage.PromptTokens, parsed.Usage.CompletionTokens)
}

func (tm *TrafficMonitor) Record(ctx context.Context, model string, inputTokens, outputTokens int) {
	select {
	case tm.recordChan <- Record{Model: model, InputTokens: inputTokens, OutputTokens: outputTokens}:
	default:
		hlog.Warnf("TrafficMonitor channel full. Dropping record: model=%s in=%d out=%d", model, inputTokens, outputTokens)
	}
}

func (tm *TrafficMonitor) run(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case r := <-tm.recordChan:
			tm.buffer = append(tm.buffer, r)

		case t := <-ticker.C:
			now := t.Unix()
			if now%10 == 0 {
				tm.aggregateAndFlush(ctx, now)
			}
		}
	}
}

func (tm *TrafficMonitor) aggregateAndFlush(ctx context.Context, ts int64) {
	if len(tm.buffer) == 0 {
		return
	}

	records := tm.buffer
	tm.buffer = tm.buffer[:0]

	buckets := make(map[string]map[string]int)
	counts := make(map[string]int)

	for _, r := range records {
		bucket := fmt.Sprintf("%d:%d", log2(r.InputTokens), log2(r.OutputTokens))
		if _, ok := buckets[r.Model]; !ok {
			buckets[r.Model] = make(map[string]int)
		}
		buckets[r.Model][bucket]++
		counts[r.Model]++
	}

	for model, profile := range buckets {
		profile["meta_v"] = 3
		profile["meta_precision"] = 1
		profile["meta_interval_sec"] = 10
		profile["meta_total_reqs"] = counts[model]

		profile["meta_pending_reqs"] = int(tm.autoscaler.GetScalerInfo(ctx).ReqCounter)

		jsonData, _ := json.Marshal(profile)
		key := fmt.Sprintf("aibrix:%s_request_trace_%d", model, ts)
		if err := tm.redis.Set(context.Background(), key, jsonData, 24*time.Hour).Err(); err != nil {
			hlog.Errorf("Redis write error: %v", err)
		}
	}
}

func log2(x int) int {
	if x < 4 {
		return 2
	} else if x > 8192 {
		return 13
	}
	return int(math.Round(math.Log2(float64(x))))
}
