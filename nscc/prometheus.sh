#!/bin/bash

# Set username
username=$(whoami)

# Config for router
export ROUTER_IMAGE="/scratch/users/ntu/$username/prometheus_v3.2.1.sif"
export LOG_PATH="$1"
export SD_FILE=$LOG_PATH/prometheus/sd.json
export FULL_LOG_PATH=$LOG_PATH/prometheus/$(hostname).log
export CONFIG_FILE=$LOG_PATH/prometheus/prometheus.yml
export DATA_PATH=$LOG_PATH/prometheus/data

mkdir -p $DATA_PATH

create_file_if_not_exists() {
  local file=$1
  if [ ! -f "$file" ]; then
    install -D /dev/null "$file"
    echo "Created $file"
  fi
}

create_file_if_not_exists $SD_FILE
create_file_if_not_exists $FULL_LOG_PATH
create_file_if_not_exists $CONFIG_FILE

cat <<EOF > $CONFIG_FILE
global:
  scrape_interval: 1s
  evaluation_interval: 1s

scrape_configs:
  - job_name: 'liquid'
    file_sd_configs:
      - files:
          - sd.json
        refresh_interval: 1s
EOF


module load singularity

# Run prometheus
singularity run --nv --bind /scratch/users/ntu/$username:/scratch/users/ntu/$username --bind $CONFIG_FILE:/etc/prometheus/prometheus.yml --bind $SD_FILE:/etc/prometheus/sd.json $ROUTER_IMAGE --config.file=/etc/prometheus/prometheus.yml --storage.tsdb.path=$DATA_PATH --web.enable-admin-api &> $FULL_LOG_PATH