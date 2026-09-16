#!/usr/bin/env python3
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self):
        path = self.path.split("?", 1)[0]
        if path in ("/api/v3/repos/compat/repo/actions/runs/101", "/api/v3/repos/compat/repo/actions/runs/102"):
            run_id = int(path.rsplit("/", 1)[1])
            self.send_json(200, {"id": run_id, "workflow_id": 1, "status": "completed", "conclusion": "failure"})
        elif path == "/api/v3/repos/compat/repo/actions/workflows/1":
            self.send_json(200, {"id": 1, "name": "CI"})
        else:
            self.send_json(404, {"message": "Not Found"})

    def do_POST(self):
        if self.path == "/api/v3/repos/compat/repo/actions/runs/101/rerun-failed-jobs":
            self.send_json(201, {})
        elif self.path == "/api/v3/repos/compat/repo/actions/runs/102/rerun-failed-jobs":
            self.send_bytes(201, b"", "application/json")
        else:
            self.send_json(404, {"message": "Not Found"})

    def send_json(self, status, value):
        self.send_bytes(status, json.dumps(value, separators=(",", ":")).encode() + b"\n", "application/json")

    def send_bytes(self, status, body, content_type):
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args):
        return


ThreadingHTTPServer(("0.0.0.0", 8082), Handler).serve_forever()
