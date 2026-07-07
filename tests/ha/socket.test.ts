import { AddressInfo } from "node:net";

import { describe, expect, it } from "vitest";
import { WebSocketServer } from "ws";

import {
  HaSocketAuthError,
  HaSocketConnectError,
  createAuthenticatedHaSocket,
  haWebSocketUrl
} from "../../nova/src/ha/socket.js";

function startServer(handler: (socket: import("ws").WebSocket, request: import("node:http").IncomingMessage) => void): Promise<{ url: string; close: () => void }> {
  return new Promise((resolve) => {
    const server = new WebSocketServer({ port: 0 });
    server.on("connection", handler);
    server.on("listening", () => {
      const { port } = server.address() as AddressInfo;
      resolve({ url: `http://127.0.0.1:${port}`, close: () => server.close() });
    });
  });
}

// Real localhost sockets: under full-suite parallel load the TCP/WS
// handshake can transiently error — retry instead of failing the push gate.
describe("ha authenticated socket", { retry: 2 }, () => {
  it("builds the HA websocket URL from the base URL", () => {
    expect(haWebSocketUrl("http://homeassistant:8123")).toBe("ws://homeassistant:8123/api/websocket");
    expect(haWebSocketUrl("https://ha.example/")).toBe("wss://ha.example/api/websocket");
  });

  it("completes the auth handshake without negotiating compression", async () => {
    let sawExtensions: string | undefined = "unset";
    const server = await startServer((socket, request) => {
      sawExtensions = request.headers["sec-websocket-extensions"] as string | undefined;
      socket.send(JSON.stringify({ type: "auth_required" }));
      socket.on("message", (raw) => {
        const message = JSON.parse(String(raw)) as { type?: string; access_token?: string };
        if (message.type === "auth" && message.access_token === "token-1") {
          socket.send(JSON.stringify({ type: "auth_ok", ha_version: "2026.6.4" }));
        } else {
          socket.send(JSON.stringify({ type: "auth_invalid" }));
        }
      });
    });

    try {
      const socket = await createAuthenticatedHaSocket({ haUrl: server.url, token: "token-1" });
      expect(socket.haVersion).toBe("2026.6.4");
      // permessage-deflate must NOT be offered — the undici deflate path is
      // exactly what truncated large registry responses.
      expect(sawExtensions).toBeUndefined();
      socket.close();
    } finally {
      server.close();
    }
  });

  it("delivers multi-megabyte messages after the handshake", async () => {
    const bigPayload = JSON.stringify({ id: 1, type: "result", success: true, result: "x".repeat(8 * 1024 * 1024) });
    const server = await startServer((socket) => {
      socket.send(JSON.stringify({ type: "auth_required" }));
      socket.on("message", (raw) => {
        const message = JSON.parse(String(raw)) as { type?: string };
        if (message.type === "auth") {
          socket.send(JSON.stringify({ type: "auth_ok", ha_version: "2026.6.4" }));
          socket.send(bigPayload);
        }
      });
    });

    try {
      const socket = await createAuthenticatedHaSocket({ haUrl: server.url, token: "token-1" });
      const received = await new Promise<string>((resolve) => {
        socket.addEventListener("message", (event) => resolve(String(event.data)));
      });
      expect(received.length).toBe(bigPayload.length);
      socket.close();
    } finally {
      server.close();
    }
  });

  it("rejects with an auth error when HA declines the token", async () => {
    const server = await startServer((socket) => {
      socket.send(JSON.stringify({ type: "auth_required" }));
      socket.on("message", () => {
        socket.send(JSON.stringify({ type: "auth_invalid" }));
      });
    });

    try {
      await expect(createAuthenticatedHaSocket({ haUrl: server.url, token: "bad" })).rejects.toBeInstanceOf(
        HaSocketAuthError
      );
    } finally {
      server.close();
    }
  });

  it("rejects with a connect error when the socket closes mid-handshake", async () => {
    const server = await startServer((socket) => {
      socket.close();
    });

    try {
      await expect(createAuthenticatedHaSocket({ haUrl: server.url, token: "t" })).rejects.toBeInstanceOf(
        HaSocketConnectError
      );
    } finally {
      server.close();
    }
  });
});

describe("ha authenticated socket post-auth error safety", { retry: 2 }, () => {
  it("keeps an error listener after auth so transport errors cannot crash the process", async () => {
    const server = await startServer((socket) => {
      socket.send(JSON.stringify({ type: "auth_required" }));
      socket.on("message", () => {
        socket.send(JSON.stringify({ type: "auth_ok", ha_version: "2026.6.4" }));
      });
    });

    try {
      const socket = await createAuthenticatedHaSocket({ haUrl: server.url, token: "t" });
      // ws sockets are EventEmitters: with zero 'error' listeners this emit
      // would throw synchronously (and crash the process outside tests).
      expect(() => {
        (socket as unknown as { emit: (event: string, error: Error) => boolean }).emit(
          "error",
          new Error("simulated post-auth transport error")
        );
      }).not.toThrow();
      await new Promise((resolve) => setTimeout(resolve, 20));
    } finally {
      server.close();
    }
  });
});
