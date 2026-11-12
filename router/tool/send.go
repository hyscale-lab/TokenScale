package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/cloudwego/hertz/pkg/app/client"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func PostBody(ctx context.Context, body []byte, addr string) error {
	req := &protocol.Request{}
	res := &protocol.Response{}
	req.Header.SetMethod(consts.MethodPost)
	req.Header.SetContentTypeBytes([]byte("application/json"))
	req.SetRequestURI(addr)
	req.SetBody(body)
	err := client.Do(ctx, req, res)
	if err != nil {
		hlog.CtxInfof(ctx, "Failed to post: %v", err)
		return err
	}
	bodyString := string(res.Body())

	hlog.CtxInfof(ctx, "Post success with resp: %v", bodyString)
	// check status code
	if res.StatusCode() != 200 {
		hlog.CtxInfof(ctx, "Post failed with status code: %v", res.StatusCode())
		err = fmt.Errorf("Post failed with status code: %v and resp %v", res.StatusCode(), bodyString)
		return err
	}
	return nil
}

func Post(ctx context.Context, data interface{}, addr string) error {
	body, err := json.Marshal(data)
	if err != nil {
		hlog.CtxInfof(ctx, "Failed to marshal data: %v", err)
		return err
	}
	return PostBody(ctx, body, addr)
}

func PortDetect(ctx context.Context, host string, port int) bool {
	address := host + ":" + strconv.Itoa(port)
	conn, err := net.DialTimeout("tcp", address, 1*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func VllmServiceDetect(ctx context.Context, host string, port int) bool {
	url := fmt.Sprintf("http://%s:%d/openapi.json", host, port)

	// Create a new HTTP client with a short timeout
	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false
	}

	// Perform the request
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Consider any 2xx response as success
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
