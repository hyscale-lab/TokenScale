package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"router/autoscaler"
	"router/mocks" // path to where you put mocks
	"router/model"
	"router/route_policy"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/golang/mock/gomock"
)

func setupProjectRoot(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot get current test file path")
	}
	projectRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	absPath, err := filepath.Abs(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv("PROJECT_ROOT_PATH", absPath)
}

// ---------------- Helper: Create Manager mock ----------------
func setMockManager(mockMgr *mocks.MockManager, instanceUUID string, instanceStatus model.InstanceStatus, instanceReverseProxy *mocks.MockReverseProxy) {
	mockMgr.EXPECT().
		GetInstanceInfo(gomock.Any(), instanceUUID).
		Return(instanceStatus, nil)
	mockMgr.EXPECT().
		GetInstanceReverseProxy(gomock.Any(), instanceUUID).
		Return(instanceReverseProxy, nil)
}

// ---------------- Helper: Create ReverseProxy mock ----------------
func createMockReverseProxy(ctrl *gomock.Controller) *mocks.MockReverseProxy {
	proxy := mocks.NewMockReverseProxy(ctrl)
	mockServe := func(ctx context.Context, c *app.RequestContext) {
		c.Response.SetBody([]byte(`{"foo":"bar"}`))
		c.Response.SetStatusCode(200)
	}
	proxy.EXPECT().
		ServeHTTP(gomock.Any(), gomock.Any()).
		DoAndReturn(mockServe)
	return proxy
}

func setMockReverseProxy(proxy *mocks.MockReverseProxy) {
	mockServe := func(ctx context.Context, c *app.RequestContext) {
		c.Response.SetBody([]byte(`{"foo":"bar"}`))
		c.Response.SetStatusCode(200)
	}
	proxy.EXPECT().
		ServeHTTP(gomock.Any(), gomock.Any()).
		DoAndReturn(mockServe)

}

// ---------------- Helper: Register model & scaler ----------------
func registerScaler(ctx context.Context, modelName string, config map[string]interface{}) (*model.ScalerChan, error) {
	scalerChan, err := autoscaler.RegisterModel(ctx, modelName, config)
	if err != nil {
		return nil, err
	}
	scalerMap[modelName] = scalerChan
	return scalerChan, nil
}

// ---------------- Helper: Create Tokenpipe route policy ----------------
func createRoutePolicy(ctx context.Context) {
	routePolicy := route_policy.NewTokenpipeRoutePolicy()
	route_policy.SetRoutePolicy(routePolicy)
}

// ---------------- Helper: Register a single instance ----------------

func registerModelOnRouter(ctx context.Context, modelName string) {
	// Ensure model is registered (no-op if already registered)
	route_policy.RegisterModel(ctx, modelName, nil)
}

func registerInstance(ctx context.Context, modelName string, instanceStatus model.InstanceStatus, scalerChan *model.ScalerChan) {

	// Register the instance
	route_policy.RegisterInstance(ctx, instanceStatus.UUID, modelName, instanceStatus)

	// announce the scaler
	scalerChan.ScaleUpResult <- model.Pair{First: true, Second: instanceStatus.UUID}
}

// ---------------- Helper: Assert router timestamps in response ----------------
func assertRouterTimestamps(t *testing.T, c *app.RequestContext) {
	var respMap map[string]interface{}
	if err := json.Unmarshal(c.Response.Body(), &respMap); err != nil {
		t.Fatalf("failed to unmarshal response body: %v", err)
	}
	if _, ok := respMap["router_timestamps"]; !ok {
		t.Errorf("expected 'router_timestamps' in response, but not found")
	}
}
func assertNotRouterTimestamps(t *testing.T, c *app.RequestContext) {
	var respMap map[string]interface{}
	if err := json.Unmarshal(c.Response.Body(), &respMap); err != nil {
		t.Fatalf("failed to unmarshal response body: %v", err)
	}
	if _, ok := respMap["router_timestamps"]; ok {
		t.Errorf("did not expect 'router_timestamps' in response, but found")
	}
}

