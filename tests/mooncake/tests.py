from tests.utils import wait_for_server, get_project_root_path
import os

import subprocess
import unittest
import requests
import json

class TestLiquidletContainer(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        project_root_path = get_project_root_path()
        cls.container_name = "liquidlet_mooncake_test"
        test_script_path = os.path.join(project_root_path, "tests/mooncake/vllm_test_entrypoint.sh")
        cls.custom_script =   test_script_path
        cls.port = 8000
        cls.model_path = "/mnt/network_drive/shared_models"
        cls.prefiller_config_path = os.path.join(project_root_path, "tests/mooncake/mooncake_prefill.json")
        cls.decoder_config_path = os.path.join(project_root_path, "tests/mooncake/mooncake_decode.json")
        

        # Run Docker container with overridden entrypoint
        try:
            subprocess.run([
                "docker", "run", "--rm", "-d",
                "--name", cls.container_name,
                "-p", f"{cls.port}:{cls.port}",  # map container port to host
                "-v", f"{cls.model_path}:/models",
                "-v", f"{cls.prefiller_config_path}:/app/mooncake_prefill.json",
                "-v", f"{cls.decoder_config_path}:/app/mooncake_decode.json",
                "-v", f"{cls.custom_script}:/app/test_script.sh",
                "--gpus", "all",
                "-e", "PD_MODE=mooncake",
                "-e", "STORAGE_PATH=/models",
                "-e", "MOONCAKE_MASTER_ARGS=--port 50001 -eviction-high-watermark-ratio 0.9 -eviction-ratio 0.3 -enable-gc True",
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
        url = f"http://localhost:{self.port}/v1/completions"
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
            print("Response JSON:", response.json())
        except requests.exceptions.Timeout:
            self.fail("Request timed out after 30 seconds")

if __name__ == "__main__":
    unittest.main()