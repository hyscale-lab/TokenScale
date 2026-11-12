from tests.utils import wait_for_server, get_project_root_path, generate_prompt
import os

import subprocess
import unittest
import requests
import json
import time

class TestLiquidletContainer(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.project_root_path = get_project_root_path()
        cls.container_name = "vllm_lmcache_test"
        test_script_path = os.path.join(cls.project_root_path, "tests/lmcache/vllm_test_entrypoint_xpyd.sh")
        cls.custom_script =   test_script_path
        cls.port = 8000
        cls.model_path = "/mnt/network_drive/shared_models"
        cls.prefiller_config_path = os.path.join(cls.project_root_path, "tests/lmcache/lmcache_prefill_xpyd.yaml")
        cls.decoder_config_path = os.path.join(cls.project_root_path, "tests/lmcache/lmcache_decode_xpyd.yaml")
        cls.proxy_server_path = os.path.join(cls.project_root_path, "pd/lmcache/lmcache_proxy_server.py")
        cls.loadgen_process = None
        

        # Run Docker container with overridden entrypoint
        try:
            subprocess.run([
                "docker", "run", "--rm", "-d",
                "--name", cls.container_name,
                "-p", f"{cls.port}:{cls.port}",  # map container port to host
                "-v", f"{cls.model_path}:/models",
                "-v", f"{cls.prefiller_config_path}:/app/lmcache-prefill.yaml",
                "-v", f"{cls.decoder_config_path}:/app/lmcache-decode.yaml",
                "-v", f"{cls.custom_script}:/app/test_script.sh",
                "-v", f"{cls.proxy_server_path}:/app/lmcache_proxy_server.py",
                "--gpus", "all",
                "-e", "PD_MODE=lmcache",
                "-e", "STORAGE_PATH=/models",
                "--entrypoint", "/app/test_script.sh",
                "lrq619/liquidlet:latest",
                "--storage-path=/models",
                "--mem-pool-size=20GB"
            ], check=True)
        except subprocess.CalledProcessError:
            raise RuntimeError("Failed to start the Docker container.")

        # Wait for server to come online
        wait_for_server(port=8000, timeout_seconds=300)

    @classmethod
    def tearDownClass(cls):
        # Stop the container if it's still running
        project_root_path = get_project_root_path()
        docker_logs_path = os.path.join(project_root_path, "logs", "docker_logs.log")
        if cls.loadgen_process != None:
            cls.loadgen_process.terminate()  # Graceful shutdown
            try:
                cls.loadgen_process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                cls.loadgen_process.kill()  # Force kill if it doesn't exit in time

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

    def test_server_response(self):
        prompt_length = 1034
        prompt = generate_prompt(prompt_length)
        url = f"http://localhost:{self.port}/v1/completions"
        headers = {"Content-Type": "application/json"}
        payload = {
            "model": "meta-llama/Llama-3.1-8B",
            "prompt": prompt,
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
        except requests.exceptions.Timeout:
            self.fail("Request timed out after 30 seconds")

        
    def test_stress(self):
        llm_loadgen_path = os.path.join(self.project_root_path, "../LLMLoadgen")
        dataset_path = "/path/to/data/traces/conv/sample_0_all_input1_output1_ALL.json"
        sampling_rate = 1
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
                "-url", f"http://localhost:{self.router_port}/v1/completions",
                "-result_path", loadgen_result_path],
                cwd=llm_loadgen_path,
                stdout=log_file,
                stderr=err_file
            )

            time.sleep(120)  # Let the process run for 2 minutes

            self.loadgen_process.terminate()
            try:
                self.loadgen_process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.loadgen_process.kill()

        finally:
            log_file.close()
            err_file.close()

if __name__ == "__main__":
    unittest.main()