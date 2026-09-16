#!/usr/bin/env python3
import json
import os
import urllib.error
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

UPSTREAM = os.environ.get("UPSTREAM_URL", "http://gitea:3000").rstrip("/")
LOG_PATH = os.environ.get("FIXTURE_LOG", "/state/actions-fixture.log")
PREFIX = "/api/v1/repos/gateway/bar/actions/"


def write_log(method, path, query):
    with open(LOG_PATH, "a", encoding="utf-8") as stream:
        stream.write(json.dumps({"method": method, "path": path, "query": query}, separators=(",", ":")) + "\n")


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self):
        self.handle_request()

    def do_POST(self):
        self.handle_request()

    def do_PATCH(self):
        self.handle_request()

    def do_PUT(self):
        self.handle_request()

    def handle_request(self):
        parsed = urllib.parse.urlsplit(self.path)
        if parsed.path.startswith(PREFIX):
            write_log(self.command, parsed.path, parsed.query)
            self.handle_actions(parsed)
            return
        self.forward()

    def handle_actions(self, parsed):
        suffix = parsed.path[len(PREFIX):]
        query = urllib.parse.parse_qs(parsed.query)
        head_sha = query.get("head_sha", ["fixture-head"])[0]
        if self.command != "GET":
            self.send_json(405, {"message": "fixture Actions mutations are disabled"})
        elif suffix == "runs":
            self.send_json(200, {
                "total_count": 5,
                "workflow_runs": [
                    {"id": 101, "display_title": "CI", "path": ".gitea/workflows/ci.yml@refs/heads/feature", "status": "completed", "conclusion": "failure", "head_sha": head_sha, "html_url": "https://fixture.example.test/gateway/bar/actions/runs/1"},
                    {"id": 102, "display_title": "success", "path": ".gitea/workflows/ci.yml@refs/heads/feature", "status": "completed", "conclusion": "success", "head_sha": head_sha, "html_url": "https://fixture.example.test/gateway/bar/actions/runs/2"},
                    {"id": 103, "display_title": "skipped", "path": ".gitea/workflows/ci.yml@refs/heads/feature", "status": "completed", "conclusion": "skipped", "head_sha": head_sha, "html_url": "https://fixture.example.test/gateway/bar/actions/runs/3"},
                    {"id": 104, "display_title": "running", "path": ".gitea/workflows/ci.yml@refs/heads/feature", "status": "in_progress", "head_sha": head_sha, "html_url": "https://fixture.example.test/gateway/bar/actions/runs/4"},
                    {"id": 105, "display_title": "wrong sha", "path": ".gitea/workflows/ci.yml@refs/heads/feature", "status": "completed", "conclusion": "failure", "head_sha": "0000000000000000000000000000000000000000", "html_url": "https://fixture.example.test/gateway/bar/actions/runs/5"},
                ],
            })
        elif suffix == "runs/101/jobs":
            self.send_json(200, {"total_count": 2, "jobs": [
                {"id": 201, "name": "matrix", "status": "completed", "conclusion": "failure", "html_url": "https://fixture.example.test/gateway/bar/actions/runs/1/jobs/0"},
                {"id": 202, "name": "matrix", "status": "completed", "conclusion": "cancelled", "html_url": "https://fixture.example.test/gateway/bar/actions/runs/1/jobs/1"},
            ]})
        elif suffix == "runs/104/jobs":
            self.send_json(200, {"total_count": 1, "jobs": [
                {"id": 204, "name": "still running", "status": "in_progress", "html_url": "https://fixture.example.test/gateway/bar/actions/runs/4/jobs/0"},
            ]})
        elif suffix == "runs/101":
            self.send_json(200, {"id": 101, "display_title": "CI", "path": ".gitea/workflows/ci.yml@refs/heads/feature", "status": "completed", "conclusion": "failure", "head_sha": head_sha})
        elif suffix == "workflows":
            self.send_json(200, {"total_count": 1, "workflows": [{"id": "ci.yml", "name": "CI"}]})
        elif suffix == "workflows/ci.yml":
            self.send_json(200, {"id": "ci.yml", "name": "CI"})
        elif suffix == "jobs/201/logs":
            self.send_bytes(200, b"fixture matrix one failed\n", "text/plain; charset=utf-8", "attachment")
        else:
            self.send_json(404, {"message": "fixture route not found"})

    def forward(self):
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length) if length else None
        request = urllib.request.Request(UPSTREAM + self.path, data=body, method=self.command)
        for name in ("Authorization", "Accept", "Content-Type"):
            if self.headers.get(name):
                request.add_header(name, self.headers[name])
        try:
            with urllib.request.urlopen(request) as response:
                data = response.read()
                self.send_bytes(response.status, data, response.headers.get("Content-Type", "application/octet-stream"), response.headers.get("Content-Disposition"))
        except urllib.error.HTTPError as error:
            self.send_bytes(error.code, error.read(), error.headers.get("Content-Type", "application/json"), error.headers.get("Content-Disposition"))

    def send_json(self, status, value):
        self.send_bytes(status, json.dumps(value, separators=(",", ":")).encode() + b"\n", "application/json; charset=utf-8")

    def send_bytes(self, status, body, content_type, disposition=None):
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        if disposition:
            self.send_header("Content-Disposition", disposition)
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args):
        return


os.makedirs(os.path.dirname(LOG_PATH), exist_ok=True)
ThreadingHTTPServer(("0.0.0.0", 8081), Handler).serve_forever()
