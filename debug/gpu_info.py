import subprocess

def get_gpu_info():
    # Query total and used memory for each GPU
    gpu_info_cmd = [
        "nvidia-smi",
        "--query-gpu=index,memory.total,memory.used",
        "--format=csv,noheader,nounits"
    ]
    gpu_info_output = subprocess.check_output(gpu_info_cmd).decode().strip().splitlines()

    # Query running processes for each GPU
    proc_info_cmd = [
        "nvidia-smi",
        "--query-compute-apps=gpu_uuid",
        "--format=csv,noheader"
    ]
    proc_info_output = subprocess.check_output(proc_info_cmd).decode().strip().splitlines()

    # Count processes per GPU
    process_count = {}
    for line in proc_info_output:
        gpu_uuid = line.strip()
        process_count[gpu_uuid] = process_count.get(gpu_uuid, 0) + 1

    # Map GPU index -> UUID
    uuid_map_cmd = [
        "nvidia-smi",
        "--query-gpu=index,uuid",
        "--format=csv,noheader"
    ]
    uuid_map_output = subprocess.check_output(uuid_map_cmd).decode().strip().splitlines()
    idx_to_uuid = {int(idx): uuid for idx, uuid in (l.split(",") for l in uuid_map_output)}

    # Print info in GB
    for line in gpu_info_output:
        idx_str, mem_total_mib, mem_used_mib = [x.strip() for x in line.split(",")]
        idx = int(idx_str)
        uuid = idx_to_uuid[idx]
        num_procs = process_count.get(uuid, 0)

        mem_total_gb = float(mem_total_mib) / 1024
        mem_used_gb = float(mem_used_mib) / 1024

        print(f"[GPU{idx}]: running processes: {num_procs}, "
              f"memory in use: {mem_used_gb:.2f} GB, "
              f"memory capacity: {mem_total_gb:.2f} GB")

if __name__ == "__main__":
    get_gpu_info()