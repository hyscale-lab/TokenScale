#!/bin/bash
#PBS -N GPU_JOB
#PBS -l select=2:ngpus=4
#PBS -l walltime=4:00:00
#PBS -P YOUR_PROJECT_ID
#PBS -q normal
#PBS -j oe

# Set script to exit on any error
set -e

# Get the username of the current user
username=$(whoami)

FIRST_NODE=$(head -n 1 $PBS_NODEFILE)
IP_ADDRESS=$(ssh $FIRST_NODE "hostname -i")
LOG_PATH="/home/users/ntu/$username/nscc/logs_${PBS_JOBID}_$(date +%Y%m%d_%H%M%S)"

SCRIPT_PATH="/home/users/ntu/$username/nscc"
MODEL_CONFIG_PATH=${1:-"$SCRIPT_PATH/sample_config"}
LOADGEN_SCRIPT_PATH=${2:-"$SCRIPT_PATH/sample_loadgen"}
echo "LOG_PATH: $LOG_PATH"
echo "SCRIPT_PATH: $SCRIPT_PATH"
echo "MODEL_CONFIG_PATH: $MODEL_CONFIG_PATH"
echo "LOADGEN_SCRIPT_PATH: $LOADGEN_SCRIPT_PATH"
# Define the router URL
export ROUTER_URL="http://$IP_ADDRESS:8888/register_node"
export MODEL_URL="http://$IP_ADDRESS:8888/register_model"
# ssh to first node and start router
ssh $FIRST_NODE "cd $SCRIPT_PATH && ./router.sh $LOG_PATH" &
sleep 5
# ssh to first node and start prometheus
ssh $FIRST_NODE "cd $SCRIPT_PATH && ./prometheus.sh $LOG_PATH" &
sleep 5
# ssh to all nodes and start liquidlet
for node in $(cat $PBS_NODEFILE); do
  ssh $node "cd $SCRIPT_PATH && ./liquidlet.sh $ROUTER_URL $LOG_PATH" &
done

sleep 5

echo "All services are started"
echo "To register node on $ROUTER_URL with $MODEL_CONFIG_PATH"
# register
ssh $FIRST_NODE "cd $SCRIPT_PATH && ./register.sh $MODEL_URL $MODEL_CONFIG_PATH"

# start LLM Loadgen
ssh $FIRST_NODE "cd $LOADGEN_SCRIPT_PATH && ./run.sh"

# ssh to first node and do pre-stop snapshot
ssh $FIRST_NODE "cd $SCRIPT_PATH && ./pre-stop.sh"