// ---------------- Test case ----------------
func TestNormalPath(t *testing.T) {
	setupProjectRoot(t)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()
	modelName := "meta-llama/Llama-3.1-8B"
	prefillModel := fmt.Sprintf("%s::prefill", modelName)
	decodeModel := fmt.Sprintf("%s::decode", modelName)
	prefillUUID := "prefill-123"
	decodeUUID := "decode-123"

	// ---------------- Register scalers ----------------
	prefillScalerConfig := map[string]interface{}{
		"type":          "KPA",
		"tp_level":      1,
		"target_metric": "rps_counter",
		"target_value":  10000,
		"min_instance":  1,
		"max_instance":  1,
	}
	decodeScalerConfig := map[string]interface{}{
		"type":                       "CAP",
		"tp_level":                   1,
		"capacity_profile_file_path": "entrypoints/configs/capacity_profiles/llama_3.1_8B.json",
		"min_instance":               1,
		"max_instance":               1,
		"convert_instance":           0,
		"convert_max_request":        10,
	}

	prefillScalerChan, err := registerScaler(ctx, prefillModel, prefillScalerConfig)
	if err != nil {
		t.Error(err)
	}
	decodeScalerChan, err := registerScaler(ctx, decodeModel, decodeScalerConfig)
	if err != nil {
		t.Error(err)
	}
	// ---------------- Create tokenpipe route policy ----------------
	createRoutePolicy(ctx)
	registerModelOnRouter(ctx, prefillModel)
	registerModelOnRouter(ctx, decodeModel)

	// ---------------- Register tokenpipe instances ----------------
	// Create instance statuses
	prefillInstanceStatus := model.InstanceStatus{
		UUID:      "prefill-123",
		ModelName: prefillModel,
		HostName:  "localhost",
		Port:      7500,
		GPUIDs:    []string{"0"},
		IsConvert: false,
	}

	decodeInstanceStatus := model.InstanceStatus{
		UUID:      "decode-123",
		ModelName: decodeModel,
		HostName:  "localhost",
		Port:      7500,
		GPUIDs:    []string{"1"},
		IsConvert: false,
	}

	// Register them
	registerInstance(ctx, prefillModel, prefillInstanceStatus, prefillScalerChan)
	registerInstance(ctx, decodeModel, decodeInstanceStatus, decodeScalerChan)

	// ---------------- Create mocks ----------------
	prefillProxy := createMockReverseProxy(ctrl)
	decodeProxy := createMockReverseProxy(ctrl)
	mockMgr := mocks.NewMockManager(ctrl)
	setMockManager(mockMgr, prefillUUID, prefillInstanceStatus, prefillProxy)
	setMockManager(mockMgr, decodeUUID, decodeInstanceStatus, decodeProxy)

	mockKV := mocks.NewMockKVClient(ctrl)
	reqID := fmt.Sprintf("%s-%d", modelName, 0)
	mockKV.EXPECT().WaitForKVReady(gomock.Any(), reqID, gomock.Any()).Return(true)

	// ---------------- Inject mocks ----------------
	// node.SetManagerForTest(mockMgr)
	// kvcclient.SetKVCClient(mockKV)

	// ---------------- Run router ----------------
	tokenpipeRouter := TokenpipeGateway{}
	c := mocks.GenerateRequestContext(1024, 10, modelName)
	tokenpipeRouter.Route(ctx, modelName, c)

	// ---------------- Assert ----------------
	assertRouterTimestamps(t, c)
}

