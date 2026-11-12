
from tests.utils import wait_for_server, get_project_root_path, register_model, wait_for_instances
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
            project_root_path = get_project_root_path()
            cls.model_name = "meta-llama/Llama-3.1-8B"
            cls.container_name = "liquidlet_aggregation_test"
            cls.register_file_path = os.path.join(project_root_path, "tests/aggregation/Llama-3.1-8B.json")
            cls.liquidlet_port = 9000
            cls.router_port = 8889
            cls.router_host = "0.0.0.0"
            cls.model_path = "/mnt/network_drive/shared_models"
            cls.router_log_path = os.path.join(project_root_path, "logs/router.log")
            cls.router_err_path = os.path.join(project_root_path, "logs/router.err")

            # Run router
            router_env = {
                "PD_MODE": "",
                "ROUTER_TMP_DIR_PATH": "/tmp",
                "ROUTER_HOST": "0.0.0.0",
                "ROUTER_PORT": f"{cls.router_port}",
                "SD_FILE": "/tmp/sd.json",
                "DOCKER": "1",
            }
            router_work_dir = os.path.join(project_root_path, "router")
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
                "ROUTER": f"http://172.17.0.1:{cls.router_port}/register_node",
                "STORAGE_PATH": f"/models",
                "DAEMON_HOST": f"0.0.0.0",
                "DAEMON_PORT": f"{cls.liquidlet_port}",
                "PD_MODE": ""
            }
            # Run Docker container with overridden entrypoint
            try:
                cmd = f"""docker run --rm -d \
--name {cls.container_name} \
-p {cls.liquidlet_port}:{cls.liquidlet_port} \
-p 23333:23333 \
-p 23334:23334 \
-v {cls.model_path}:/models \
-v /tmp:/tmp \
--gpus '"device=1"' \
-e PD_MODE=$PD_MODE \
-e STORAGE_PATH=$STORAGE_PATH \
-e ROUTER=$ROUTER \
-e DAEMON_HOST=$DAEMON_HOST \
-e DAEMON_PORT=$DAEMON_PORT \
lrq619/liquidlet:latest \
--storage-path=/models \
--mem-pool-size=20GB
"""
                subprocess.run(cmd, check=True, env=liquidlet_env, shell=True)
            except subprocess.CalledProcessError:
                raise RuntimeError("Failed to start the Docker container.")

            # Wait for liquidlet to come online
            wait_for_server(port=cls.liquidlet_port, timeout_seconds=300)
            # preload the model
            model_path = "/models/meta-llama/Llama-3.1-8B"
            print(f"Preloading model: {model_path}")
            preload_url = f"http://{cls.router_host}:{cls.liquidlet_port}/preload"
            preload(preload_url, model_path)
            print(f"Preload finished!")
            # Register prefill model and decode model
            register_url = f"http://localhost:{cls.router_port}/register_model"
            response = register_model(register_url, cls.register_file_path)

            router_info_url = f"http://localhost:{cls.router_port}/get_router_info"
            instance_count = 1
            wait_for_instances(router_info_url, cls.model_name, instance_count)

        except Exception as e:
            cls._cleanup()
            raise RuntimeError(f"Setup failed: {e}")


    @classmethod
    def tearDownClass(cls):
        cls._cleanup()


    def test_server_response(self):
        url = f"http://localhost:{self.router_port}/v1/completions"
        headers = {"Content-Type": "application/json"}
        payload = {
            "model": "meta-llama/Llama-3.1-8B",
            "prompt": "San Francisco is a",
            "max_tokens": 10,
            "min_tokens": 10,
            "temperature": 0
        }
        try:
            response = requests.post(url, headers=headers, data=json.dumps(payload), timeout=30)
            self.assertEqual(response.status_code, 200, f"Expected 200 OK but got {response.status_code}")
            response_json = response.json()
            print("Response JSON:", response_json)
            # Check that "metrics_list" exists and is not None
            self.assertIn("metrics_list", response_json, "Response JSON missing 'metrics_list' key")
            self.assertIsNotNone(response_json["metrics_list"], "'metrics_list' is None")

            metrics = response_json['metrics_list'][0]

            ttft = metrics['first_token_time'] - metrics['arrival_time']
            # transfer latency is equal to decoder's ttft
            transfer_latency = 0
            decode_latency = metrics['finished_time'] - metrics['first_token_time']
            tpot = decode_latency / response_json['usage']['completion_tokens']
            e2e = metrics['finished_time'] - metrics['arrival_time']
            print(f"--------------------Performance Benchmark--------------------")
            print(f"E2E latency: {e2e*1000:.1f} ms")
            print(f"TTFT: {ttft*1000:.1f} ms (prefiller metrics)")
            print(f"Transfer latency: {transfer_latency*1000:.1f} ms")
            print(f"Decode latency: {decode_latency*1000:.1f} ms, TPOT: {tpot*1000:.1f} ms/tok")
            print(f"-------------------------------------------------------------")

        except requests.exceptions.Timeout:
            self.fail("Request timed out after 30 seconds")


if __name__ == "__main__":
    unittest.main()