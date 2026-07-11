#!/usr/bin/env python3
"""Mint a long-lived access token on a disposable Home Assistant.

Home Assistant only offers this over the WebSocket API (auth/long_lived_access_token),
not over REST — the same path the user's browser takes in Profile > Security.
Used by run.sh; not part of the shipped product.
"""

import json
import sys
from urllib.parse import urlparse

try:
    from websockets.sync.client import connect
except ImportError:  # pragma: no cover - developer environment hint
    print(
        "missing dependency: pip install websockets  (needed only for the disposable-HA e2e)",
        file=sys.stderr,
    )
    raise SystemExit(2)


def main() -> int:
    ha_url, access_token = sys.argv[1], sys.argv[2]
    parsed = urlparse(ha_url)
    ws_url = f"ws://{parsed.netloc}/api/websocket"

    with connect(ws_url) as ws:
        # auth_required -> auth -> auth_ok
        json.loads(ws.recv())
        ws.send(json.dumps({"type": "auth", "access_token": access_token}))
        auth_result = json.loads(ws.recv())
        if auth_result.get("type") != "auth_ok":
            print(f"authentication failed: {auth_result}", file=sys.stderr)
            return 1

        ws.send(
            json.dumps(
                {
                    "id": 1,
                    "type": "auth/long_lived_access_token",
                    "client_name": "HA NOVA E2E",
                    "lifespan": 1,
                }
            )
        )
        result = json.loads(ws.recv())

    if not result.get("success"):
        print(f"could not mint a long-lived token: {result}", file=sys.stderr)
        return 1

    print(result["result"])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
