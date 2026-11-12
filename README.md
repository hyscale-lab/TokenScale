# TokenScale

This is the open-source repository of the SoCC '26 paper **TokenScale: Timely and Accurate Autoscaling for Disaggregated LLM Serving with Token Velocity** ([arXiv:2512.03416](https://arxiv.org/abs/2512.03416)).

TokenScale is an autoscaling framework for prefill/decode-disaggregated LLM serving. It scales prefillers and decoders based on **Token Velocity**, a leading indicator of backpressure across the prefill, network, and decode stages. It also uses **Convertible Decoders**, decoders that temporarily run prefill work to absorb traffic bursts. In the code, TokenScale is referred to as *TokenPipe* (`tokenpipe`, route policy `TP`).

## Repository Structure

| Path | Description |
|---|---|
| [`router/`](router/) | Control plane (Go), entry point `router/main.go`. Contains the gateway, load balancer, and autoscalers. |
| ├ [`router/gateway/tokenpipe_gateway.go`](router/gateway/tokenpipe_gateway.go) | TokenScale gateway: burst detection and routing to Convertible Decoders. |
| ├ [`router/autoscaler/`](router/autoscaler/) | `capacity_scale.go`: Token Velocity decoder autoscaler. `kpa_scale.go`, `hpa_scale.go`, `rps_scale.go`: prefiller and baseline autoscalers. |
| ├ [`router/capacity/`](router/capacity/) | Per-bucket Token Velocity profiles and the (simulated) output-length predictor. |
| └ [`router/gateway/splitwise.go`](router/gateway/splitwise.go) | SplitWise baseline. |
| [`liquidlet/`](liquidlet/) | Per-node agent (Go), entry point `liquidlet/main.go`. Launches and manages vLLM instances. `metrics_patch/` holds the vLLM patches for metrics and cold-start profiling. |
| [`entrypoints/`](entrypoints/) | Experiment launcher: `entrypoints/main.go -index_path <index.json> -sweep_path <sweep.json>`. Deploys the router, liquidlets, and load generator, then sweeps configurations. |
| ├ [`entrypoints/configs/capacity_profiles/`](entrypoints/configs/capacity_profiles/) | Token Velocity profiles (Table II) for Llama-3.1-8B and Qwen-2.5-32B. |
| └ [`entrypoints/configs/sweep_{llama,qwen}_*/`](entrypoints/configs/) | End-to-end experiment configs per model and trace (`conv`, `code`, `mixed`). `tokenpipe.json` = TokenScale |
| [`pd/`](pd/) | KV-cache transfer proxies for LMCache and Mooncake. |
| [`model_save/`](model_save/), [`preload/`](preload/) | Convert models to the ServerlessLLM format and preload them into host memory for fast cold starts. |
| [`usertools/`](usertools/) | CLI tools and a custom scaler sidecar. |
| [`nscc/`](nscc/), [`scripts/`](scripts/) | PBS job scripts for running on an HPC cluster. |
| [`prometheus/`](prometheus/), [`analysis/`](analysis/) | Metrics collection and Grafana-based analysis. |
| [`tests/`](tests/) | Integration tests. |
| `Dockerfile.*` | Container images for the liquidlet (vLLM + LMCache), router, and model saver. |

## Running Experiments

1. Download and convert model files and upload to NSCC
There is a docker image that download model and conver it into sllm format, execute this command on another machine and upload the downloaded model files to nscc
```
docker run -it --gpus all --rm -v ./models:/models lrq619/save_model --model-name <model-name>
```
2. Pull docker images and convert it as sif image
```
qsub scripts/download_liquidlet_sif.pbs
qsub scripts/download_prometheus.pbs
```
3. Build all binary
```
cd router
go build .
```
4. Clone and build LLMLoadgen
```
git clone LLMLoadgen
cd LLMLoadgen/
go build .
```
5. Prepare trace files that you're going to run
For NTU CCDS student, you can download these traces files from the NTU shared network storage.
You can check the shared network drive
```
/path/to/data/traces/
```

6. Edit configuration files
First, ensure that all configuration file path is pointing to the correct path. Change the user to your current NSCC user. 
For example:
```
"model_path": "/scratch/users/ntu/<your-user>/models"
```
Currently there are several config files that need to be loaded when starting the experiment, you should tell the entrypoint the path to each of the configuration files in an index json file, it's usually in `entrypoints/configs/index.json`, an example json file is like:
```json
{
    "project_root_path": "/home/users/ntu/$USER/proj/Liquid",
    "config_dir_path": "/home/users/ntu/$USER/proj/Liquid/entrypoints/configs",
    "common_config_path": "common_configs/common_config.json",
    "LLMLoadgen_config_path": "loadgen_configs/loadgen_config.json",
    "pd_config_path": "pd_configs/pd.json",
    "model_config_path_list": [ 
        "model_configs/Llama-3.1-8B.json"
    ]
}
```
Then, you can sweep experiment parameters by editing `sweep.json`, an example sweep.json is provided:
```json
{
    "loadgen_configs/loadgen_config.json::sampling_rate": [10,20],
    "autoscale_configs/default_autoscale_config.json::max_request_per_instance": [13, 23]
}
```
Keys in `sweep.json` should follow specific rules: `<config_file_path>::<parameter_key>`, entrypoint will automatically parse these keys and modify the configuration parameters accordingly.
5. Prepare environment variable
```
export PLATFORM=NSCC_2A
```
If it's 2A+
```
export PLATFORM=NSCC_2P
```

