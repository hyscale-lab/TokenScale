#!/bin/bash

set -e

start_redis() {
  echo "[entrypoint] Starting Redis..."
  redis-server --daemonize yes
}

start_aibrix_app() {
  echo "[entrypoint] Starting AIBrix app_liquid.py..."
  export MODELS="meta-llama/Llama-3.1-8B"

  percentile=99
  ttft=2
  tpot=0.1
  port=8090
  tput_tolerance=0.9

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --percentile)
        percentile="$2"
        shift 2
        ;;
      --ttft)
        ttft="$2"
        shift 2
        ;;
      --tpot)
        tpot="$2"
        shift 2
        ;;
      --port)
        port="$2"
        shift 2
        ;;
      --tput_tolerance)
        tput_tolerance="$2"
        shift 2
        ;;
      *)
        echo "[entrypoint] Unknown option: $1"
        shift
        ;;
    esac
  done

  echo "[entrypoint] Setting TPUT_TOLERANCE=$tput_tolerance"
  export TPUT_TOLERANCE="$tput_tolerance" 
  echo "[entrypoint] Generating profile with --percentile $percentile --ttft $ttft --tpot $tpot"
  aibrix_gen_profile meta-llama/Llama-3.1-8B \
    --cost 1 \
    --percentile "$percentile" \
    --ttft "$ttft" \
    --tpot "$tpot" \
    -o "redis://localhost:6379/?model=meta-llama/Llama-3.1-8B"

  echo "[entrypoint] Launching app_liquid.py with --port $port"
  python3 /app/aibrix-liquid/python/aibrix/aibrix/gpu_optimizer/app_liquid.py --port "$port"
}

main() {
  start_redis
  start_aibrix_app
}

main "$@"
