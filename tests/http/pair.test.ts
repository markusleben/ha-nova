import { afterEach, describe, expect, it } from "vitest";

import { createApp } from "../../nova/src/index.js";
import { createPairingManager } from "../../nova/src/security/pairing.js";

describe("POST /pair", () => {
  const servers: Array<ReturnType<typeof createApp>["server"]> = [];

  afterEach(async () => {
    await Promise.all(
      servers.map(
        (server) =>
          new Promise<void>((resolve) => {
            server.close(() => resolve());
          })
      )
    );
    servers.length = 0;
  });

  it("exchanges a code without bearer auth, disables caching, and rejects replay", async () => {
    const logged: unknown[] = [];
    const baseUrl = await startApp(logged);

    const paired = await postPair(baseUrl, { code: "123456" });
    expect(paired.status).toBe(200);
    expect(paired.headers.get("cache-control")).toBe("no-store");
    await expect(paired.json()).resolves.toEqual({
      ok: true,
      data: { relay_token: "relay-secret" }
    });

    const replay = await postPair(baseUrl, { code: "123456" });
    expect(replay.status).toBe(401);
    expect(replay.headers.get("cache-control")).toBe("no-store");
    await expect(replay.json()).resolves.toMatchObject({
      ok: false,
      error: { code: "PAIRING_FAILED" }
    });
    expect(JSON.stringify(logged)).not.toContain("123456");
    expect(JSON.stringify(logged)).not.toContain("relay-secret");
  });

  it.each([
    [null],
    [[]],
    [{ code: 123456 }],
    [{ code: "12345" }],
    [{ code: "123456", extra: true }]
  ])("rejects malformed request shape %#", async (body) => {
    const baseUrl = await startApp();
    const response = await postPair(baseUrl, body);

    expect(response.status).toBe(400);
    expect(response.headers.get("cache-control")).toBe("no-store");
    await expect(response.json()).resolves.toMatchObject({
      ok: false,
      error: { code: "VALIDATION_ERROR" }
    });
  });

  it("keeps invalid JSON non-cacheable", async () => {
    const baseUrl = await startApp();
    const response = await fetch(`${baseUrl}/pair`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: "{"
    });

    expect(response.status).toBe(400);
    expect(response.headers.get("cache-control")).toBe("no-store");
    await expect(response.json()).resolves.toMatchObject({ error: { code: "INVALID_JSON" } });
  });

  it("rate-limits by socket peer and sends Retry-After", async () => {
    const baseUrl = await startApp();
    for (let attempt = 0; attempt < 5; attempt += 1) {
      const failed = await postPair(baseUrl, { code: "999999" }, `198.51.100.${attempt}`);
      expect(failed.status).toBe(401);
    }

    const limited = await postPair(baseUrl, { code: "123456" });
    expect(limited.status).toBe(429);
    expect(limited.headers.get("retry-after")).toBe("60");
    expect(limited.headers.get("cache-control")).toBe("no-store");
    await expect(limited.json()).resolves.toMatchObject({
      error: { code: "PAIRING_RATE_LIMITED" }
    });
  });

  it("does not exempt other methods or routes from bearer auth", async () => {
    const baseUrl = await startApp();

    const wrongMethod = await fetch(`${baseUrl}/pair`);
    expect(wrongMethod.status).toBe(401);
    expect(wrongMethod.headers.get("cache-control")).toBe("no-store");

    const health = await fetch(`${baseUrl}/health`);
    expect(health.status).toBe(401);
    expect(health.headers.get("cache-control")).toBeNull();
  });

  async function startApp(logged: unknown[] = []): Promise<string> {
    const pairing = createPairingManager({
      relayToken: "relay-secret",
      generateCodeNumber: sequence([123_456, 654_321])
    });
    const app = createApp({
      authToken: "relay-secret",
      version: "1.0.0",
      pairingManager: pairing,
      fileAccess: { mode: "off", configRoot: "", warnings: [] },
      snapshotRoot: "/tmp/nova-pair-test",
      wsClient: {
        isConnected: () => true,
        sendMessage: async () => ({}),
        collectMessageEvents: async () => ({ events: [], truncated: false })
      },
      coreClient: { request: async () => ({ status: 200, body: null }) },
      logger: {
        warn: (message, context) => logged.push({ message, context }),
        error: (message, context) => logged.push({ message, context })
      }
    });
    servers.push(app.server);

    const port = await new Promise<number>((resolve, reject) => {
      app.server.listen(0, "127.0.0.1", () => {
        const address = app.server.address();
        if (!address || typeof address === "string") {
          reject(new Error("Server did not return a TCP address"));
          return;
        }
        resolve(address.port);
      });
      app.server.on("error", reject);
    });
    return `http://127.0.0.1:${port}`;
  }
});

function postPair(baseUrl: string, body: unknown, forwardedFor?: string): Promise<Response> {
  return fetch(`${baseUrl}/pair`, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      ...(forwardedFor ? { "x-forwarded-for": forwardedFor } : {})
    },
    body: JSON.stringify(body)
  });
}

function sequence(values: number[]): () => number {
  return () => {
    const value = values.shift();
    if (value === undefined) {
      throw new Error("Pairing test code sequence exhausted");
    }
    return value;
  };
}
