from tests.utils import wait_for_server, get_project_root_path, register_model, wait_for_instances, generate_prompt, wait_for_nodes
import os
import pytest
import time

import subprocess
import unittest
import requests
import json
import uuid
import concurrent.futures

class TestLiquidletContainer(unittest.TestCase):
    router_proc = None
    @classmethod
    def _cleanup(cls):
        # Stop the container if it's still running
        project_root_path = get_project_root_path()
        docker_logs_path = os.path.join(project_root_path, "logs", "docker_logs.log")

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
            cls.router_host = "192.0.2.1"
            cls.master_host = cls.router_host
            cls.worker_host = "192.0.2.2"
            cls.liquidlet_port = 9000
            cls.router_port = 8889
            cls.zmq_port = 7500
            cls.model_path = "/mnt/B200-dataset/shared_models"
            cls.router_log_path = os.path.join(cls.project_root_path, "logs/router.log")
            cls.router_err_path = os.path.join(cls.project_root_path, "logs/router.err")
            cls.loadgen_process = None
            debug_bind_str = f"-v {cls.project_root_path}/cache_engine.py:/opt/conda/lib/python3.11/site-packages/lmcache/v1/cache_engine.py"

            
            print(f"Start executing liquidlet...")
            liquidlet_env = {
                "ROUTER": f"http://{cls.router_host}:{cls.router_port}/register_node",
                "STORAGE_PATH": f"/models",
                "DAEMON_HOST": cls.worker_host,
                "DAEMON_PORT": f"{cls.liquidlet_port}",
                "PD_MODE": "lmcache"
            }
            # Run Docker container with overridden entrypoint
            try:
                cmd = f"""docker run --rm -d \
--name {cls.container_name} \
-p {cls.liquidlet_port}:{cls.liquidlet_port} \
-p 23333:23333 \
-p 23334:23334 \
-p 7300:7300 \
-p 7301:7301 \
-p 7400:7400 \
-p 7401:7401 \
-v {cls.model_path}:/models \
-v /tmp:/tmp \
{debug_bind_str} \
--gpus all \
-e ROUTER=$ROUTER \
-e PD_MODE=$PD_MODE \
-e STORAGE_PATH=$STORAGE_PATH \
-e DAEMON_HOST=$DAEMON_HOST \
-e DAEMON_PORT=$DAEMON_PORT \
lrq619/liquidlet:latest \
--storage-path=/models \
--mem-pool-size=20GB
"""
                subprocess.run(cmd, check=True, env=liquidlet_env, shell=True)
            except subprocess.CalledProcessError:
                raise RuntimeError("Failed to start the Docker container.")

            # Wait for two nodes connected
            router_info_url = f"http://{cls.router_host}:{cls.router_port}/get_router_info"
            wait_for_nodes(router_info_url, 2)
            
        except Exception as e:
            cls._cleanup()
            raise RuntimeError(f"Setup failed: {e}")

    @classmethod
    def tearDownClass(cls):
        cls._cleanup()

    def test_multi_node_worker(self):
        router_info_url = f"http://{self.router_host}:{self.router_port}/get_router_info"
        while True:
            try:
                response = requests.post(router_info_url)
                if not response.ok:
                    print(f"Error response received: {response.status_code}")
                    return
                print(f"Received successful response: {response.status_code}")
            except requests.RequestException as e:
                print(f"Request failed: {e}")
                return
            time.sleep(1)