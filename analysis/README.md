# Analysis data using Grafana

## Step 1: Go snapshot dir and set $SNAPSHOT

``` bash
# cd prometheus/data/snapshots/xxxx
export SNAPSHOT=$(pwd)
```

## Step 2: Start
``` bash
docker compose up -d
```

## Step 3: Follow [Guide](https://github.com/vllm-project/vllm/tree/main/examples/online_serving/prometheus_grafana)