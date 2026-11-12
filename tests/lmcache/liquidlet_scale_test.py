
from tests.utils import wait_for_server, get_project_root_path, register_model, wait_for_instances, generate_prompt
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
        if cls.loadgen_process and cls.loadgen_process.poll() is None:
            print("Terminating loadgen...")
            cls.loadgen_proc.terminate()
            try:
                cls.loadgen_proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                print("Killing router after timeout...")
                cls.loadgen_proc.kill()


    @classmethod
    def setUpClass(cls):
        try:
            cls.project_root_path = get_project_root_path()
            cls.model_name = "meta-llama/Llama-3.1-8B"
            cls.container_name = "liquidlet_lmcache_test"
            cls.prefill_register_file_path = os.path.join(cls.project_root_path, "tests/lmcache/Llama-3.1-8B_prefill_v1.json")
            cls.decode_register_file_path = os.path.join(cls.project_root_path, "tests/lmcache/Llama-3.1-8B_decode_v1.json")
            cls.router_host = "172.17.0.1"
            cls.liquidlet_port = 9000
            cls.router_port = 8889
            cls.zmq_port = 7500
            cls.model_path = "/mnt/network_drive/shared_models"
            cls.router_log_path = os.path.join(cls.project_root_path, "logs/router.log")
            cls.router_err_path = os.path.join(cls.project_root_path, "logs/router.err")
            cls.loadgen_process = None
            # debug_bind_str = " ".join([
            #     f"-v {cls.project_root_path}/debug/vllm_v1_adapter.py:"
            #     "/opt/conda/lib/python3.11/site-packages/lmcache/integration/vllm/vllm_v1_adapter.py",
            # ])
            debug_bind_str = ""

            # Run router
            router_env = {
                "PD_MODE": "lmcache",
                "ROUTER_TMP_DIR_PATH": "/tmp",
                "ROUTER_HOST": cls.router_host,
                "ROUTER_PORT": f"{cls.router_port}",
                "SD_FILE": "/tmp/sd.json",
                "DOCKER": "1",
                "DISABLE_AUTOSCALER": "1",
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
                "STORAGE_PATH": f"/models",
                "DAEMON_HOST": f"0.0.0.0",
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

            # Wait for liquidlet to come online
            wait_for_server(port=cls.liquidlet_port, timeout_seconds=300)
            model_path = "/models/meta-llama/Llama-3.1-8B"
            print(f"Preloading model: {model_path}")
            preload_url = f"http://localhost:{cls.liquidlet_port}/preload"
            preload(preload_url, model_path)
            print(f"Preload finished!")
            # Register model
            register_url = f"http://localhost:{cls.router_port}/register_model"
            response = register_model(register_url, cls.prefill_register_file_path)
            response = register_model(register_url, cls.decode_register_file_path)
        except Exception as e:
            cls._cleanup()
            raise RuntimeError(f"Setup failed: {e}")

    @classmethod
    def tearDownClass(cls):
        cls._cleanup()

    def send_create_request(self, model_name):
        url = f"http://{self.router_host}:{self.router_port}/create_instance_test"
        params = {"model": model_name}
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
            "model_name": model_name,
            }

        delete_url = f"http://{self.router_host}:{self.router_port}/delete_instance_test"

        try:
            response = requests.post(delete_url,params=params)
            response.raise_for_status()
            print(f"Successfully deleted instance with UUID: {request_uuid}")
            return request_uuid
        except requests.RequestException as e:
            print(f"Failed to send delete request: {e}")
            return None

    def send_serve_request(self, url, headers, payload):
        response = requests.post(url, headers=headers, data=json.dumps(payload), timeout=120)
        return response

    def send_set_instance_needed_request(self, model_name, instance_needed):
        params = {
            "model": model_name,
            "instance_needed": instance_needed,
        }
        set_url = f"http://{self.router_host}:{self.router_port}/set_instance_needed_test"
        try:
            response = requests.post(set_url,params=params)
            response.raise_for_status()
            print(f"Successfully set model {model_name} instance needed to {instance_needed}")
            return response
        except requests.RequestException as e:
            print(f"Failed to send set_instance needed request: {e}")
            return None

    def test_create_then_delete(self):
        prefill_model_name = f"{self.model_name}::prefill"
        decode_model_name = f"{self.model_name}::decode"
        self.send_set_instance_needed_request(model_name=prefill_model_name, instance_needed=1)
        self.send_set_instance_needed_request(model_name=decode_model_name, instance_needed=1)
        router_info_url = f"http://{self.router_host}:{self.router_port}/get_router_info"

        wait_for_instances(router_info_url, f"{self.model_name}::prefill", 1)
        wait_for_instances(router_info_url, f"{self.model_name}::decode", 1)

        url = f"http://{self.router_host}:{self.router_port}/v1/completions"
        headers = {"Content-Type": "application/json"}
        input_token_len = 1024
        output_token_len = 512
        prompt = generate_prompt(input_token_len)
        payload = {
            "model": "meta-llama/Llama-3.1-8B",
            "prompt": prompt,
            "max_tokens": output_token_len,
            "min_tokens": output_token_len,
            "temperature": 0
        }
        try:
        # Start asynchronous POST request
            responses = []
            submitted_request_num = 3
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

    def test_scale_after_stress(self):
        prefill_model_name = f"{self.model_name}::prefill"
        decode_model_name = f"{self.model_name}::decode"
        self.send_set_instance_needed_request(model_name=prefill_model_name, instance_needed=1)
        self.send_set_instance_needed_request(model_name=decode_model_name, instance_needed=1)
        router_info_url = f"http://{self.router_host}:{self.router_port}/get_router_info"

        wait_for_instances(router_info_url, f"{self.model_name}::prefill", 1)
        wait_for_instances(router_info_url, f"{self.model_name}::decode", 1)


        llm_loadgen_path = os.path.join(self.project_root_path, "../LLMLoadgen")
        dataset_path = "/path/to/data/traces/conv/sample_0_all_input1_output1_ids.json"
        sampling_rate = 20
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

            time.sleep(600)
            self.send_set_instance_needed_request(model_name=prefill_model_name, instance_needed=0)
            self.send_set_instance_needed_request(model_name=decode_model_name, instance_needed=0)
            wait_for_instances(router_info_url, f"{self.model_name}::prefill", 0)
            wait_for_instances(router_info_url, f"{self.model_name}::decode", 0)


            self.loadgen_process.terminate()
            try:
                self.loadgen_process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.loadgen_process.kill()
            
        except requests.exceptions.Timeout:
            self.fail("Error")
