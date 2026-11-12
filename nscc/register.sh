#!/bin/bash

ROUTER_URL="$1"
MODEL_CONFIG_PATH="$2"

echo "ROUTER_URL: $ROUTER_URL"
echo "MODEL_CONFIG_PATH: $MODEL_CONFIG_PATH"

register_node () {
  local url=$1
  local json_path=$2

  curl -X POST "$url" \
    -H "Content-Type: application/json" \
    -d @"$json_path"
}

for file in "$MODEL_CONFIG_PATH"/*.json; do
  echo "Registering $file"
  echo "Using router URL: $ROUTER_URL"
  register_node "$ROUTER_URL" "$file"
done
