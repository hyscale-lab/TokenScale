#!/bin/bash
echo "Starting sllm-store with arguments: --storage-path=$STORAGE_PATH $@"
exec sllm-store start --storage-path="$STORAGE_PATH" "$@" &

initialize_mooncake_master_service() {
  echo "Use mooncake for PD disaggregation"
  echo "Starting etcd in the background..."
  etcd --listen-client-urls http://0.0.0.0:2379 --advertise-client-urls http://localhost:2379 &

  echo "Starting mooncake_master in the background..."
  eval "mooncake_master $MOONCAKE_MASTER_ARGS &"
}

initialize_mooncake_master_service

wait_for_server() {
  local port=$1
  local timeout_seconds=1200
  local start_time
  start_time=$(date +%s)

  echo "Waiting for server on port $port..."

  while true; do
    if curl -s "localhost:${port}/v1/completions" > /dev/null; then
      echo "Server on port $port is ready."
      return 0
    fi

    local now
    now=$(date +%s)
    if (( now - start_time >= timeout_seconds )); then
      echo "Timeout waiting for server on port $port"
      return 1
    fi

    sleep 1
  done
}

# Start prefill instance
MOONCAKE_CONFIG_PATH=/app/mooncake_prefill.json VLLM_USE_V1=0 CUDA_VISIBLE_DEVICES=0 vllm serve /models/meta-llama/Llama-3.1-8B --port 8100 --gpu-memory-utilization 0.9 --load-format serverless_llm --kv-transfer-config '{"kv_connector":"MooncakeStoreConnector","kv_role":"kv_producer"}' --max-model-len 74767 --served-model-name meta-llama/Llama-3.1-8B &

# Start decode instance
MOONCAKE_CONFIG_PATH=/app/mooncake_decode.json VLLM_USE_V1=0 CUDA_VISIBLE_DEVICES=1 vllm serve /models/meta-llama/Llama-3.1-8B --port 8200 --gpu-memory-utilization 0.9 --load-format serverless_llm --kv-transfer-config '{"kv_connector":"MooncakeStoreConnector","kv_role":"kv_consumer"}' --max-model-len 74767 --served-model-name meta-llama/Llama-3.1-8B &

# Wait for prefill and decode servers to be ready
wait_for_server 8100
wait_for_server 8200

# Start the proxy server
python proxy_server.py --model meta-llama/Llama-3.1-8B --prefill localhost:8100 --decode localhost:8200 --port 8000