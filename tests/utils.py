import time
import requests
from pathlib import Path
import json

def wait_for_server(port: int, timeout_seconds: int = 1200) -> bool:
    url = f"http://localhost:{port}/v1/completions"
    print(f"Waiting for server on port {port}...")

    start_time = time.time()

    while True:
        try:
            response = requests.get(url, timeout=1)
            # If the server responds at all, it's considered "up"
            print(f"Server on {url} is ready!")
            return True
        except KeyboardInterrupt as e:
            raise e
        except requests.RequestException:
            pass  # Server not ready or port not open yet

        if time.time() - start_time >= timeout_seconds:
            print(f"Timeout waiting for server on {url}")
            return False

        time.sleep(1)

def wait_for_instances(router_info_url: str, model_name: str, instance_count: int, timeout: int = 300, interval: float = 1.0):
    """
    Polls router_info_url until the given model_name has instance_counter > instance_count.
    """
    start_time = time.time()
    
    while True:
        try:
            response = requests.post(router_info_url, json={}, timeout=5)
            response.raise_for_status()
            data = response.json()

            scaler_info_map = data.get("scaler_info_map", {})
            if model_name in scaler_info_map:
                scaler_info = scaler_info_map[model_name]
                current_count = scaler_info.get("instance_counter", 0)
                # print(f"Current instance count for {model_name}: {current_count}")
                
                if current_count == instance_count:
                    print(f"Instance count requirement met: {current_count} == {instance_count}")
                    return
        except KeyboardInterrupt as e:
            raise e
        except Exception as e:
            print(f"Error fetching instance info: {e}")

        if time.time() - start_time > timeout:
            raise TimeoutError(f"Timed out waiting for {model_name} to exceed {instance_count} instances.")

        time.sleep(interval)

def wait_for_nodes(router_info_url: str, node_count: int, timeout: int=300, interval: float = 1.0):
    start_time = time.time()
    
    while True:
        try:
            response = requests.post(router_info_url, json={}, timeout=5)
            response.raise_for_status()
            data = response.json()

            registered_node_number = data.get("registered_node_number", 0)
            if registered_node_number >= node_count:
                print(f"Node count requirement met: {registered_node_number} >= {node_count}")
                return
        except KeyboardInterrupt as e:
            raise e
        except Exception as e:
            print(f"Error fetching instance info: {e}")

        if time.time() - start_time > timeout:
            raise TimeoutError(f"Timed out waiting for node to exceed {node_count} nodes.")

        time.sleep(interval)


def register_model(register_url: str, register_file_path: str):
    # Step 1: Assert the file is a JSON file
    assert register_file_path.endswith(".json"), "register_file_path must be a .json file"

    # Step 2: Read the JSON contents
    with open(register_file_path, "r") as f:
        payload = json.load(f)

    # Step 3: Send POST request to the register URL
    headers = {"Content-Type": "application/json"}
    response = requests.post(register_url, json=payload, headers=headers)

    # Optional: raise an exception if the request failed
    response.raise_for_status()

    return response


def get_project_root_path() -> str:
    """
    Walks up from the current file's location until it finds a directory
    containing 'Dockerfile.liquidlet'. Returns that directory's absolute path.
    Raises RuntimeError if not found.
    """
    current_dir = Path(__file__).resolve().parent

    for parent in [current_dir, *current_dir.parents]:
        if (parent / "Dockerfile.liquidlet").is_file():
            return str(parent)

    raise RuntimeError("Could not find project root containing 'Dockerfile.liquidlet'")

def format_performance_metrics(response_json, request_id):
    router_metrics = response_json['router_timestamps']
    prefill_metrics = response_json['prefill_metrics_list'][0]
    decode_metrics = response_json['metrics_list'][0]

    ttft_router = router_metrics['prefillFinished'] - router_metrics['request_arrival']
    ttft_prefiller = prefill_metrics['first_token_time'] - prefill_metrics['arrival_time']
    decode_ttft = decode_metrics['first_token_time'] - decode_metrics['arrival_time']
    prefill_kv_ready_latency = 0
    if "prefill_kv_ready" in router_metrics:
        prefill_kv_ready_latency = router_metrics['prefill_kv_ready'] - router_metrics['prefillFinished']
    decode_latency = decode_metrics['finished_time'] - decode_metrics['first_token_time']
    transfer_latency = prefill_kv_ready_latency + decode_ttft
    tpot = decode_latency / response_json['usage']['completion_tokens']
    e2e = router_metrics['decodeFinished'] - router_metrics['request_arrival']

    return (
        f"--------------------Performance Benchmark--------------------\n"
        f"Request ID: {request_id}\n"
        f"E2E latency: {e2e*1000:.1f} ms\n"
        f"TTFT: {ttft_router*1000:.1f} ms (router metrics), {ttft_prefiller*1000:.1f} ms (prefiller metrics)\n"
        f"Transfer latency: {transfer_latency*1000:.1f} ms, prefill kv ready latency: {prefill_kv_ready_latency*1000:.1f} ms, decode ttft: {decode_ttft*1000:.1f} ms\n"
        f"Decode latency: {decode_latency*1000:.1f} ms, TPOT: {tpot*1000:.1f} ms/tok\n"
        f"-------------------------------------------------------------"
    )

def generate_prompt(prompt_length):
    return " ".join(["happy"] * prompt_length) + " "

def analyze_trace(file_path):
    total = 0
    valid_responses = 0
    error_responses = 0

    with open(file_path, "r") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue  # skip empty lines
            total += 1
            try:
                item = json.loads(line)
                response = item.get("response", {}).get("response")
                error = item.get("error", "")
                # error = item.get("error", "")
                if error:
                    error_responses += 1
                else:
                    if 'choices' not in response:
                        error_responses += 1
                    else:
                        valid_responses += 1
            except json.JSONDecodeError:
                error_responses += 1

    print(f"Total requests: {total}")
    print(f"Valid responses: {valid_responses}")
    print(f"Errored responses: {error_responses}")
    return total, valid_responses, error_responses