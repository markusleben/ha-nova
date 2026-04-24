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
