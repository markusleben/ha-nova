# Fake Companion push relay for the phone lane (phone.mjs). Home Assistant's
# mobile_app notify posts here instead of to a real phone. Keeps each body in
# memory (push_token dropped) and lists them on GET /pushes. Local only.
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

pushes = []


class Handler(BaseHTTPRequestHandler):
    def reply(self, code, body):
        data = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))) or b"{}")
        body.pop("push_token", None)
        pushes.append({"path": self.path, "body": body})
        # What the real relay answers; Home Assistant only logs it.
        self.reply(201, {"rateLimits": {"successful": 1, "errors": 0, "maximum": 500, "remaining": 499, "resetsAt": "2099-01-01T00:00:00Z"}})

    def do_GET(self):
        self.reply(200, pushes) if self.path == "/pushes" else self.reply(404, {})

    def log_message(self, *args):
        pass


ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
