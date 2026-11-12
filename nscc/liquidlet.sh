#!/bin/bash

# Set username
username=$(whoami)

# Config for liquidlet
export STORAGE_PATH="/models"
export LOCAL_STORAGE_PATH="/scratch/users/ntu/$username/models"
export LIQUIDLET_IMAGE="/scratch/users/ntu/$username/liquid_liquidlet.sif"
export ROUTER="$1"
export LOG_PATH="$2"
export FULL_LOG_PATH=$LOG_PATH/liquidlet/$(hostname).log

module load singularity

create_file_if_not_exists() {
  local file=$1
  if [ ! -f "$file" ]; then
    install -D /dev/null "$file"
    echo "Created $file"
  fi
}

create_file_if_not_exists $FULL_LOG_PATH
# Run liquidlet
singularity run --nv --bind /scratch/users/ntu/$username:/scratch/users/ntu/$username --bind $LOCAL_STORAGE_PATH:$STORAGE_PATH $LIQUIDLET_IMAGE --mem-pool-size 32GB &> $FULL_LOG_PATH 

