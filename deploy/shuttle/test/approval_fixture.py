"""Disposable exact-approval display fixture. It cannot execute business writes."""
import argparse
from datetime import datetime, timedelta, timezone
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import threading
import uuid

MODULE = "zinusya_qa_approval"


def manifest():
    return {
        "protocol_version": 1,
        "service_id": "zinusya-qa-approval",
        "revision": "display-v1",
        "description": "Disposable synthetic Telegram approval display QA; reject the action.",
        "modules": [{
            "id": MODULE, "description": "Synthetic approval display only; no business writes",
            "permissions": [], "discovery": "static", "methods": [{
                "id": "display", "description": "Prepare synthetic literal parameters for display; reject in Telegram.",
                "permissions": [], "path": "/execute", "effect": "write",
                "timeout_ms": 3000, "max_result_bytes": 65536,
                "input_schema": {"type": "object", "additionalProperties": False,
                    "required": ["case_id", "text"], "properties": {
                        "case_id": {"type": "string", "maxLength": 100},
                        "text": {"type": "string", "maxLength": 20000}}},
                "output_schema": {"type": "object", "additionalProperties": False},
                "idempotency": True, "status_path": "/status",
                "deduplication_window_seconds": 86400,
                "confirmation": {"mode": "exact_parameters", "prepare_path": "/prepare"},
                "artifact_mime_types": [], "external_disclosure": False,
            }],
        }],
    }


class Fixture:
    def __init__(self, expected, state_file, resume=False):
        self.expected = expected
        self.state_file = state_file
        self.lock = threading.Lock()
        self.preparations = {}
        self.execute_attempts = 0
        self.executions = []
        if resume:
            saved = json.loads(state_file.read_text(encoding="utf-8"))
            expected_hash = hashlib.sha256(json.dumps(
                expected, sort_keys=True, ensure_ascii=False).encode()).hexdigest()
            if saved["arguments_sha256"] != expected_hash:
                raise ValueError("fixture arguments changed across restart")
            self.preparations = saved["preparations"]
            self.execute_attempts = saved["execute_attempts"]
            self.executions = saved["executions"]
        self.save()

    def save(self):
        # Persist no credentials or delegated identity data.
        record = {"operation_ids": sorted(self.preparations),
                  "preparations": self.preparations,
                  "execute_attempts": self.execute_attempts,
                  "executions": self.executions,
                  "arguments_sha256": hashlib.sha256(json.dumps(
                      self.expected, sort_keys=True, ensure_ascii=False).encode()).hexdigest()}
        temporary = self.state_file.with_suffix(".tmp")
        temporary.write_text(json.dumps(record), encoding="utf-8")
        temporary.replace(self.state_file)

    def prepare(self, operation):
        if operation not in self.preparations:
            self.preparations[operation] = {
                "status": "prepared", "operation_id": operation,
                "arguments": self.expected, "preconditions": {"fixture": "display-only"},
                "expires_at": (datetime.now(timezone.utc) + timedelta(minutes=5)).isoformat(),
                "description": "Синтетическая проверка полного JSON. Отклоните действие; бизнес-данные не изменятся.",
            }
            self.save()
        return self.preparations[operation]


def handler(fixture):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def reply(self, status, document):
            raw = json.dumps(document, ensure_ascii=False).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)

        def do_GET(self):
            if self.path == "/manifest":
                self.reply(200, manifest())
            elif self.path == "/health":
                self.reply(200, {"fixture": "display-only"})
            else:
                self.reply(404, {})

        def do_POST(self):
            # Loom authorizes the user; this fixture holds only preselected synthetic data.
            # Even an approved request is unconditionally refused below.
            if not self.headers.get("Authorization", "").startswith("Bearer "):
                self.reply(401, {})
                return
            try:
                length = int(self.headers.get("Content-Length", "0"))
                if not 0 < length <= 65536:
                    raise ValueError("invalid body size")
                data = json.loads(self.rfile.read(length))
                operation = str(uuid.UUID(data["operation_id"]))
                if data.get("protocol_version") != 1:
                    raise ValueError("invalid protocol")
            except (ValueError, KeyError, TypeError):
                self.reply(400, {})
                return
            with fixture.lock:
                if self.path == "/prepare" and data.get("arguments") == fixture.expected:
                    self.reply(200, fixture.prepare(operation))
                elif self.path == "/status":
                    refused = any(e["operation_id"] == operation for e in fixture.executions)
                    self.reply(200, {"status": "failed" if refused else "not_found",
                                     "operation_id": operation})
                elif self.path == "/execute":
                    fixture.execute_attempts += 1
                    fixture.executions.append({
                        "operation_id": operation,
                        "arguments_sha256": hashlib.sha256(json.dumps(
                            data.get("arguments"), sort_keys=True, ensure_ascii=False).encode()).hexdigest(),
                    })
                    fixture.save()
                    self.reply(200, {"status": "rejected", "operation_id": operation,
                                     "error": "fixture_execution_disabled"})
                else:
                    self.reply(400, {})
    return Handler


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--expected", type=Path, required=True)
    parser.add_argument("--state", type=Path, required=True)
    parser.add_argument("--resume", action="store_true")
    args = parser.parse_args()
    if args.state.exists() and not args.resume:
        raise SystemExit("Use a fresh disposable state file; do not overwrite prior QA evidence")
    fixture = Fixture(json.loads(args.expected.read_text(encoding="utf-8")), args.state, args.resume)
    ThreadingHTTPServer(("0.0.0.0", 8080), handler(fixture)).serve_forever()
