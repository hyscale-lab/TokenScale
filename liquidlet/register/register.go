package register

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"

	"liquidlet/model"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/cloudwego/hertz/pkg/app/client"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func init() {
	ctx := context.Background()
	gpuDetect(ctx)
	hostDetect(ctx)
	portDetect(ctx)
	routerDetect(ctx)
}

var (
	gpuUUIDs []string = make([]string, 0)
	host     string
	port     int
	router   string
)

func gpuDetect(ctx context.Context) {
	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		hlog.CtxErrorf(ctx, "Failed to initialize NVML: %v", nvml.ErrorString(ret))
		return
	}
	defer func() {
		ret := nvml.Shutdown()
		if ret != nvml.SUCCESS {
			hlog.CtxErrorf(ctx, "Failed to shutdown NVML: %v", nvml.ErrorString(ret))
		}
	}()
	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		hlog.CtxErrorf(ctx, "Unable to get device count: %v", nvml.ErrorString(ret))
	}

	for i := 0; i < count; i++ {
		device, ret := nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			hlog.CtxErrorf(ctx, "Unable to get device at index %d: %v", i, nvml.ErrorString(ret))
		}
		uuid, ret := device.GetUUID()
		if ret != nvml.SUCCESS {
			hlog.CtxErrorf(ctx, "Unable to get uuid of device at index %d: %v", i, nvml.ErrorString(ret))
		}
		hlog.CtxInfof(ctx, "Device %d: %s", i, uuid)
		gpuUUIDs = append(gpuUUIDs, uuid)
	}
}

func hostDetect(ctx context.Context) {
	host = os.Getenv("DAEMON_HOST")
	if host != "" {
		hlog.CtxInfof(ctx, "Host address is set to %s", host)
		return
	}
	hlog.CtxInfof(ctx, "Host address is not set, trying to detect it")
	interfaces, err := net.Interfaces()
	if err != nil {
		hlog.CtxFatalf(ctx, "Failed to get interfaces: %v", err)
	}
	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			hlog.CtxFatalf(ctx, "Failed to get addresses of interface %s: %v", iface.Name, err)
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
				host = ipnet.IP.String()
				hlog.CtxInfof(ctx, "Host address is detected as %s", host)
				return
			}
		}
	}
	hlog.CtxFatalf(ctx, "Failed to get host address")
}

func portDetect(ctx context.Context) {
	portStr := os.Getenv("DAEMON_PORT")
	var err error
	if portStr != "" {
		port, err = strconv.Atoi(portStr)

		if err != nil {
			hlog.CtxFatalf(ctx, "Failed to get port: %v", err)
		}
	} else {
		port = 9000
	}
	hlog.CtxInfof(ctx, "Port is set to %d", port)
}

func routerDetect(ctx context.Context) {
	router = os.Getenv("ROUTER")
	if router == "" {
		hlog.CtxInfof(ctx, "Router address is not set, setting to default")
		router = fmt.Sprintf("http://%s:%d/register_node", host, 8888)
		hlog.CtxInfof(ctx, "Router address is set to %s", router)
	}
}

func GetHostPort() string {
	return fmt.Sprintf(":%v", port)
}

func Register() {
	data := model.RegisterReq{
		GPUUUIDs: gpuUUIDs,
		Host:     host,
		Port:     port,
	}
	req := &protocol.Request{}
	res := &protocol.Response{}
	req.Header.SetMethod(consts.MethodPost)
	req.Header.SetContentTypeBytes([]byte("application/json"))
	req.SetRequestURI(router)
	jsonByte, _ := json.Marshal(data)
	req.SetBody(jsonByte)
	err := client.Do(context.Background(), req, res)
	if err != nil {
		hlog.CtxErrorf(context.Background(), "Failed to register: %v to %v", err, router)
	}
}
