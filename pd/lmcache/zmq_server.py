import argparse
import asyncio
import zmq
import zmq.asyncio
import msgspec
from fastapi import FastAPI, WebSocket, WebSocketDisconnect
from typing import Dict, Set
import uvicorn
import logging

from lmcache.v1.storage_backend.connector.nixl_connector_v3 import NixlMsg

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

zmq_ctx = zmq.asyncio.Context()
app = FastAPI()

# Stores finished req ids
finished_reqs: Set[str] = set()
finished_reqs_lock = asyncio.Lock()

# Mapping: req_id -> set of WebSocket connections waiting for it
waiters: Dict[str, Set[WebSocket]] = {}
waiters_lock = asyncio.Lock()

async def notify_waiters(req_id: str):
    async with waiters_lock:
        clients = waiters.pop(req_id, set())
    for ws in clients:
        try:
            await ws.send_text(req_id)
        except Exception as e:
            logger.warning(f"Failed to notify websocket client: {e}")

@app.websocket("/ws")
async def websocket_endpoint(ws: WebSocket):
    await ws.accept()
    try:
        while True:
            req_id = await ws.receive_text()
            async with finished_reqs_lock:
                if req_id in finished_reqs:
                    # Already finished, notify immediately
                    await ws.send_text(req_id)
                    continue

            # Not finished yet, register websocket as waiter
            async with waiters_lock:
                if req_id not in waiters:
                    waiters[req_id] = set()
                waiters[req_id].add(ws)
            # Now just wait until notified (notification sends message)
            # We rely on notify_waiters to send the notification and remove the waiter
    except WebSocketDisconnect:
        # Clean up websocket from waiters on disconnect
        async with waiters_lock:
            for wset in waiters.values():
                wset.discard(ws)

async def zmq_pull_server(bind_url: str):
    socket = zmq_ctx.socket(zmq.PULL)
    socket.bind(bind_url)
    logger.info(f"ZMQ proxy server started on {bind_url}")

    while True:
        try:
            msg_bytes = await socket.recv()
            msg = msgspec.msgpack.decode(msg_bytes, type=NixlMsg)
            req_id = msg.req_id
            async with finished_reqs_lock:
                finished_reqs.add(req_id)
            await notify_waiters(req_id)
            logger.info(f"Prefill of req {req_id} done.")
        except zmq.Again:
            await asyncio.sleep(0.01)
        except Exception as e:
            logger.error(f"ZMQ Error: {e}")
            break

    socket.close()
    logger.info("ZMQ PULL server stopped.")

async def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--zmq-host", type=str, default="127.0.0.1", help="ZMQ bind host")
    parser.add_argument("--zmq-port", type=int, default=7500, help="ZMQ bind port")
    parser.add_argument("--ws-host", type=str, default="0.0.0.0", help="HTTP/WebSocket server host")
    parser.add_argument("--ws-port", type=int, default=60000, help="HTTP/WebSocket server port")
    args = parser.parse_args()

    zmq_url = f"tcp://{args.zmq_host}:{args.zmq_port}"
    zmq_task = asyncio.create_task(zmq_pull_server(zmq_url))

    import uvicorn
    config = uvicorn.Config(app, host=args.ws_host, port=args.ws_port, log_level="info")
    server = uvicorn.Server(config)
    api_task = asyncio.create_task(server.serve())

    await asyncio.gather(zmq_task, api_task)

if __name__ == "__main__":
    asyncio.run(main())