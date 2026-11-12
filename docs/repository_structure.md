# Repository Structure

| Path | Description |
|---|---|
| [`router/`](../router/) | Control plane (Go), entry point `router/main.go`. Contains the gateway, load balancer, and autoscalers. |
| ├ [`router/gateway/tokenpipe_gateway.go`](../router/gateway/tokenpipe_gateway.go) | TokenScale gateway: burst detection and routing to Convertible Decoders. |
| ├ [`router/autoscaler/`](../router/autoscaler/) | `capacity_scale.go`: Token Velocity decoder autoscaler. `kpa_scale.go`, `hpa_scale.go`, `rps_scale.go`: prefiller and baseline autoscalers. |
| ├ [`router/capacity/`](../router/capacity/) | Per-bucket Token Velocity profiles and the (simulated) output-length predictor. |
| └ [`router/gateway/splitwise.go`](../router/gateway/splitwise.go) | SplitWise baseline. |
| [`liquidlet/`](../liquidlet/) | Per-node agent (Go), entry point `liquidlet/main.go`. Launches and manages vLLM instances. `metrics_patch/` holds the vLLM patches for metrics and cold-start profiling. |
| [`entrypoints/`](../entrypoints/) | Experiment launcher: `entrypoints/main.go -index_path <index.json> -sweep_path <sweep.json>`. Deploys the router, liquidlets, and load generator, then sweeps configurations. |
| ├ [`entrypoints/configs/capacity_profiles/`](../entrypoints/configs/capacity_profiles/) | Token Velocity profiles (Table II) for Llama-3.1-8B and Qwen-2.5-32B. |
| └ [`entrypoints/configs/sweep_{llama,qwen}_*/`](../entrypoints/configs/) | End-to-end experiment configs per model and trace (`conv`, `code`, `mixed`). `tokenpipe.json` = TokenScale |
| [`pd/`](../pd/) | KV-cache transfer proxies for LMCache and Mooncake. |
| [`model_save/`](../model_save/), [`preload/`](../preload/) | Convert models to the ServerlessLLM format and preload them into host memory for fast cold starts. |
| [`usertools/`](../usertools/) | CLI tools and a custom scaler sidecar. |
| [`nscc/`](../nscc/), [`scripts/`](../scripts/) | PBS job scripts for running on an HPC cluster. |
| [`prometheus/`](../prometheus/), [`analysis/`](../analysis/) | Metrics collection and Grafana-based analysis. |
| [`tests/`](../tests/) | Integration tests. |
| `Dockerfile.*` | Container images for the liquidlet (vLLM + LMCache), router, and model saver. |
