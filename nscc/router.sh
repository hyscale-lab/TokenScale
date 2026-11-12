#!/bin/bash

# Set username
username=$(whoami)

# Config for router
export ROUTER_IMAGE="/scratch/users/ntu/$username/liquid_router.sif"
export PROMETHEUS_PORT=8080
export LOG_PATH="$1"
export SD_FILE=$LOG_PATH/prometheus/sd.json
export FULL_LOG_PATH=$LOG_PATH/router/$(hostname).log

module load singularity

create_file_if_not_exists() {
  local file=$1
  if [ ! -f "$file" ]; then
    install -D /dev/null "$file"
    echo "Created $file"
  fi
}
create_file_if_not_exists $SD_FILE
create_file_if_not_exists $FULL_LOG_PATH
# Run liquidlet
singularity run --nv --bind /scratch/users/ntu/$username:/scratch/users/ntu/$username $ROUTER_IMAGE &> $FULL_LOG_PATH 

