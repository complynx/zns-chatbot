"""Owned QA Responses fixture. Text counts use four canonical JSON bytes/unit.

Image payloads are opaque and cost 1024 synthetic units each, independent of bytes.

This is neither a real model nor its tokenizer. Never use for production, model
quality, billing, real context-limit, vision, or native-compaction acceptance.
Manual request/response files keep the existing Loom mock-provider wire shape.
"""
import argparse
import hmac
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import threading
import time
import uuid

LIMIT = 2 * 1024 * 1024
SYNTHETIC_IMAGE_TOKENS = 1024

def encoded(value):
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()

def synthetic_count(body):
    if not isinstance(body.get("input"), list):
        return max(1, (len(encoded(body)) + 3) // 4)
    # Normalize only protocol image parts, never strings or tool arguments.
    counted = dict(body)
    images = 0
    inputs = []
    for item in body.get("input", []):
        if isinstance(item, dict) and isinstance(item.get("content"), list):
            item = dict(item)
            content = []
            for part in item["content"]:
                if (isinstance(part, dict) and part.get("type") == "input_image"
                        and isinstance(part.get("image_url"), str)):
                    part = dict(part, image_url="<synthetic image payload>")
                    images += 1
                content.append(part)
            item["content"] = content
        inputs.append(item)
    counted["input"] = inputs
    return max(1, (len(encoded(counted)) + 3) // 4) + images * SYNTHETIC_IMAGE_TOKENS

def wire_response(value, max_output_tokens):
    if not isinstance(value, dict) or not value.get("ID") or value.get("ErrorCode"):
        raise ValueError("invalid manual response")
    usage = value.get("Usage")
    keys = ("InputTokens", "OutputTokens", "CachedInputTokens", "CacheWriteTokens")
    if not isinstance(usage, dict) or any(type(usage.get(k)) is not int or usage[k] < 0 for k in keys):
        raise ValueError("invalid synthetic usage")
    if usage.get("CachedInputTokens", 0) + usage.get("CacheWriteTokens", 0) > usage.get("InputTokens", 0):
        raise ValueError("invalid cached usage")
    output = []
    for call in value.get("Calls") or []:
        if not call.get("ID") or not call.get("Name") or "Arguments" not in call:
            raise ValueError("invalid manual call")
        output.append({"type": "function_call", "call_id": call["ID"], "name": call["Name"], "arguments": encoded(call["Arguments"]).decode()})
    text = value.get("Text", "")
    if text:
        kind = "refusal" if value.get("Refused") else "output_text"
        output.append({"type": "message", "role": "assistant", "content": [{"type": kind, "refusal" if kind == "refusal" else "text": text}]})
    return {"id": value["ID"], "status": "incomplete" if usage["OutputTokens"] > max_output_tokens else "completed", "output": output,
            "usage": {"input_tokens": usage.get("InputTokens", 0), "output_tokens": usage.get("OutputTokens", 0),
                      "input_tokens_details": {"cached_tokens": usage.get("CachedInputTokens", 0), "cache_write_tokens": usage.get("CacheWriteTokens", 0)}}}

class FixtureServer(ThreadingHTTPServer):
    daemon_threads = True
    def __init__(self, address, mailbox, key, wait_seconds=120, io_timeout_seconds=5):
        self.mailbox = Path(mailbox).resolve()
        if not self.mailbox.is_dir() or not key or wait_seconds <= 0 or io_timeout_seconds <= 0:
            raise ValueError("existing private mailbox, key and positive wait required")
        self.key = key
        self.wait_seconds = wait_seconds
        self.io_timeout_seconds = io_timeout_seconds
        self.publish_lock = threading.Lock()
        self.slots = threading.BoundedSemaphore(4)
        super().__init__(address, Handler)

    def get_request(self):
        connection, address = super().get_request()
        connection.settimeout(self.io_timeout_seconds)
        return connection, address

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass  # Request headers and user context must not reach process logs.

    def reply(self, status, value):
        raw = encoded(value)
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(raw)

    def read_body(self, size):
        end = time.monotonic() + self.server.io_timeout_seconds
        raw = bytearray()
        while len(raw) < size:
            remaining = end - time.monotonic()
            if remaining <= 0:
                raise TimeoutError("body deadline")
            self.connection.settimeout(remaining)
            chunk = self.rfile.read1(min(65536, size - len(raw)))
            if not chunk:
                raise ValueError("incomplete request body")
            raw.extend(chunk)
        self.connection.settimeout(self.server.io_timeout_seconds)
        return bytes(raw)

    def do_POST(self):
        if not hmac.compare_digest(self.headers.get("Authorization", "").encode(), ("Bearer " + self.server.key).encode()):
            self.reply(401, {"error": "unauthorized"})
            return
        if self.path not in ("/v1/responses", "/v1/responses/input_tokens"):
            self.reply(404, {"error": "unsupported fixture endpoint"})
            return
        if not self.server.slots.acquire(blocking=False):
            self.reply(503, {"error": "fixture busy"})
            return
        try:
            size = int(self.headers.get("Content-Length", "0"))
            if not 0 < size <= LIMIT or self.headers.get("Transfer-Encoding"):
                self.reply(413, {"error": "bounded content length required"})
                return
            body = json.loads(self.read_body(size))
            if not isinstance(body, dict) or not isinstance(body.get("input"), list) or not body.get("model"):
                raise ValueError("model and input required")
            if self.path.endswith("/input_tokens"):
                self.reply(200, {"object": "response.input_tokens", "input_tokens": synthetic_count(body)})
                return
            client = str(uuid.UUID(self.headers.get("X-Client-Request-Id", "")))
            if type(body.get("max_output_tokens")) is not int or body["max_output_tokens"] <= 0 or not body["input"]:
                raise ValueError("model request requires output bound and input")
            request = {"Provider": "openai", "Model": body["model"], "Effort": body.get("reasoning", {}).get("effort", ""),
                       "Instructions": body.get("instructions", ""), "ClientRequestID": client, "Input": body["input"],
                       "Tools": body.get("tools", []), "MaxOutputTokens": body["max_output_tokens"]}
            path = self.server.mailbox / (client + ".request.json")
            with self.server.publish_lock:
                if path.exists():
                    if path.stat().st_size > LIMIT or json.loads(path.read_bytes()) != request:
                        self.reply(409, {"error": "client request differs"})
                        return
                else:
                    temporary = path.with_name(client + "." + uuid.uuid4().hex + ".tmp")
                    temporary.write_bytes(encoded(request))
                    temporary.replace(path)
            response = path.with_name(client + ".response.json")
            end = time.monotonic() + self.server.wait_seconds
            while time.monotonic() < end:
                if response.exists():
                    if response.stat().st_size > LIMIT:
                        raise ValueError("manual response too large")
                    self.reply(200, wire_response(json.loads(response.read_bytes()), request["MaxOutputTokens"]))
                    return
                time.sleep(0.05)
            self.reply(504, {"error": "manual response not yet available; preserve same request"})
        except TimeoutError:
            self.connection.settimeout(self.server.io_timeout_seconds)
            self.reply(408, {"error": "fixture request IO timeout"})
        except (ValueError, TypeError, KeyError, AttributeError):
            self.reply(400, {"error": "invalid fixture request or response"})
        except (BrokenPipeError, ConnectionResetError):
            pass
        finally:
            self.server.slots.release()

if __name__ == "__main__":
    import os
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mailbox", required=True)
    parser.add_argument("--listen", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=18089)
    args = parser.parse_args()
    if os.environ.get("ZINUSYA_SYNTHETIC_COUNTED_QA") != "1":
        raise SystemExit("Explicit ZINUSYA_SYNTHETIC_COUNTED_QA=1 required; never production")
    key = os.environ.get("ZINUSYA_COUNTED_QA_KEY", "")
    server = FixtureServer((args.listen, args.port), args.mailbox, key)
    print("Synthetic counted QA fixture listening; no real model/tokenization or upstream requests", flush=True)
    server.serve_forever()
