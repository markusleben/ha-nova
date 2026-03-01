import { afterEach, describe, expect, it } from "vitest";

import { createApp } from "../../src/index.js";

describe("app wiring", () => {
  const servers: Array<ReturnType<typeof createApp>["server"]> = [];

  afterEach(async () => {
    await Promise.all(
      servers.map(
        (server) =>
          new Promise<void>((resolve, reject) => {
            server.close((error) => {
              if (error) {
                reject(error);
                return;
              }
              resolve();
            });
          })
      )
    );
    servers.length = 0;
  });

  it("wires /health and /ws handlers in application router", async () => {
    const app = createApp({
      authToken: "secret",
      version: "1.0.0",
      wsClient: {
        isConnected: () => true,
        sendMessage: async (message) => ({ echoed: message.type })
      },
      coreClient: {
        request: async () => ({
          status: 200,
          body: {
            ok: true
          }
        })
      },
      startedAtMs: 1_000,
      now: () => 5_000
    });
    servers.push(app.server);

    const baseUrl = await startServer(app.server);

    const health = await fetch(`${baseUrl}/health`, {
      headers: { authorization: "Bearer secret" }
    });
    expect(health.status).toBe(200);
    await expect(health.json()).resolves.toEqual({
      ok: true,
      data: {
        status: "ok",
        ha_ws_connected: true,
        version: "1.0.0",
        uptime_s: 4
      }
    });

    const ws = await fetch(`${baseUrl}/ws`, {
      method: "POST",
      headers: {
        authorization: "Bearer secret",
        "content-type": "application/json"
      },
      body: JSON.stringify({ type: "ping" })
    });
    expect(ws.status).toBe(200);
    await expect(ws.json()).resolves.toEqual({
      ok: true,
      data: {
        echoed: "ping"
      }
    });

    const core = await fetch(`${baseUrl}/core`, {
      method: "POST",
      headers: {
        authorization: "Bearer secret",
        "content-type": "application/json"
      },
      body: JSON.stringify({
        method: "GET",
        path: "/api/states"
      })
    });
    expect(core.status).toBe(200);
    await expect(core.json()).resolves.toEqual({
      ok: true,
      data: {
        status: 200,
        body: {
          ok: true
        }
      }
    });
  });
});

async function startServer(server: ReturnType<typeof createApp>["server"]): Promise<string> {
  const address = await new Promise<{ port: number }>((resolve, reject) => {
    server.listen(0, "127.0.0.1", () => {
      const serverAddress = server.address();
      if (!serverAddress || typeof serverAddress === "string") {
        reject(new Error("Server did not return a TCP address"));
        return;
      }
      resolve(serverAddress);
    });
    server.on("error", reject);
  });

  return `http://127.0.0.1:${address.port}`;
}
