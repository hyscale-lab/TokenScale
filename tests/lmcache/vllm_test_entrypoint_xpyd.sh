#!/bin/bash
echo "Starting sllm-store with arguments: --storage-path=$STORAGE_PATH $@"
exec sllm-store start --storage-path="$STORAGE_PATH" "$@" &

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

Start prefill instance
UCX_TLS=cuda_ipc,cuda_copy,tcp LMCACHE_CONFIG_FILE=/app/lmcache-prefill.yaml VLLM_ENABLE_V1_MULTIPROCESSING=1 VLLM_WORKER_MULTIPROC_METHOD=spawn CUDA_VISIBLE_DEVICES=0 vllm serve /models/meta-llama/Llama-3.1-8B \
  --port 8100 \
  --disable-log-requests \
  --enforce-eager \
  --kv-transfer-config '{"kv_connector":"LMCacheConnectorV1","kv_role":"kv_producer","kv_connector_extra_config": {"discard_partial_chunks": false, "lmcache_rpc_port": "producer1"}}' \
  --load-format serverless_llm \
  --served-model-name meta-llama/Llama-3.1-8B &

# Start decode instance
UCX_TLS=cuda_ipc,cuda_copy,tcp LMCACHE_CONFIG_FILE=/app/lmcache-decode.yaml VLLM_ENABLE_V1_MULTIPROCESSING=1 VLLM_WORKER_MULTIPROC_METHOD=spawn CUDA_VISIBLE_DEVICES=1 vllm serve /models/meta-llama/Llama-3.1-8B \
  --port 8200 \
  --disable-log-requests \
  --enforce-eager \
  --kv-transfer-config '{"kv_connector":"LMCacheConnectorV1","kv_role":"kv_consumer","kv_connector_extra_config": {"discard_partial_chunks": false, "lmcache_rpc_port": "consumer1", "skip_last_n_tokens": 1}}' \
  --load-format serverless_llm \
  --served-model-name meta-llama/Llama-3.1-8B &

Wait for prefill and decode servers to be ready
wait_for_server 8100
wait_for_server 8200

# Start the proxy server
# python lmcache_proxy_server.py --model meta-llama/Llama-3.1-8B --prefill localhost:8100 --decode localhost:8200 --port 8000
python lmcache_proxy_server.py \
        --host 0.0.0.0 \
        --port 8000 \
        --prefiller-host localhost \
        --prefiller-port 8100 \
        --num-prefillers 1 \
        --decoder-host localhost \
        --decoder-port 8200  \
        --decoder-init-port 7300 \
        --decoder-alloc-port 7400 \
        --proxy-host localhost \
        --proxy-port 7500 \
        --num-decoders 1 \