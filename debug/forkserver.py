import os
import re
import sys
import signal
import multiprocessing
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from typing import List, Dict
import uvicorn
import vllm.global_var as gv
import time
import threading
import subprocess
from datetime import datetime
import logging
import lmcache.v1.nixl_agent_pool as nixl_agent_pool
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S"
)
logger = logging.getLogger("Forkserver")


DEFAULT_FORKSERVER_PORT = "48888"

def get_gpu_memory_leak():
    """Returns a dict {gpu_id: leaked_mem_MB}"""
    # Get total GPU memory usage per GPU
    smi_total_cmd = [
        "nvidia-smi",
        "--query-gpu=index,memory.used",
        "--format=csv,noheader,nounits"
    ]
    total_output = subprocess.check_output(smi_total_cmd, encoding="utf-8").strip().split("\n")

    gpu_total = {}
    for line in total_output:
        gpu_id, mem_used = line.split(",")
        gpu_total[int(gpu_id.strip())] = int(mem_used.strip())

    # Get per-process memory usage
    smi_proc_cmd = [
        "nvidia-smi",
        "--query-compute-apps=gpu_uuid,used_memory",
        "--format=csv,noheader,nounits"
    ]
    proc_output = subprocess.check_output(smi_proc_cmd, encoding="utf-8").strip()

    gpu_proc_sum = {gpu_id: 0 for gpu_id in gpu_total}
    if proc_output:
        for line in proc_output.split("\n"):
            uuid, mem_used = line.split(",")
            mem_used = int(mem_used.strip())
            # Use uuid → index mapping
            # First get uuid to index mapping
            smi_uuid_cmd = [
                "nvidia-smi",
                "--query-gpu=index,uuid",
                "--format=csv,noheader,nounits"
            ]
            uuid_map = subprocess.check_output(smi_uuid_cmd, encoding="utf-8").strip().split("\n")
            uuid_to_idx = {u.strip(): int(i.strip()) for i, u in (row.split(",") for row in uuid_map)}

            gpu_id = uuid_to_idx[uuid.strip()]
            gpu_proc_sum[gpu_id] += mem_used

    # Calculate leak
    leak = {}
    for gpu_id in gpu_total:
        leak[gpu_id] = gpu_total[gpu_id] - gpu_proc_sum[gpu_id]
    return leak

import os
import time
import json
import subprocess

def record_gpu_power(dst_file: str):
    """
    Reads current GPU power usage (W) using nvidia-smi and appends a JSON line to `dst_file`.

    The record format is:
      {
        "timestamp": 1698138720.124,
        "worker": "worker-name",
        "power_readings": [120.5, 130.2, 118.9]
      }

    The 'worker' field is read from the environment variable `DAEMON_HOST`.
    """
    # --- 1. Get current time ---
    timestamp = time.time()

    # --- 2. Get worker name ---
    worker = os.environ.get("DAEMON_HOST", "unknown")

    # --- 3. Query GPU power readings ---
    try:
        output = subprocess.check_output(
            ["nvidia-smi", "--query-gpu=power.draw", "--format=csv,noheader,nounits"],
            encoding="utf-8",
            stderr=subprocess.DEVNULL
        )
        power_readings = [float(x.strip()) for x in output.splitlines() if x.strip()]
    except Exception as e:
        power_readings = []
        print(f"[WARN] Failed to read GPU power via nvidia-smi: {e}")

    # --- 4. Compose JSON record ---
    record = {
        "timestamp": timestamp,
        "worker": worker,
        "power_readings": power_readings
    }

    # --- 5. Append to file atomically (safe for concurrent writers) ---
    with open(dst_file, "a") as f:
        f.write(json.dumps(record) + "\n")

def monitor_leak(interval=5):
    run_log_dir = "/run_log_dir"
    power_filepath = os.path.join(run_log_dir, "power.json")

    while True:
        record_gpu_power(power_filepath)
        leak = get_gpu_memory_leak()
        timestamp = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
        leaks_str = [f"GPU {gpu_id}: {leaked_mem} MB" for gpu_id, leaked_mem in leak.items()]
        logger.info(f"[{timestamp}] [Leaked Memory] {leaks_str}")
        time.sleep(interval)
