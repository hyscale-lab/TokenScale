import argparse
import json
import time
import requests
import sys
import logging

logger = logging.getLogger(__name__)

def wait_for_model_registered(router_info_url: str, model_name: str):
    while True:
        try:
            response = requests.post(router_info_url, json={}, timeout=5)
            response.raise_for_status()
            data = response.json()

            scaler_info_map = data.get("scaler_info_map", {})
            if model_name in scaler_info_map:
                logger.info(f"model {model_name} is registered")
                return
        except KeyboardInterrupt:
            raise
        except Exception as e:
            logger.warning(f"Error fetching instance info: {e}")

        time.sleep(1)


def wait_for_instances(router_info_url: str, model_name: str, instance_count: int, timeout: int = 300, interval: float = 1.0):
    """
    Polls router_info_url until the given model_name has instance_counter == instance_count.
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
                logger.info(f"Current instance count for {model_name}: {current_count}")
                
                if current_count == instance_count:
                    logger.info(f"Instance count requirement met: {current_count} == {instance_count}")
                    return
        except KeyboardInterrupt:
            raise
        except Exception as e:
            logger.warning(f"[WARN] Error fetching instance info: {e}")

        if time.time() - start_time > timeout:
            raise TimeoutError(f"Timed out waiting for {model_name} to reach {instance_count} instances.")

        time.sleep(interval)

def set_instance_needed(set_url: str, model_name: str, instance_needed: int):
    params = {
        "model": model_name,
        "instance_needed": instance_needed,
    }
    try:
        response = requests.post(set_url, params=params)
        response.raise_for_status()
        logger.info(f"[INFO] Successfully set model {model_name} instance needed to {instance_needed}")
        return response
    except requests.RequestException as e:
        logger.fatal(f"[ERROR] Failed to send set_instance_needed request: {e}")
        return None

def main():
    parser = argparse.ArgumentParser(description="Scale models according to trace file.")
    parser.add_argument("--router_host", default="localhost", help="Router host address")
    parser.add_argument("--router_port", type=int, default=8889, help="Router port")
    parser.add_argument("--scaler_trace_file", help="Path to scaler trace JSON file")
    
    args = parser.parse_args()

    # Construct URLs
    router_info_url = f"http://{args.router_host}:{args.router_port}/get_router_info"
    set_url = f"http://{args.router_host}:{args.router_port}/set_instance_needed_test"



    # Load scaler trace file
    try:
        with open(args.scaler_trace_file, "r") as f:
            events = json.load(f)
        if not isinstance(events, list):
            logger.fatal("scaler_trace_file must be a JSON list.")
            sys.exit(1)
    except Exception as e:
        logger.fatal(f"Failed to read scaler_trace_file: {e}")
        sys.exit(1)
    # Get all models, wait for their registration
    model_name_set = set()
    for i, event in enumerate(events, start=1):
        model_name = event["model_name"]
        model_name_set.add(model_name)

    for model_name in model_name_set:
        logger.info(f"Waiting for model {model_name} registered")
        wait_for_model_registered(router_info_url, model_name)

    # Process events sequentially
    for i, event in enumerate(events, start=1):
        try:
            model_name = event["model_name"]
            instance_needed = event["instance_needed"]
        except KeyError as e:
            logger.fatal(f"Missing key in event {i}: {e}")
            continue

        logger.info(f"\n=== Event {i}: Scaling {model_name} to {instance_needed} instances ===")

        # Send scaling instruction
        set_instance_needed(set_url, model_name, instance_needed)

        # Wait until scaling reaches desired count
        try:
            wait_for_instances(router_info_url, model_name, instance_needed)
        except TimeoutError as e:
            logger.warning(f"{e}")
            continue

    logger.info("\nAll scaling events processed successfully.")

if __name__ == "__main__":
    main()