func TestConvertPath(t *testing.T) {
	setupProjectRoot(t)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()
	modelName := "meta-llama/Llama-3.1-8B"
	prefillModel := fmt.Sprintf("%s::prefill", modelName)
	decodeModel := fmt.Sprintf("%s::decode", modelName)
	prefillUUID := "prefill-123"
	decodeUUID := "decode-123"
	convertUUID := "convert-123"

	// ---------------- Register scalers ----------------
	prefillScalerConfig := map[string]interface{}{
		"type":          "KPA",
		"tp_level":      1,
		"target_metric": "rps_counter",
		"target_value":  10000,
		"min_instance":  1,
		"max_instance":  1,
	}
	decodeScalerConfig := map[string]interface{}{
		"type":                       "CAP",
		"tp_level":                   1,
		"capacity_profile_file_path": "entrypoints/configs/capacity_profiles/llama_3.1_8B.json",
		"min_instance":               1,
		"max_instance":               1,
		"convert_instance":           1,
		"convert_max_request":        10,
	}

	prefillScalerChan, err := registerScaler(ctx, prefillModel, prefillScalerConfig)
	if err != nil {
		t.Error(err)
	}
	decodeScalerChan, err := registerScaler(ctx, decodeModel, decodeScalerConfig)
	if err != nil {
		t.Error(err)
	}
	// ---------------- Create tokenpipe route policy ----------------
	createRoutePolicy(ctx)
	registerModelOnRouter(ctx, prefillModel)
	registerModelOnRouter(ctx, decodeModel)

	// ---------------- Register tokenpipe instances ----------------
	// Create instance statuses
	prefillInstanceStatus := model.InstanceStatus{
		UUID:      prefillUUID,
		ModelName: prefillModel,
		HostName:  "localhost",
		Port:      7500,
		GPUIDs:    []string{"0"},
		IsConvert: false,
	}

	decodeInstanceStatus := model.InstanceStatus{
		UUID:      decodeUUID,
		ModelName: decodeModel,
		HostName:  "localhost",
		Port:      7500,
		GPUIDs:    []string{"1"},
		IsConvert: false,
	}

	convertInstanceStatus := model.InstanceStatus{
		UUID:       convertUUID,
		ModelName:  decodeModel,
		HostName:   "localhost",
		Port:       7500,
		GPUIDs:     []string{"2"},
		IsConvert:  true,
		MaxRequest: 10,
	}

	// sleep, wait for autoscaler to send creating instance instruction
	time.Sleep(2 * time.Second)
	// Register them
	registerInstance(ctx, prefillModel, prefillInstanceStatus, prefillScalerChan)
	registerInstance(ctx, decodeModel, decodeInstanceStatus, decodeScalerChan)
	registerInstance(ctx, decodeModel, convertInstanceStatus, decodeScalerChan)

	// ---------------- Create mocks ----------------
	prefillProxy := createMockReverseProxy(ctrl)
	decodeProxy := createMockReverseProxy(ctrl)
	convertProxy := createMockReverseProxy(ctrl)
	mockMgr := mocks.NewMockManager(ctrl)
	mockKV := mocks.NewMockKVClient(ctrl)
	// prefill instance
	// first request
	reqID := fmt.Sprintf("%s-%d", modelName, 0)
	mockKV.EXPECT().WaitForKVReady(gomock.Any(), reqID, gomock.Any()).Return(true)
	setMockManager(mockMgr, prefillUUID, prefillInstanceStatus, prefillProxy)
	setMockManager(mockMgr, decodeUUID, decodeInstanceStatus, decodeProxy)
	// second request
	setMockManager(mockMgr, convertUUID, convertInstanceStatus, convertProxy)
	// third request
	reqID = fmt.Sprintf("%s-%d", modelName, 2)
	mockKV.EXPECT().WaitForKVReady(gomock.Any(), reqID, gomock.Any()).Return(true)
	setMockReverseProxy(prefillProxy)
	setMockReverseProxy(convertProxy)
	setMockManager(mockMgr, prefillUUID, prefillInstanceStatus, prefillProxy)
	setMockManager(mockMgr, convertUUID, convertInstanceStatus, convertProxy)
	// setMockManager(mockMgr, prefillUUID, prefillInstanceStatus, prefillProxy)
	// setMockManager(mockMgr, convertUUID, convertInstanceStatus, convertProxy)

	// ---------------- Inject mocks ----------------
	// node.SetManagerForTest(mockMgr)
	// kvcclient.SetKVCClient(mockKV)

	// ---------------- Run router ----------------
	tokenpipeRouter := TokenpipeGateway{}
	// ---------------- Test and Assert ----------------
	// First request should not skip routing
	c := mocks.GenerateRequestContext(20000, 10, modelName)
	tokenpipeRouter.Route(ctx, modelName, c)
	assertRouterTimestamps(t, c)
	// Second request arrives immediately, should skip routing
	c = mocks.GenerateRequestContext(20000, 10, modelName)
	tokenpipeRouter.Route(ctx, modelName, c)
	assertNotRouterTimestamps(t, c)
	// sleep for 1s, tpsBuffer should refresh
	time.Sleep(1 * time.Second)
	// third request should go through the normal path again
	c = mocks.GenerateRequestContext(20000, 10, modelName)
	tokenpipeRouter.Route(ctx, modelName, c)
	assertRouterTimestamps(t, c)

}
