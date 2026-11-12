
from tests.utils import wait_for_server, get_project_root_path, register_model, wait_for_instances
from tests.preload import preload
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
            project_root_path = get_project_root_path()
            cls.model_name = "meta-llama/Llama-3.1-8B"
            cls.container_name = "liquidlet_aggregation_test"
            cls.register_file_path = os.path.join(project_root_path, "tests/aggregation/Llama-3.1-8B_v1.json")
            cls.liquidlet_port = 9000
            cls.router_port = 8889
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
                "DISABLE_AUTOSCALER": "1",
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

            # Wait for liquidlet to come online
            wait_for_server(port=cls.liquidlet_port, timeout_seconds=300)
            model_path = "/models/meta-llama/Llama-3.1-8B"
            print(f"Preloading model: {model_path}")
            preload_url = f"http://localhost:{cls.liquidlet_port}/preload"
            preload(preload_url, model_path)
            print(f"Preload finished!")
            # Register model
            register_url = f"http://localhost:{cls.router_port}/register_model"
            response = register_model(register_url, cls.register_file_path)
        except Exception as e:
            cls._cleanup()
            raise RuntimeError(f"Setup failed: {e}")

    def send_create_request(self):
        url = f"http://localhost:{self.router_port}/create_instance_test"
        params = {"model": "meta-llama/Llama-3.1-8B"}
        headers = {"Content-Type": "application/json"}
        request_uuid = str(uuid.uuid4())
        data = {
            "instance_uuid": request_uuid,
            "gpu_num_per_node": 1,
            "node_num": 1
        }

        response = requests.post(url, params=params, json=data, headers=headers)
        return request_uuid

    def send_delete_request(self, model_name, request_uuid):
        params = {
            "instance": request_uuid,
            "model_name": model_name
            }

        delete_url = f"http://localhost:{self.router_port}/delete_instance_test"

        try:
            response = requests.post(delete_url,params=params)
            response.raise_for_status()
            print(f"Successfully deleted instance with UUID: {request_uuid}")
            return request_uuid
        except requests.RequestException as e:
            print(f"Failed to send delete request: {e}")
            return None

    def send_set_instance_needed_request(self, model_name, instance_needed):
        params = {
            "model": model_name,
            "instance_needed": instance_needed,
        }
        set_url = f"http://localhost:{self.router_port}/set_instance_needed_test"
        try:
            response = requests.post(set_url,params=params)
            response.raise_for_status()
            print(f"Successfully set model {model_name} instance needed to {instance_needed}")
            return response
        except requests.RequestException as e:
            print(f"Failed to send set_instance needed request: {e}")
            return None
        

    def send_serve_request(self, url, headers, payload):
        response = requests.post(url, headers=headers, data=json.dumps(payload), timeout=120)
        return response


    def test_create_then_delete(self):
        resposne = self.send_set_instance_needed_request(self.model_name, 2)
        router_info_url = f"http://localhost:{self.router_port}/get_router_info"
        wait_for_instances(router_info_url, self.model_name, 2)
        # server_port = 23333
        # wait_for_server(server_port)
        url = f"http://localhost:{self.router_port}/v1/completions"
        headers = {"Content-Type": "application/json"}
        output_token_len = 10
        payload = {
            "model": "meta-llama/Llama-3.1-8B",
            "prompt": "San Francisco is a",
            "max_tokens": output_token_len,
            "min_tokens": output_token_len,
            "temperature": 0
        }
        try:
        # Start asynchronous POST request
            responses = []
            submitted_request_num = 10
            with concurrent.futures.ThreadPoolExecutor(max_workers=10) as executor:
                # Submit 100 tasks
                futures = [
                    executor.submit(self.send_serve_request, url, headers, payload)
                    for _ in range(submitted_request_num)
                ]

                self.send_set_instance_needed_request(model_name=decode_model_name, instance_needed=0)

                # Collect all results
                for future in concurrent.futures.as_completed(futures):
                    try:
                        response = future.result()
                        responses.append(response)
                    except Exception as e:
                        print(f"Request failed: {e}")

            for response in responses:
                self.assertEqual(response.status_code, 200, f"Expected 200 OK but got {response.status_code}, response body: {response.text}")
                response_json = response.json()
                # Check that "metrics_list" exists and is not None
                self.assertIn("metrics_list", response_json, "Response JSON missing 'metrics_list' key")
                self.assertIsNotNone(response_json["metrics_list"], "'metrics_list' is None")

        except requests.exceptions.Timeout:
            self.fail("Request timed out after 30 seconds")