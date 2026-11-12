#!/bin/bash

set -e

initialize_mooncake_master_service() {
  echo "Use mooncake for PD disaggregation"
  echo "Starting etcd in the background..."
  etcd --listen-client-urls http://0.0.0.0:2379 --advertise-client-urls http://localhost:2379 --data-dir $ETCD_DATA_DIR &

  echo "Starting mooncake_master in the background..."
  eval "mooncake_master $MOONCAKE_MASTER_ARGS &"
}

initialize_lmcache() {
  echo "Use lmcache for PD disaggregation"
}

initialize_liquidlet() {
  echo "Starting liquidlet in the background..."
  liquidlet &
}

main() {
  if [ "$PD_MODE" = "mooncake" ]; then
    initialize_mooncake_master_service
  elif [ "$PD_MODE" = "lmcache" ]; then
    initialize_lmcache
  fi

  initialize_liquidlet

  echo "Starting forkserver in the background..."
  python /app/liquidlet/forkserver/forkserver.py &

  if [ -n "$EXT_SLLM" ]; then
    echo "EXT_SLLM is set. Skipping sllm-store startup. Sleeping indefinitely..."
    sleep infinity
  else
    echo "Starting sllm-store with arguments: --storage-path=$STORAGE_PATH $@"
    exec sllm-store start --storage-path="$STORAGE_PATH" "$@"
  fi
}

main "$@"