
from tests.utils import wait_for_server, get_project_root_path, register_model, wait_for_instances, format_performance_metrics, generate_prompt, analyze_trace
from tests.preload import preload
import os
import pytest
import time

import subprocess
import unittest
import requests
import json

class TestLiquidletContainer(unittest.TestCase):
    router_proc = None
    @classmethod
    def _cleanup(cls):
        # Stop the container if it's still running
        docker_logs_path = os.path.join(cls.project_root_path, "logs", "docker_logs.log")
        if cls.loadgen_process != None:
            cls.loadgen_process.kill()  # Graceful shutdown

        try:
            with open(docker_logs_path, "w") as f:
                subprocess.run(
                    ["docker", "logs", cls.container_name],
                    stdout=f,
                    stderr=subprocess.STDOUT,
                    check=False
                )
        except Exception as e:
            print(f"Failed to write docker logs: {e}")
        subprocess.run(["docker", "rm", "-f", cls.container_name], stderr=subprocess.DEVNULL)

        if cls.router_proc and cls.router_proc.poll() is None:  # still running
            print("Terminating router...")
            cls.router_proc.terminate()
            try:
                cls.router_proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                print("Killing router after timeout...")
                cls.router_proc.kill()

    @classmethod
    def setUpClass(cls):
        try:
            cls.project_root_path = get_project_root_path()
            cls.model_name = "meta-llama/Llama-3.1-8B"
            cls.container_name = "liquidlet_lmcache_test"
            cls.prefill_register_file_path = os.path.join(cls.project_root_path, "tests/lmcache/Llama-3.1-8B_prefill.json")
            cls.decode_register_file_path = os.path.join(cls.project_root_path, "tests/lmcache/Llama-3.1-8B_decode.json")
            cls.router_host = "0.0.0.0"
            cls.liquidlet_port = 9000
            cls.router_port = 8889
            cls.zmq_port = 7500
            cls.model_path = "/mnt/network_drive/shared_models"
            cls.router_log_path = os.path.join(cls.project_root_path, "logs/router.log")
            cls.router_err_path = os.path.join(cls.project_root_path, "logs/router.err")
            cls.loadgen_process = None

            # Run router
            router_env = {
                "PD_MODE": "lmcache",
                "ROUTER_TMP_DIR_PATH": "/tmp",
                "ROUTER_HOST": cls.router_host,
                "ROUTER_PORT": f"{cls.router_port}",
                "SD_FILE": "/tmp/sd.json",
                "DOCKER": "1",
                "ZMQ_PORT": f"{cls.zmq_port}"
            }
            router_work_dir = os.path.join(cls.project_root_path, "router")
            print(f"Start executing router...")
            try:
                with open(cls.router_log_path, "w") as stdout_log, open(cls.router_err_path, "w") as stderr_log:
                    cls.router_proc = subprocess.Popen(
                        ["./router"],
                        cwd=router_work_dir,
                        env=router_env,
                        stdout=stdout_log,
                        stderr=stderr_log,
                    )
            except subprocess.CalledProcessError:
                raise RuntimeError(f"Failed to start router")
            
            time.sleep(1) 
            
            print(f"Start executing liquidlet...")
            liquidlet_env = {
                "ROUTER": f"http://{cls.router_host}:{cls.router_port}/register_node",
                "ZMQ_PORT": f"{cls.zmq_port}",
                "STORAGE_PATH": f"/models",
                "DAEMON_HOST": f"0.0.0.0",
                "DAEMON_PORT": f"{cls.liquidlet_port}",
                "PD_MODE": "lmcache"
            }
            debug_bind_str = ""
            # debug_bind_str = " ".join([
            #     f"-v {cls.project_root_path}/debug/vllm_v1_adapter.py:"
            #     "/opt/conda/lib/python3.11/site-packages/lmcache/integration/vllm/vllm_v1_adapter.py",

            #     f"-v {cls.project_root_path}/debug/cache_engine.py:"
            #     "/opt/conda/lib/python3.11/site-packages/lmcache/v1/cache_engine.py",

            #     f"-v {cls.project_root_path}/debug/nixl_connector_v3.py:"
            #     "/opt/conda/lib/python3.11/site-packages/lmcache/v1/storage_backend/connector/nixl_connector_v3.py",

            #     f"-v {cls.project_root_path}/debug/nixl_backend_v3.py:"
            #     "/opt/conda/lib/python3.11/site-packages/lmcache/v1/storage_backend/nixl_backend_v3.py"
            # ])
            # Run Docker container with overridden entrypoint
            try:
                cmd = f"""docker run --rm -d \
--name {cls.container_name} \
--network host \
-p {cls.liquidlet_port}:{cls.liquidlet_port} \
-p 23333:23333 \
-p 23334:23334 \
-v {cls.model_path}:/models \
-v /tmp:/tmp \
{debug_bind_str} \
--gpus all \
-e PD_MODE=$PD_MODE \
-e STORAGE_PATH=$STORAGE_PATH \
-e ROUTER=$ROUTER \
-e DAEMON_HOST=$DAEMON_HOST \
-e DAEMON_PORT=$DAEMON_PORT \
-e EXT_SLLM=1 \
lrq619/liquidlet:latest \
--storage-path=/models \
--mem-pool-size=20GB
"""
                subprocess.run(cmd, check=True, env=liquidlet_env, shell=True)
            except subprocess.CalledProcessError:
                raise RuntimeError("Failed to start the Docker container.")

            # Wait for liquidlet to come online
            wait_for_server(port=cls.liquidlet_port, timeout_seconds=300)
            # Register prefill model and decode model
            register_url = f"http://{cls.router_host}:{cls.router_port}/register_model"
            response = register_model(register_url, cls.prefill_register_file_path)
            response = register_model(register_url, cls.decode_register_file_path)

            router_info_url = f"http://{cls.router_host}:{cls.router_port}/get_router_info"
            model_name = f"{cls.model_name}::prefill"
            instance_count = 1
            wait_for_instances(router_info_url, model_name, instance_count)

            model_name = f"{cls.model_name}::decode"
            wait_for_instances(router_info_url, model_name, instance_count)
        except Exception as e:
            cls._cleanup()
            raise RuntimeError(f"Setup failed: {e}")


    @classmethod
    def tearDownClass(cls):
        cls._cleanup()

    def test_stress(self):
        llm_loadgen_path = os.path.join(self.project_root_path, "../LLMLoadgen")
        dataset_path = "/path/to/data/traces/conv/sample_0_all_input1_output1_no_prefix.json"
        sampling_rate = 15
        loadgen_result_path = os.path.join(self.project_root_path, "logs/loadgen_result.json")
        loadgen_log_path = os.path.join(self.project_root_path, "logs/loadgen.log")
        loadgen_err_path = os.path.join(self.project_root_path, "logs/loadgen.err")
        log_file = open(loadgen_log_path, "w")
        err_file = open(loadgen_err_path, "w")

        try:
            self.loadgen_process = subprocess.Popen(
                ["./LLMLoadgen",
                "-dataset_path", dataset_path,
                "-sampling_rate", str(sampling_rate),
                "-url", f"http://{self.router_host}:{self.router_port}/v1/completions",
                "-result_path", loadgen_result_path],
                cwd=llm_loadgen_path,
                stdout=log_file,
                stderr=err_file
            )

            time.sleep(360)  # Let the process run for 10 minutes

            self.loadgen_process.terminate()
            try:
                self.loadgen_process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.loadgen_process.kill()

        finally:
            log_file.close()
            err_file.close()
        total, valid, error = analyze_trace(loadgen_result_path)
        valid_ratio = valid / total
        min_threshold = 0.9
        self.assertGreater(valid_ratio, min_threshold, f"valid ratio must be > {min_threshold}, however, we've got {valid} valid out of {total} requests")



        


if __name__ == "__main__":
    unittest.main()