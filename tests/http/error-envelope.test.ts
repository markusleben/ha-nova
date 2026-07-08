import { afterEach, describe, expect, it } from "vitest";

import { createRouter } from "../../nova/src/http/router.js";
import { createHttpServer } from "../../nova/src/http/server.js";

const TEST_AUTH_TOKEN = "secret";

describe("error envelope", () => {
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

  it("returns 404 for unknown route", async () => {
    const { baseUrl } = await startServer(servers, (router) => router);
    const response = await fetch(`${baseUrl}/missing`, {
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`
      }
    });

    expect(response.status).toBe(404);
    await expect(response.json()).resolves.toEqual({
      ok: false,
      error: {
        code: "NOT_FOUND",
        message: "Route not found: GET /missing"
      }
    });
  });

  it("returns 400 for malformed JSON", async () => {
    const { baseUrl } = await startServer(servers, (router) => {
      router.register("POST", "/ws", () => ({ ok: true }));
      return router;
    });

    const response = await fetch(`${baseUrl}/ws`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json"
      },
      body: "{invalid-json"
    });

    expect(response.status).toBe(400);
    await expect(response.json()).resolves.toEqual({
      ok: false,
      error: {
        code: "INVALID_JSON",
        message: "Request body is not valid JSON"
      }
    });
  });

  it("returns 500 for unhandled internal errors", async () => {
    const { baseUrl } = await startServer(servers, (router) => {
      router.register("GET", "/health", () => {
        throw new Error("boom");
      });
      return router;
    });

    const response = await fetch(`${baseUrl}/health`, {
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`
      }
    });

    expect(response.status).toBe(500);
    await expect(response.json()).resolves.toEqual({
      ok: false,
      error: {
        code: "INTERNAL_ERROR",
        message: "Internal server error"
      }
    });
  });

  it("stamps the relay version header on success and error responses", async () => {
    const { baseUrl } = await startServer(
      servers,
      (router) => {
        router.register("GET", "/health", () => ({ status: "ok" }));
        return router;
      },
      "0.2.6"
    );

    const success = await fetch(`${baseUrl}/health`, {
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`
      }
    });
    expect(success.status).toBe(200);
    expect(success.headers.get("x-ha-nova-relay-version")).toBe("0.2.6");

    // Error paths (including auth failures) carry the header too, so the CLI
    // can always evaluate relay freshness.
    const unauthorized = await fetch(`${baseUrl}/health`);
    expect(unauthorized.status).toBe(401);
    expect(unauthorized.headers.get("x-ha-nova-relay-version")).toBe("0.2.6");
  });
});

async function startServer(
  servers: Array<ReturnType<typeof createHttpServer>>,
  configure: (router: ReturnType<typeof createRouter>) => ReturnType<typeof createRouter>,
  version?: string
): Promise<{ baseUrl: string }> {
  const router = configure(createRouter());
  const server = createHttpServer({
    authToken: TEST_AUTH_TOKEN,
    router,
    ...(version !== undefined ? { version } : {})
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
