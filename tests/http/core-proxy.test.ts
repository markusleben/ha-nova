import { afterEach, describe, expect, it } from "vitest";

import { createCoreProxyHandler } from "../../src/http/handlers/core-proxy.js";
import { createRouter } from "../../src/http/router.js";
import { createHttpServer } from "../../src/http/server.js";

const TEST_AUTH_TOKEN = "secret";

describe("core proxy endpoint", () => {
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

  it("forwards config request and returns upstream payload", async () => {
    const router = createRouter();

    router.register(
      "POST",
      "/core",
      createCoreProxyHandler({
        coreClient: {
          request: async (input) => ({
            status: 201,
            body: {
              method: input.method,
              path: input.path
            }
          })
        }
      })
    );

    const { baseUrl } = await startServer(servers, router);
    const response = await fetch(`${baseUrl}/core`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json"
      },
      body: JSON.stringify({
        method: "POST",
        path: "/api/config/automation/config/test_automation",
        body: {
          alias: "test"
        }
      })
    });

    expect(response.status).toBe(200);
    await expect(response.json()).resolves.toEqual({
      ok: true,
      data: {
        status: 201,
        body: {
          method: "POST",
          path: "/api/config/automation/config/test_automation"
        }
      }
    });
  });

  it("forwards non-crud /api path without allowlist filter", async () => {
    const router = createRouter();

    router.register(
      "POST",
      "/core",
      createCoreProxyHandler({
        coreClient: {
          request: async (input) => ({
            status: 200,
            body: {
              method: input.method,
              path: input.path
            }
          })
        }
      })
    );

    const { baseUrl } = await startServer(servers, router);
    const response = await fetch(`${baseUrl}/core`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json"
      },
      body: JSON.stringify({
        method: "GET",
        path: "/api/config"
      })
    });

    expect(response.status).toBe(200);
    await expect(response.json()).resolves.toEqual({
      ok: true,
      data: {
        status: 200,
        body: {
          method: "GET",
          path: "/api/config"
        }
      }
    });
  });

  it("returns 400 for absolute url path", async () => {
    const router = createRouter();

    router.register(
      "POST",
      "/core",
      createCoreProxyHandler({
        coreClient: {
          request: async () => ({
            status: 200,
            body: {}
          })
        }
      })
    );

    const { baseUrl } = await startServer(servers, router);
    const response = await fetch(`${baseUrl}/core`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json"
      },
      body: JSON.stringify({
        method: "GET",
        path: "http://malicious.local/api/states"
      })
    });

    expect(response.status).toBe(400);
    await expect(response.json()).resolves.toEqual({
      ok: false,
      error: {
        code: "CORE_PATH_INVALID",
        message: "Core request path is invalid"
      }
    });
  });

  it("returns 400 for double-encoded traversal token", async () => {
    const router = createRouter();

    router.register(
      "POST",
      "/core",
      createCoreProxyHandler({
        coreClient: {
          request: async () => ({
            status: 200,
            body: {}
          })
        }
      })
    );

    const { baseUrl } = await startServer(servers, router);
    const response = await fetch(`${baseUrl}/core`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json"
      },
      body: JSON.stringify({
        method: "GET",
        path: "/api/%252e%252e/config"
      })
    });

    expect(response.status).toBe(400);
    await expect(response.json()).resolves.toEqual({
      ok: false,
      error: {
        code: "CORE_PATH_INVALID",
        message: "Core request path is invalid"
      }
    });
  });

  it("returns 400 for double-encoded slash token", async () => {
    const router = createRouter();

    router.register(
      "POST",
      "/core",
      createCoreProxyHandler({
        coreClient: {
          request: async () => ({
            status: 200,
            body: {}
          })
        }
      })
    );

    const { baseUrl } = await startServer(servers, router);
    const response = await fetch(`${baseUrl}/core`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json"
      },
      body: JSON.stringify({
        method: "GET",
        path: "/api/%252fstates"
      })
    });

    expect(response.status).toBe(400);
    await expect(response.json()).resolves.toEqual({
      ok: false,
      error: {
        code: "CORE_PATH_INVALID",
        message: "Core request path is invalid"
      }
    });
  });

  it("returns 400 for double-encoded backslash token", async () => {
    const router = createRouter();

    router.register(
      "POST",
      "/core",
      createCoreProxyHandler({
        coreClient: {
          request: async () => ({
            status: 200,
            body: {}
          })
        }
      })
    );

    const { baseUrl } = await startServer(servers, router);
    const response = await fetch(`${baseUrl}/core`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json"
      },
      body: JSON.stringify({
        method: "GET",
        path: "/api/%255cstates"
      })
    });

    expect(response.status).toBe(400);
    await expect(response.json()).resolves.toEqual({
      ok: false,
      error: {
        code: "CORE_PATH_INVALID",
        message: "Core request path is invalid"
      }
    });
  });

  it("maps upstream failure to 502", async () => {
    const router = createRouter();

    router.register(
      "POST",
      "/core",
      createCoreProxyHandler({
        coreClient: {
          request: async () => {
            throw new Error("upstream unavailable");
          }
        }
      })
    );

    const { baseUrl } = await startServer(servers, router);
    const response = await fetch(`${baseUrl}/core`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json"
      },
      body: JSON.stringify({
        method: "GET",
        path: "/api/states"
      })
    });

    expect(response.status).toBe(502);
    await expect(response.json()).resolves.toEqual({
      ok: false,
      error: {
        code: "UPSTREAM_HTTP_ERROR",
        message: "upstream unavailable"
      }
    });
  });
});

async function startServer(
  servers: Array<ReturnType<typeof createHttpServer>>,
  router: ReturnType<typeof createRouter>
): Promise<{ baseUrl: string }> {
  const server = createHttpServer({
    authToken: TEST_AUTH_TOKEN,
    router
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
