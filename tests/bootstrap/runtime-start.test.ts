import { afterEach, describe, expect, it } from "vitest";

import { bootstrapRuntime, startRelay } from "../../nova/src/runtime/start.js";

describe("runtime bootstrap", () => {
  const servers: Array<ReturnType<typeof bootstrapRuntime>["app"]["server"]> = [];

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

  it("uses env LLAT as the only upstream source", () => {
    let seenSource: string | null = null;
    let restSeenSource: string | null = null;

    const runtime = bootstrapRuntime({
      loadEnv: () => ({
        relayAuthToken: "relay-token",
        haLlat: "env-llat",
        haUrl: "http://supervisor/core",
        relayVersion: "1.2.3",
        appOptionsPath: "/data/options.json",
        relayPort: 8791,
        logLevel: "info"
      }),
      readAppOptions: () => ({
        ha_llat: "app-llat"
      }),
      createWsClient: (input) => {
        seenSource = input.upstreamAuth.source;
        return {
          isConnected: () => true,
          sendMessage: async <T>() => ({ ok: true } as T)
        };
      },
      createRestClient: (input) => {
        restSeenSource = input.upstreamAuth.source;
        return {
          request: async () => ({
            status: 200,
            body: { ok: true }
          })
        };
      }
    });

    expect(runtime.upstreamAuth.source).toBe("env_ha_llat");
    expect(seenSource).toBe("env_ha_llat");
    expect(restSeenSource).toBe("env_ha_llat");
    expect(runtime.app.version).toBe("1.2.3");
  });

  it("throws when no LLAT source is available", () => {
    expect(() =>
      bootstrapRuntime({
        loadEnv: () => ({
          relayAuthToken: "relay-token",
          haLlat: "   ",
          haUrl: "http://supervisor/core",
          relayVersion: "1.2.3",
          appOptionsPath: "/data/options.json",
          relayPort: 8791,
          logLevel: "info"
        }),
        readAppOptions: () => ({})
      })
    ).toThrowError("HA_LLAT is required for runtime startup.");
  });

  it("starts and serves when LLAT is available", async () => {
    const runtime = bootstrapRuntime({
      loadEnv: () => ({
        relayAuthToken: "relay-token",
        haLlat: "env-llat",
        haUrl: "http://supervisor/core",
        relayVersion: "1.2.3",
        appOptionsPath: "/data/options.json",
        relayPort: 8791,
        logLevel: "info"
      }),
      readAppOptions: () => ({}),
      createWsClient: () => ({
        isConnected: () => true,
        sendMessage: async <T>() => ({ type: "pong" } as T)
      })
    });

    servers.push(runtime.app.server);
    const baseUrl = await startServer(runtime.app.server);

    const health = await fetch(`${baseUrl}/health`, {
      headers: { authorization: "Bearer relay-token" }
    });

    expect(health.status).toBe(200);
    await expect(health.json()).resolves.toEqual({
      ok: true,
      data: {
        status: "ok",
        ha_ws_connected: true,
        version: "1.2.3",
        uptime_s: expect.any(Number)
      }
    });

    const ws = await fetch(`${baseUrl}/ws`, {
      method: "POST",
      headers: {
        authorization: "Bearer relay-token",
        "content-type": "application/json"
      },
      body: JSON.stringify({ type: "ping" })
    });

    expect(runtime.upstreamAuth.source).toBe("env_ha_llat");
    expect(runtime.upstreamAuth.capability).toBe("full");
    expect(ws.status).toBe(200);
    await expect(ws.json()).resolves.toEqual({
      ok: true,
      data: {
        type: "pong"
      }
    });
  });

  it("forwards /core through injected rest client", async () => {
    const seenRequests: Array<{ method: string; path: string; body: unknown }> = [];

    const runtime = bootstrapRuntime({
      loadEnv: () => ({
        relayAuthToken: "relay-token",
        haLlat: "env-llat",
        haUrl: "http://supervisor/core",
        relayVersion: "1.2.3",
        appOptionsPath: "/data/options.json",
        relayPort: 8791,
        logLevel: "info"
      }),
      readAppOptions: () => ({}),
      createWsClient: () => ({
        isConnected: () => true,
        sendMessage: async <T>() => ({ type: "pong" } as T)
      }),
      createRestClient: () => ({
        request: async (input) => {
          seenRequests.push({
            method: input.method,
            path: input.path,
            body: input.body
          });
          return {
            status: 200,
            body: {
              echoed: input.path
            }
          };
        }
      })
    });

    servers.push(runtime.app.server);
    const baseUrl = await startServer(runtime.app.server);

    const core = await fetch(`${baseUrl}/core`, {
      method: "POST",
      headers: {
        authorization: "Bearer relay-token",
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
          echoed: "/api/states"
        }
      }
    });
    expect(seenRequests).toEqual([
      {
        method: "GET",
        path: "/api/states",
        body: undefined
      }
    ]);
  });

  it("logs startup auth context and listens successfully in full mode", async () => {
    const infoLogs: Array<{ message: string; context: Record<string, unknown> | undefined }> = [];
    const warnLogs: Array<{ message: string; context: Record<string, unknown> | undefined }> = [];
    let listenCalledWithPort: number | null = null;

    const result = await startRelay({
      loadEnv: () => ({
        relayAuthToken: "relay-token",
        haLlat: "env-llat",
        haUrl: "http://supervisor/core",
        relayVersion: "1.2.3",
        appOptionsPath: "/data/options.json",
        relayPort: 8791,
        logLevel: "info"
      }),
      readAppOptions: () => ({}),
      createWsClient: () => ({
        isConnected: () => true,
        sendMessage: async <T>() => ({ type: "pong" } as T)
      }),
      logger: {
        info: (message, context) => {
          infoLogs.push({ message, context });
        },
        warn: (message, context) => {
          warnLogs.push({ message, context });
        },
        error: () => {}
      },
      listen: async (_server, port) => {
        listenCalledWithPort = port;
      }
    });

    expect(result.upstreamAuth.source).toBe("env_ha_llat");
    expect(result.upstreamAuth.capability).toBe("full");
    expect(listenCalledWithPort).toBe(8791);
    expect(infoLogs).toContainEqual({
      message: "Relay bootstrap",
      context: {
        ha_url: "http://supervisor/core",
        relay_port: 8791,
        app_options_path: "/data/options.json",
        auth_source: "env_ha_llat",
        auth_capability: "full"
      }
    });
    expect(warnLogs).toEqual([]);
  });
});

async function startServer(
  server: ReturnType<typeof bootstrapRuntime>["app"]["server"]
): Promise<string> {
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
