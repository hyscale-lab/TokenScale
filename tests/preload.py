import requests

def preload(url, model_path):
    payload = {"model_path": model_path}
    try:
        response = requests.post(url, json=payload)
        response.raise_for_status()  # Raise an error for non-2xx responses
        return response.json()  # Or response.text if you expect plain text
    except requests.RequestException as e:
        print(f"Failed to preload model: {e}")
        return None