import requests
import time

BASE_URL = "http://localhost:8080"

UUID = "test-abc123"

def create_instance(uuid, port, device):
    payload = {
        "uuid": uuid,
        "cli": "vllm",
        "args": [
            "serve", "corag/CoRAG-Llama3.1-8B-MultihopQA",
            "--gpu-memory-utilization", "0.8",
            "--enforce-eager",
            "--max-model-len", "120000",
            "--port", str(port)
        ],
        "env": [
            f"CUDA_VISIBLE_DEVICES={device}",
            f"VLLM_WORKER_MULTIPROC_METHOD=fork"
        ]
    }

    response = requests.post(f"{BASE_URL}/create", json=payload)
    print("[CREATE]", response.status_code, response.json())

def delete_instance(uuid):
    payload = {
        "uuid": uuid
    }

    response = requests.post(f"{BASE_URL}/delete", json=payload)
    print("[DELETE]", response.status_code, response.json())

def main():
    uuid0 = "test-0"
    uuid1 = "test-1"
    print("Creating vLLM instance...")
    create_instance(uuid0, 8000, 0)
    create_instance(uuid1, 8001, 1)

    print("Waiting vllm")
    time.sleep(50)
    
    try:
        print("Sending test request to vLLM instance...")
        response = requests.post(
            "http://localhost:8000/v1/chat/completions",
            headers={"Content-Type": "application/json"},
            json={
                "model": "corag/CoRAG-Llama3.1-8B-MultihopQA",
                "messages": [{"role": "user", "content": "Hello, what can you do?"}],
                "max_tokens": 10
            },
            timeout=5
        )
        print("Response status:", response.status_code)
        print("Response content:", response.json())
    except Exception as e:
        print("Failed to query vLLM:", e)
        
    try:
        print("Sending test request to vLLM instance...")
        response = requests.post(
            "http://localhost:8001/v1/chat/completions",
            headers={"Content-Type": "application/json"},
            json={
                "model": "corag/CoRAG-Llama3.1-8B-MultihopQA",
                "messages": [{"role": "user", "content": "Hello, what can you do?"}],
                "max_tokens": 10
            },
            timeout=5
        )
        print("Response status:", response.status_code)
        print("Response content:", response.json())
    except Exception as e:
        print("Failed to query vLLM:", e)


    print("Deleting vLLM instance...")
    delete_instance(uuid0)
    print("Deleting vLLM instance...")
    delete_instance(uuid1)

if __name__ == "__main__":
    main()
