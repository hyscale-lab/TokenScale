import lmcache.v1.nixl_agent_pool as nixl_agent_pool
import torch
import multiprocessing as mp

def init_paged_cpu_buffer(cpu_buffer: torch.Tensor):
    cpu_buffer = cpu_buffer.view(torch.uint8).flatten()
    cpu_buffer_size = cpu_buffer.numel() * cpu_buffer.element_size()

    align_bytes = 32 * (1024**2) 
    cpu_buffer_size = (cpu_buffer_size // align_bytes) * align_bytes
    cpu_buffer = cpu_buffer[:cpu_buffer_size]
    
    cpu_paged_buffers = torch.split(cpu_buffer, align_bytes, dim=0)
    return cpu_paged_buffers
    

def child_process(abs_rank):

    print("[Child] Initializing CUDA context manually...")
    try:
        torch._C._cuda_init()
        print("[Child] CUDA initialized successfully.")
        agent, reg_descs, xfer_descs, xfer_handler = nixl_agent_pool.get_nixl_agent(abs_rank)
        print(f"[Child] gets agent on rank: {abs_rank}")
        cpu_buffer = nixl_agent_pool.get_cpu_buffer(abs_rank)
        paged_buffers = init_paged_cpu_buffer(cpu_buffer)
        print(f"[Child] cpu_buffer shape: {cpu_buffer.shape}")
        print(f"[Child] paged cpu buffer shape: {paged_buffers[0].shape}")
        tmp_gpu_buffer = torch.ones_like(paged_buffers[0], device='cuda')
        print(f"[Child] copy started!")
        paged_buffers[0].copy_(tmp_gpu_buffer)
        torch.cuda.synchronize()
        print(f"[Child] copy finished!")
    except Exception as e:
        import traceback
        print("[Child] CUDA initialization failed:")
        traceback.print_exc()

if __name__ == "__main__":
    mp.set_start_method("fork", force=True)
    num_gpus = 2
    model_path = "/models/Qwen/Qwen2.5-32B/"
    nixl_agent_pool.init_nixl_maps(num_gpus, model_path)

    print("[Parent] Forking child process...")
    cpu_buffer = nixl_agent_pool.get_cpu_buffer(0)
    print(f"[Parent] Before child, tensor: {cpu_buffer}")
    p = mp.Process(target=child_process, args=(0,))
    p.start()
    p.join()
    print(f"[Parent] After child, tensor: {cpu_buffer}")


    print(f"[Parent] Child exit code: {p.exitcode}")