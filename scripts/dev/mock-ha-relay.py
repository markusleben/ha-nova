#!/usr/bin/env python3
import argparse
import json
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer


class HAHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "text/plain")
        self.end_headers()
        self.wfile.write(b"OK")

    def log_message(self, format, *args):
        return


class RelayHandler(BaseHTTPRequestHandler):
    reported_version = "0.1.12"

    def do_GET(self):
        if self.path == "/health":
            body = json.dumps(
                {
                    "status": "ok",
                    "version": self.reported_version,
                    "ha_ws_connected": True,
                }
            ).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return

        self.send_response(404)
        self.end_headers()

    def log_message(self, format, *args):
        return


def serve(port, handler):
    HTTPServer(("0.0.0.0", port), handler).serve_forever()


def main():
    parser = argparse.ArgumentParser(
        description="Tiny HA + fake relay /health mock for private RC validation"
    )
    parser.add_argument("--ha-port", type=int, default=8123)
    parser.add_argument("--relay-port", type=int, default=8791)
    parser.add_argument("--reported-version", default="0.1.12")
    args = parser.parse_args()

    RelayHandler.reported_version = args.reported_version
    print(f"[mock-ha-relay] HA listening on :{args.ha_port}")
    print(f"[mock-ha-relay] Fake relay /health listening on :{args.relay_port}")
    print(f"[mock-ha-relay] Fake relay /health reported version: {args.reported_version}")

    threading.Thread(target=serve, args=(args.ha_port, HAHandler), daemon=True).start()
    threading.Thread(target=serve, args=(args.relay_port, RelayHandler), daemon=True).start()
    threading.Event().wait()


if __name__ == "__main__":
    main()
