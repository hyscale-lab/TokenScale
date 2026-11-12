package detect

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"

	"github.com/cloudwego/hertz/pkg/common/hlog"
)

func init() {
	ctx := context.Background()
	hostDetect(ctx)
	portDetect(ctx)
	promDetect(ctx)
}

var (
	host              string
	port              int
	metrics           int
	prometheusAddress string
)

func hostDetect(ctx context.Context) {
	host = os.Getenv("ROUTER_HOST")
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
	portStr := os.Getenv("ROUTER_PORT")
	var err error
	if portStr != "" {
		port, err = strconv.Atoi(portStr)
		if err != nil {
			hlog.CtxFatalf(ctx, "Failed to get port: %v", err)
		}
	} else {
		port = 8888
	}
	prometheusStr := os.Getenv("METRICS_PORT")
	if prometheusStr != "" {
		metrics, err = strconv.Atoi(prometheusStr)
		if err != nil {
			hlog.CtxFatalf(ctx, "Failed to get prometheus port: %v", err)
		}
	} else {
		metrics = 9090
	}
	hlog.CtxInfof(ctx, "Port is set to %d", port)
}

func promDetect(ctx context.Context) {
	prometheusAddress = os.Getenv("PROMETHEUS_ADDR")
	if prometheusAddress == "" {
		prometheusAddress = "localhost:9090"
	}
	hlog.CtxInfof(ctx, "Prometheus url is set to %s", prometheusAddress)
}

func GetHostPort() string {
	return fmt.Sprintf(":%v", port)
}

func GetMetricsPort() string {
	return fmt.Sprintf("%v:%v", host, metrics)
}

func GetPrometheusAddress() string {
	return prometheusAddress
}