5. Start the entrypoint by:
```
./entrypoint -index_path configs/index.json -sweep_path configs/sweep.json
```
## Notice About Sweeping
The "sweep_config" there indicates the configurations that is going to be sweeped during experiment runs
It will generate a full combinations of the sweeped parameters. In the example of 
```json
{
    "loadgen_configs/loadgen_config.json::sampling_rate": [10,20],
    "autoscale_configs/default_autoscale_config.json::max_request_per_instance": [13, 23]
}
```
It will iterate over all combinations of the parameters and generate 2*2 = 4 experiment run configurations and launch the experiment runs at the same time.

For the filepath, please use relative path under `entrypoints/configs` dir

Theoretically you can sweep any parameters defined under the `entrypoints/configs` dir, but this haven't been fully tested.

## PD Disaggregation
### Mooncake PD Disaggregation
Example `sweep.json`:
```json
{
    "index.json::model_config_path_list": [
        [
            "model_configs/mooncake/Llama-3.1-8B_prefill.json",
            "model_configs/mooncake/Llama-3.1-8B_decode.json"
        ]
    ],
    "loadgen_configs/loadgen_config.json::trace_file_path": ["/scratch/users/ntu/$USER/traces/conv/sample_0_all_input1_output1_ALL.json"],
    "loadgen_configs/loadgen_config.json::sampling_rate": [20],
    "common_configs/common_config.json::wait_min": [65],
    "common_configs/common_config.json::nscc_nodes": [1],
    "autoscale_configs/default_prefill_config.json::min_instance": [3],
    "autoscale_configs/default_prefill_config.json::max_instance": [3],
    "autoscale_configs/default_decode_config.json::min_instance": [1],
    "autoscale_configs/default_decode_config.json::max_instance": [1],
    "pd_configs/pd.json::mooncake_master_args": ["\"--port 50001 -eviction-high-watermark-ratio 1 -eviction-ratio 0.1 -enable-gc True\""],
    "pd_configs/pd.json::pd_mode":["mooncake"]
}
```
Need to register both prefill and decode models first
This starts a 1 prefill instance and 3 decode instances, when request arrives, it will be routed using round-robin policy between these three decode instances.

### LMCache PD Disaggregation
Example `sweep.json`:
```json
{
    "index.json::model_config_path_list": [
        [
            "model_configs/lmcache/Llama-3.1-8B_prefill.json",
            "model_configs/lmcache/Llama-3.1-8B_decode.json"
        ]
    ],
    "loadgen_configs/loadgen_config.json::trace_file_path": [
        "/scratch/users/ntu/$USER/traces/conv/sample_0_all_input1_output1_no_prefix.json"
    ],
    "loadgen_configs/loadgen_config.json::sampling_rate": [20],
    "common_configs/common_config.json::wait_min": [5],
    "common_configs/common_config.json::nscc_nodes": [2],
    "autoscale_configs/default_prefill_config.json::min_instance": [1],
    "autoscale_configs/default_prefill_config.json::max_instance": [8],
    "autoscale_configs/default_prefill_config.json::max_request_per_instance": [13],
    "autoscale_configs/default_decode_config.json::min_instance": [1],
    "autoscale_configs/default_decode_config.json::max_instance": [8],
    "autoscale_configs/default_decode_config.json::max_request_per_instance": [13],
    "pd_configs/pd.json::pd_mode":["lmcache"]
}
```
## Notice about Autoscaler Configs
Notice that since there can be separate autoscaler for different models, you should specify the autoscaler configuration in the `model_configs/<your-model>.json::autoscaler_config_path`, an example configuration of model configuration is as following:
```json
{
  "model_name": "meta-llama/Llama-3.1-8B",
  "cli": "vllm",
  "env": [],
  "args": [
    "serve",
    "/models/meta-llama/Llama-3.1-8B",
    "--gpu-memory-utilization",
    "0.9",
    "--load-format",
    "serverless_llm",
    "--served-model-name",
    "meta-llama/Llama-3.1-8B",
    "--enable-chunked-prefill",
    "True",
    "--max-num-batched-token",
    "1024"
  ],
  "tp_arg": "--tensor-parallel-size",
  "host_arg": "--host",
  "port_arg": "--port",
  "autoscaler_config_path": "autoscale_configs/default_autoscale_config.json"
}
```

## To run on NSCC 2A/NSCC 2A+
You need to specify platform before run:
For NSCC 2A:
```
export PLATFORM=NSCC_2A
```
For NSCC 2A+:
```
export PLATFORM=NSCC_2P
export NO_PROXY=*
```

## To write unit test
```
mockgen -source=router/node/manager/manager.go -destination=router/mocks/mock_manager.go -package=mocks
```