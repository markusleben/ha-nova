import { WebSocket } from "ws";

// Custom createSocket for home-assistant-js-websocket.
//
// The Node global WebSocket (undici) negotiates permessage-deflate and
// enforces a max *decompressed* message size. Large registry responses
// (e.g. `config/entity_registry/list` on instances with thousands of
// entities) exceed that limit; undici then kills the connection with an
// opaque close (code 1006, "Max decompressed message size exceeded" only
// visible on the error event). The `ws` client lets us disable compression
// entirely and set an explicit payload ceiling, so large command results
// arrive intact.
const MAX_PAYLOAD_BYTES = 256 * 1024 * 1024;

const MSG_TYPE_AUTH_REQUIRED = "auth_required";
const MSG_TYPE_AUTH_INVALID = "auth_invalid";
const MSG_TYPE_AUTH_OK = "auth_ok";

export interface AuthenticatedHaSocketOptions {
  haUrl: string;
  token: string;
  webSocketImpl?: typeof WebSocket;
}

export class HaSocketAuthError extends Error {}
export class HaSocketConnectError extends Error {}

export function haWebSocketUrl(haUrl: string): string {
  const normalized = haUrl.replace(/^http/, "ws").replace(/\/$/, "");
  if (normalized.endsWith("/core")) {
    return normalized + "/websocket";
  }
  return normalized + "/api/websocket";
}

// Performs the HA auth handshake and resolves with an authenticated socket
// carrying `haVersion`, matching the shape home-assistant-js-websocket
// expects from its `createSocket` option.
export function createAuthenticatedHaSocket(
  options: AuthenticatedHaSocketOptions
): Promise<WebSocket & { haVersion: string }> {
  const WebSocketImpl = options.webSocketImpl ?? WebSocket;
  const url = haWebSocketUrl(options.haUrl);

  return new Promise((resolve, reject) => {
    const socket = new WebSocketImpl(url, {
      perMessageDeflate: false,
      maxPayload: MAX_PAYLOAD_BYTES
    }) as WebSocket & { haVersion: string };

    let settled = false;

    const settleReject = (error: Error) => {
      if (settled) {
        return;
      }
      settled = true;
      removeHandshakeListeners();
      try {
        socket.close();
      } catch {
        // Socket may already be closed.
      }
      reject(error);
    };

    const onMessage = (event: { data: unknown }) => {
      let message: { type?: unknown; ha_version?: unknown };
      try {
        message = JSON.parse(String(event.data)) as { type?: unknown; ha_version?: unknown };
      } catch {
        settleReject(new HaSocketConnectError("HA sent an unparseable handshake message"));
        return;
      }

      switch (message.type) {
        case MSG_TYPE_AUTH_REQUIRED:
          socket.send(JSON.stringify({ type: "auth", access_token: options.token }));
          return;
        case MSG_TYPE_AUTH_INVALID:
          settleReject(new HaSocketAuthError("HA rejected the upstream access token"));
          return;
        case MSG_TYPE_AUTH_OK: {
          settled = true;
          removeHandshakeListeners();
          // ws sockets are EventEmitters: a post-auth transport error with no
          // 'error' listener would crash the Node process. Swallow it and
          // close — the close event drives home-assistant-js-websocket's
          // reconnect path (it only attaches message/close handlers itself).
          socket.addEventListener("error", () => {
            try {
              socket.close();
            } catch {
              // Socket may already be closed.
            }
          });
          socket.haVersion = typeof message.ha_version === "string" ? message.ha_version : "unknown";
          resolve(socket);
          return;
        }
        default:
          // Ignore anything else during the handshake phase.
          return;
      }
    };

    const onClose = () => {
      settleReject(new HaSocketConnectError("HA WebSocket closed during the auth handshake"));
    };

    const onError = () => {
      settleReject(new HaSocketConnectError("HA WebSocket errored during the auth handshake"));
    };

    const removeHandshakeListeners = () => {
      socket.removeEventListener("message", onMessage);
      socket.removeEventListener("close", onClose);
      socket.removeEventListener("error", onError);
    };

    socket.addEventListener("message", onMessage);
    socket.addEventListener("close", onClose);
    socket.addEventListener("error", onError);
  });
}
