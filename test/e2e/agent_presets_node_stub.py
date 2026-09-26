#!/usr/bin/env python3
"""In-process substitute for llmman's node health endpoint and proxy blocker."""

import json
import os
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class NodeHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/llmman/node":
            self.send_error(404)
            return
        body = json.dumps({"memory": 17179869184, "loaded": {}, "stored": {}}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def log_message(self, fmt, *args):
        sys.stderr.write("%s - - [%s] %s\n" % (self.address_string(), self.log_date_time_string(), fmt % args))
        sys.stderr.flush()

class ProxyBlockerHandler(BaseHTTPRequestHandler):
    def do_CONNECT(self):
        self._record()
        self.send_error(502, "Offline fallback test: external access blocked")

    def do_GET(self):
        self._record()
        self.send_error(502, "Offline fallback test: external access blocked")

    def _record(self):
        log_path = os.environ.get("CHAIRLIFT_AGENT_PROXY_LOG")
        if log_path:
            with open(log_path, "a", encoding="utf-8") as f:
                f.write(f"BLOCKED: {self.command} {self.path}\n")

    def log_message(self, fmt, *args):
        sys.stderr.write("%s - - [%s] %s\n" % (self.address_string(), self.log_date_time_string(), fmt % args))
        sys.stderr.flush()
def run():
    node_server = ThreadingHTTPServer(("127.0.0.1", 17434), NodeHandler)
    proxy_server = ThreadingHTTPServer(("127.0.0.1", 17435), ProxyBlockerHandler)

    t1 = threading.Thread(target=node_server.serve_forever, daemon=True)
    t2 = threading.Thread(target=proxy_server.serve_forever, daemon=True)
    t1.start()
    t2.start()
    t1.join()


if __name__ == "__main__":
    run()
