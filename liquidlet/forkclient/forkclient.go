package forkclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"liquidlet/model"

	"github.com/cloudwego/hertz/pkg/common/hlog"
)

var (
	forkClient = &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
	}
	defaultForkserverURL = "http://127.0.0.1:48888"
)

type ForkServerResp struct {
	PID int `json:"pid"`
}

type DeletePayload struct {
	UUID string `json:"uuid"`
}

type DeleteResp struct {
	STAT string `json:"status"`
}

func getForkserverURL() string {
	if port := os.Getenv("FORKSERVER_PORT"); port != "" {
		url := fmt.Sprintf("http://127.0.0.1:%s", port)
		return url
	}
	return defaultForkserverURL
}

func sendToForkServer(method, path string, payload interface{}, result interface{}) error {
	url := getForkserverURL() + path

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	hlog.Infof("Sending %s to %s with body: %s", method, url, string(bodyBytes))

	req, err := http.NewRequest(method, url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := forkClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	hlog.Infof("Received status %d, body: %s", resp.StatusCode, string(respBody))

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("forkserver error [%d]: %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}
	return nil
}

func SendCreate(req model.CliReq) (*ForkServerResp, error) {
	var resp ForkServerResp
	err := sendToForkServer("POST", "/create", req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func SendDelete(uuid string) error {
	var resp DeleteResp
	payload := DeletePayload{UUID: uuid}
	hlog.Infof("Sending delete request for UUID: %s", uuid)
	return sendToForkServer("POST", "/delete", payload, &resp)
}
