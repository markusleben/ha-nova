import { afterEach, describe, expect, it } from "vitest";

import { bootstrapRuntime, startBridge } from "../../src/runtime/start.js";

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

  it("uses addon option LLAT when env LLAT is missing", () => {
    let seenSource: string | null = null;

    const runtime = bootstrapRuntime({
      loadEnv: () => ({
        haToken: "bridge-token",
        haUrl: "http://supervisor/core",
        bridgeVersion: "1.2.3",
        addonOptionsPath: "/data/options.json",
        bridgePort: 8791,
        logLevel: "info",
        wsAllowlistExtra: []
      }),
      readAddonOptions: () => ({
        ha_llat: "addon-llat"
      }),
      createWsClient: (input) => {
        seenSource = input.upstreamAuth.source;
        return {
          isConnected: () => true,
          sendMessage: async <T>() => ({ ok: true } as T)
        };
      }
    });

    expect(runtime.upstreamAuth.source).toBe("addon_option_ha_llat");
    expect(seenSource).toBe("addon_option_ha_llat");
    expect(runtime.app.version).toBe("1.2.3");
  });

  it("starts in limited mode when only supervisor token is available", async () => {
    const runtime = bootstrapRuntime({
      loadEnv: () => ({
        haToken: "bridge-token",
        supervisorToken: "supervisor-token",
        haUrl: "http://supervisor/core",
        bridgeVersion: "1.2.3",
        addonOptionsPath: "/data/options.json",
        bridgePort: 8791,
        logLevel: "info",
        wsAllowlistExtra: []
      }),
      readAddonOptions: () => ({})
    });

    servers.push(runtime.app.server);
    const baseUrl = await startServer(runtime.app.server);

    const health = await fetch(`${baseUrl}/health`, {
      headers: { authorization: "Bearer bridge-token" }
    });

    expect(health.status).toBe(200);
    await expect(health.json()).resolves.toEqual({
      ok: true,
      data: {
        status: "ok",
        ha_ws_connected: false,
        version: "1.2.3",
        uptime_s: expect.any(Number)
      }
    });

    const ws = await fetch(`${baseUrl}/ws`, {
      method: "POST",
      headers: {
        authorization: "Bearer bridge-token",
        "content-type": "application/json"
      },
      body: JSON.stringify({ type: "ping" })
    });

    expect(runtime.upstreamAuth.source).toBe("supervisor_token");
    expect(runtime.upstreamAuth.capability).toBe("limited");
    expect(ws.status).toBe(502);
    await expect(ws.json()).resolves.toEqual({
      ok: false,
      error: {
        code: "UPSTREAM_WS_ERROR",
        message: "LLAT is required for full WebSocket scope. Configure HA_LLAT or addon option 'ha_llat'."
      }
    });
  });

  it("logs startup auth context and listens successfully in limited mode", async () => {
    const infoLogs: Array<{ message: string; context: Record<string, unknown> | undefined }> = [];
    const warnLogs: Array<{ message: string; context: Record<string, unknown> | undefined }> = [];
    let listenCalledWithPort: number | null = null;

    const result = await startBridge({
      loadEnv: () => ({
        haToken: "bridge-token",
        supervisorToken: "supervisor-token",
        haUrl: "http://supervisor/core",
        bridgeVersion: "1.2.3",
        addonOptionsPath: "/data/options.json",
        bridgePort: 8791,
        logLevel: "info",
        wsAllowlistExtra: []
      }),
      readAddonOptions: () => ({}),
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

    expect(result.upstreamAuth.source).toBe("supervisor_token");
    expect(result.upstreamAuth.capability).toBe("limited");
    expect(listenCalledWithPort).toBe(8791);
    expect(infoLogs).toContainEqual({
      message: "Bridge bootstrap",
      context: {
        ha_url: "http://supervisor/core",
        bridge_port: 8791,
        addon_options_path: "/data/options.json",
        auth_source: "supervisor_token",
        auth_capability: "limited"
      }
    });
    expect(warnLogs).toContainEqual({
      message: "LLAT missing. Falling back to SUPERVISOR_TOKEN with limited API scope.",
      context: undefined
    });
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
