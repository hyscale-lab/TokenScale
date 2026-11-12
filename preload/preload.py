import argparse
import time
import socket
import os
from sllm_store.client import SllmStoreClient
from vllm import LLM
from pydantic import TypeAdapter
import json
from vllm.config import VllmConfig
import lmcache.v1.nixl_agent_pool as nixl_agent_pool
import traceback

def wait_for_grpc_service(host, port, timeout=120):
    """Wait for a gRPC service to be ready by trying to open a socket connection."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with socket.create_connection((host, port), timeout=2):
                print(f"gRPC service at {host}:{port} is ready.")
                return True
        except OSError:
            print(f"Waiting for gRPC service at {host}:{port}...")
            time.sleep(1)
    raise TimeoutError(f"gRPC service at {host}:{port} did not become ready within {timeout} seconds.")

def main():
    parser = argparse.ArgumentParser(description="Load model into CPU memory.")
    parser.add_argument("--model-path", required=True, help="Path to the model directory")
    parser.add_argument("--max-retries", type=int, default=3, help="Maximum retry attempts")
    parser.add_argument("--sllm-store-addr", default="localhost:8073", help="Address of the SLLM Store server (host:port)")
    parser.add_argument("--tensor-parallel-size", help="tensor parallel size of the model")
    args = parser.parse_args()

    # Split address into host and port
    if ":" not in args.sllm_store_addr:
        raise ValueError("sllm-store-addr must be in the form host:port")
    host, port = args.sllm_store_addr.split(":")
    wait_for_grpc_service(host, int(port), timeout=300)

    client = SllmStoreClient(args.sllm_store_addr)

    for attempt in range(1, args.max_retries + 1):
        # start a dummy vllm instance to preload model into cpu memory
        try:
            os.environ['VLLM_KVC_MEM_GB'] = "1"
            llm = LLM(model=args.model_path, load_format="serverless_llm", enforce_eager=True, num_gpu_blocks_override=1, max_model_len=1, tensor_parallel_size=int(args.tensor_parallel_size))
            vllm_config = llm.llm_engine.vllm_config 
            data_json = nixl_agent_pool.serialize_vllm_config(vllm_config)
            vllm_config_path = os.path.join(args.model_path, "vllm_config.json")
            with open(vllm_config_path, 'w') as f:
                json.dump(data_json, f)
            print(f"vllm_config for model {args.model_path} is written to {vllm_config_path}")
        except Exception as e: 
            print(f"[Attempt {attempt}] Failed to load model '{args.model_path}'. error: {e}. Retrying in 5 seconds...")
            traceback.print_exc()
            time.sleep(5)
            continue
            
        print(f"Successfully loaded model '{args.model_path}' into CPU memory, wait for forkserver to be started")
        # wait for forkserver to be started
        wait_for_grpc_service("localhost", 48888)
        print(f"Forkserver has been started!")
        return

    raise RuntimeError(f"Failed to load model '{args.model_path}' into CPU after {args.max_retries} attempts.")

if __name__ == "__main__":
    main()