# prewarm
def prewarm():
    logger.info(f"Prewarm start ...")
    start_time = time.time()
    from vllm.entrypoints.cli.main import main as vllm_main
    # from vllm.entrypoints.openai.api_server import main as vllm_main
    print("Loaded modules time:", time.time()-start_time)
    start = time.time()
    from vllm.entrypoints.openai.cli_args import make_arg_parser
    from vllm.utils.argparse_utils import FlexibleArgumentParser
    os.environ["VLLM_DISK_CACHE_PATH"] = "/tmp/vllm_cache"
    make_arg_parser(FlexibleArgumentParser())
    logger.info(f"Prewarming cli args takes: {time.time() - start:.4f}s")
    start = time.time()
    from vllm.transformers_utils.tokenizer import get_tokenizer
    tokenizer_config = {'max_loras': 0, 'tokenizer_mode': 'auto', 'trust_remote_code': False, 'revision': None, 'truncation_side': 'left'}
    logger.info(f"Start to prewarm tokenizer...")
    for model_family in ["meta-llama", "Qwen"]:
        family_dir = os.path.join("/models", model_family)
        if not os.path.isdir(family_dir):
            continue

        for model_name in os.listdir(family_dir):
            model_path = os.path.join(family_dir, model_name)
            if not os.path.isdir(model_path):
                continue

            tokenizer_id = model_path
            try:
                tokenizer = get_tokenizer(tokenizer_id, **tokenizer_config)
                gv.TOKENIZER_MAP[tokenizer_id] = tokenizer
                logger.info(f"Prewarmed tokenizer for {tokenizer_id}")
            except Exception as e:
                print(f"Failed to prewarm the tokenizer for {tokenizer_id}: {e}")

        print(f"Prewarming tokenizer setup takes: {time.time() - start:.4f}s")
    # logger.info(f"Start to init nixl pool")
    # model_path = "/models/Qwen/Qwen2.5-32B"
    # num_gpus = 4
    # nixl_agent_pool.init_nixl_maps(num_gpus, model_path)
    # logger.info(f"Nixl agent pool inited!")

os.environ["VLLM_FUNC_CACHE_PATH"] = "/tmp/func_cache"
app = FastAPI()
process_table: Dict[str, int] = {}

class CreateRequest(BaseModel):
    uuid: str
    cli: str
    args: List[str]
    env: List[str]

class DeleteRequest(BaseModel):
    uuid: str

def run_vllm_main(cli: str, args_list: List[str], env_list: List[str]):
    try:
        from vllm.entrypoints.cli.main import main as vllm_main
        # from vllm.entrypoints.openai.api_server import main as vllm_main
        # Set per-process environment
        os.setsid()
        print(env_list)
        for item in env_list:
            if "=" in item:
                k, v = item.split("=", 1)
                os.environ[k] = v
        print("[Child ENV]", os.environ.get("CUDA_VISIBLE_DEVICES"))

        # Prepare sys.argv for main()
        sys.argv = [cli] + args_list
        sys.argv[0] = re.sub(r"(-script\.pyw|\.exe)?$", "", sys.argv[0])
        print("[Forked PGID]", os.getpgid(os.getpid()))
        print("[Forked PID]", os.getpid())

        # Call CLI main() inside subprocess
        try:
            vllm_main()
        except Exception as e:
            print("vLLM main exited with error:", e)
    except Exception as e:
        print("Failed to run forked process:", e)

@app.post("/create")
def create_instance(req: CreateRequest):
    # if req.cli != "vllm":
    #     raise HTTPException(status_code=400, detail="Only 'vllm' is supported as cli")

    if req.uuid in process_table:
        raise HTTPException(status_code=400, detail=f"UUID {req.uuid} already exists")

    try:
        ctx = multiprocessing.get_context("fork")
        proc = ctx.Process(
            target=run_vllm_main,
            args=(req.cli, req.args, req.env),
        )
        proc.start()
        process_table[req.uuid] = proc.pid
        return {"pid": proc.pid}
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to launch vllm: {str(e)}")

@app.post("/delete")
def delete_instance(req: DeleteRequest):
    print(f"uuid is {req.uuid}")
    pid = process_table.get(req.uuid)
    if pid is None:
        raise HTTPException(status_code=404, detail=f"UUID {req.uuid} not found")
    try:
        os.killpg(pid, signal.SIGKILL)
        del process_table[req.uuid]
        return {"status": "deleted"}
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to kill process: {str(e)}")

def main():
    logger.info(f"Going to start forkserver...")
    prewarm()
    t = threading.Thread(target=monitor_leak, args=(5,), daemon=True)
    t.start()
    port_str = os.environ.get("FORKSERVER_PORT", DEFAULT_FORKSERVER_PORT)
    port = int(port_str)
    print(f"Going to start forkserver on port {port}")
    uvicorn.run("forkserver:app", host="0.0.0.0", port=port, reload=False)

if __name__ == "__main__":
    main()
