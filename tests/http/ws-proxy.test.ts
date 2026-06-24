import { afterEach, describe, expect, it } from "vitest";

import { createWsProxyHandler } from "../../nova/src/http/handlers/ws-proxy.js";
import { createRouter } from "../../nova/src/http/router.js";
import { createHttpServer } from "../../nova/src/http/server.js";

const TEST_AUTH_TOKEN = "secret";

describe("ws proxy endpoint", () => {
  const servers: Array<ReturnType<typeof createHttpServer>> = [];

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

  it("forwards ws message type and returns data", async () => {
    const router = createRouter();

    router.register(
      "POST",
      "/ws",
      createWsProxyHandler({
        wsClient: {
          sendMessage: async (message) => ({ echoed: message.type })
        }
      })
    );

    const { baseUrl } = await startServer(servers, router);
    const response = await fetch(`${baseUrl}/ws`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json"
      },
      body: JSON.stringify({ type: "ping" })
    });

    expect(response.status).toBe(200);
    await expect(response.json()).resolves.toEqual({
      ok: true,
      data: {
        echoed: "ping"
      }
    });
  });

  it("forwards unknown ws type without local type filtering", async () => {
    const router = createRouter();

    router.register(
      "POST",
      "/ws",
      createWsProxyHandler({
        wsClient: {
          sendMessage: async (message) => ({ echoed: message.type })
        }
      })
    );

    const { baseUrl } = await startServer(servers, router);
    const response = await fetch(`${baseUrl}/ws`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json"
      },
      body: JSON.stringify({ type: "evil/type" })
    });

    expect(response.status).toBe(200);
    await expect(response.json()).resolves.toEqual({
      ok: true,
      data: {
        echoed: "evil/type"
      }
    });
  });

  it("rejects subscription/stream ws types with 400 and does not forward them", async () => {
    const router = createRouter();
    let forwarded = false;
    router.register(
      "POST",
      "/ws",
      createWsProxyHandler({
        wsClient: {
          sendMessage: async () => {
            forwarded = true;
            return { ok: true };
          }
        }
      })
    );

    const { baseUrl } = await startServer(servers, router);
    for (const type of [
      "subscribe_events",
      "subscribe_trigger",
      "subscribe_entities",
      "render_template",
      // Slash-namespaced subscription commands also open upstream subscriptions
      // even though they do not start with `subscribe_`.
      "config_entries/subscribe",
      "config_entries/flow/subscribe",
    ]) {
      const response = await fetch(`${baseUrl}/ws`, {
        method: "POST",
        headers: {
          authorization: `Bearer ${TEST_AUTH_TOKEN}`,
          "content-type": "application/json"
        },
        body: JSON.stringify({ type })
      });
      expect(response.status, type).toBe(400);
      const json = (await response.json()) as { error: { code: string } };
      expect(json.error.code, type).toBe("UNSUPPORTED_WS_TYPE");
    }
    expect(forwarded).toBe(false);
  });

  it("collects bounded ws event responses through explicit envelope", async () => {
    const router = createRouter();
    let sentType = "";
    let collectOptions: unknown;
    router.register(
      "POST",
      "/ws",
      createWsProxyHandler({
        wsClient: {
          sendMessage: async () => {
            throw new Error("should collect events instead");
          },
          collectMessageEvents: async (message, options) => {
            sentType = message.type;
            collectOptions = options;
            return [
              { type: "initial", data: { homeassistant: { info: { version: "2026.6.4" } } } },
              { type: "finish" },
            ];
          }
        }
      })
    );

    const { baseUrl } = await startServer(servers, router);
    const response = await fetch(`${baseUrl}/ws`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json"
      },
      body: JSON.stringify({
        message: { type: "system_health/info" },
        collect_events: {
          until_type: "finish",
          max_events: 50,
          timeout_ms: 5000,
        },
      })
    });

    expect(response.status).toBe(200);
    expect(sentType).toBe("system_health/info");
    expect(collectOptions).toEqual({
      finishEventType: "finish",
      maxEvents: 50,
      timeoutMs: 5000,
    });
    await expect(response.json()).resolves.toEqual({
      ok: true,
      data: {
        events: [
          { type: "initial", data: { homeassistant: { info: { version: "2026.6.4" } } } },
          { type: "finish" },
        ]
      }
    });
  });

  it("does not collect events implicitly for system-health", async () => {
    const router = createRouter();
    let collected = false;
    router.register(
      "POST",
      "/ws",
      createWsProxyHandler({
        wsClient: {
          sendMessage: async (message) => ({ ack: message.type, data: null }),
          collectMessageEvents: async () => {
            collected = true;
            return [];
          },
        },
      })
    );

    const { baseUrl } = await startServer(servers, router);
    const response = await fetch(`${baseUrl}/ws`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json",
      },
      body: JSON.stringify({ type: "system_health/info" }),
    });

    expect(response.status).toBe(200);
    expect(collected).toBe(false);
    await expect(response.json()).resolves.toEqual({
      ok: true,
      data: {
        ack: "system_health/info",
        data: null,
      },
    });
  });

  it("rejects invalid ws event collection bounds", async () => {
    const router = createRouter();
    router.register(
      "POST",
      "/ws",
      createWsProxyHandler({
        wsClient: {
          sendMessage: async () => ({ ok: true }),
          collectMessageEvents: async () => [],
        },
      })
    );

    const { baseUrl } = await startServer(servers, router);
    for (const collect_events of [
      null,
      { until_type: "" },
      { max_events: 101 },
      { timeout_ms: 10_001 },
    ]) {
      const response = await fetch(`${baseUrl}/ws`, {
        method: "POST",
        headers: {
          authorization: `Bearer ${TEST_AUTH_TOKEN}`,
          "content-type": "application/json",
        },
        body: JSON.stringify({
          message: { type: "system_health/info" },
          collect_events,
        }),
      });
      expect(response.status).toBe(400);
      const json = (await response.json()) as { error: { code: string } };
      expect(json.error.code).toBe("VALIDATION_ERROR");
    }
  });

  it("rejects subscription ws types inside event collection envelope", async () => {
    const router = createRouter();
    let forwarded = false;
    router.register(
      "POST",
      "/ws",
      createWsProxyHandler({
        wsClient: {
          sendMessage: async () => {
            forwarded = true;
            return { ok: true };
          },
          collectMessageEvents: async () => {
            forwarded = true;
            return [];
          },
        },
      })
    );

    const { baseUrl } = await startServer(servers, router);
    const response = await fetch(`${baseUrl}/ws`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json",
      },
      body: JSON.stringify({
        message: { type: "subscribe_events" },
        collect_events: { until_type: "finish" },
      }),
    });

    expect(response.status).toBe(400);
    const json = (await response.json()) as { error: { code: string } };
    expect(json.error.code).toBe("UNSUPPORTED_WS_TYPE");
    expect(forwarded).toBe(false);
  });

  it("returns 400 for missing message type", async () => {
    const router = createRouter();

    router.register(
      "POST",
      "/ws",
      createWsProxyHandler({
        wsClient: {
          sendMessage: async () => ({ ok: true })
        }
      })
    );

    const { baseUrl } = await startServer(servers, router);
    const response = await fetch(`${baseUrl}/ws`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json"
      },
      body: JSON.stringify({ payload: "missing-type" })
    });

    expect(response.status).toBe(400);
    await expect(response.json()).resolves.toEqual({
      ok: false,
      error: {
        code: "VALIDATION_ERROR",
        message: "Request body must contain a string field 'type'"
      }
    });
  });

  it("returns 502 when ws upstream fails", async () => {
    const router = createRouter();

    router.register(
      "POST",
      "/ws",
      createWsProxyHandler({
        wsClient: {
          sendMessage: async () => {
            throw new Error("upstream down");
          }
        }
      })
    );

    const { baseUrl } = await startServer(servers, router);
    const response = await fetch(`${baseUrl}/ws`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json"
      },
      body: JSON.stringify({ type: "ping" })
    });

    expect(response.status).toBe(502);
    await expect(response.json()).resolves.toEqual({
      ok: false,
      error: {
        code: "UPSTREAM_WS_ERROR",
        message: "upstream down"
      }
    });
  });

  it("returns 413 before routing when ws body exceeds the configured limit", async () => {
    const router = createRouter();
    let routed = false;
    router.register("POST", "/ws", () => {
      routed = true;
      return { ok: true };
    });

    const { baseUrl } = await startServer(servers, router, 32);
    const response = await fetch(`${baseUrl}/ws`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json"
      },
      body: JSON.stringify({ type: "ping", pad: "x".repeat(32) })
    });

    expect(response.status).toBe(413);
    expect(routed).toBe(false);
    await expect(response.json()).resolves.toEqual({
      ok: false,
      error: {
        code: "PAYLOAD_TOO_LARGE",
        message: "Request body exceeds 32 bytes"
      }
    });
  });

  it("accepts an exact-limit valid ws body", async () => {
    const router = createRouter();
    router.register(
      "POST",
      "/ws",
      createWsProxyHandler({
        wsClient: {
          sendMessage: async (message) => ({ echoed: message.type })
        }
      })
    );

    const body = jsonBodyWithPad('{"type":"ping","pad":"', '"}', 64);
    const { baseUrl } = await startServer(servers, router, Buffer.byteLength(body));
    const response = await fetch(`${baseUrl}/ws`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json"
      },
      body
    });

    expect(response.status).toBe(200);
    await expect(response.json()).resolves.toEqual({
      ok: true,
      data: {
        echoed: "ping"
      }
    });
  });
});

async function startServer(
  servers: Array<ReturnType<typeof createHttpServer>>,
  router: ReturnType<typeof createRouter>,
  maxJsonBodyBytes?: number
): Promise<{ baseUrl: string }> {
  const server = createHttpServer({
    authToken: TEST_AUTH_TOKEN,
    router,
    ...(maxJsonBodyBytes === undefined ? {} : { maxJsonBodyBytes })
  });

  servers.push(server);

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

  return {
    baseUrl: `http://127.0.0.1:${address.port}`
  };
}

function jsonBodyWithPad(prefix: string, suffix: string, targetBytes: number): string {
  const fixedBytes = Buffer.byteLength(prefix) + Buffer.byteLength(suffix);
  const padBytes = targetBytes - fixedBytes;
  if (padBytes < 0) {
    throw new Error("targetBytes is smaller than fixed JSON body");
  }
  return `${prefix}${"x".repeat(padBytes)}${suffix}`;
}
