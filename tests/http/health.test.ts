import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { gzipSync } from "node:zlib";

import { afterEach, describe, expect, it } from "vitest";

import { createHealthHandler } from "../../nova/src/http/handlers/health.js";
import { createRouter } from "../../nova/src/http/router.js";
import { createHttpServer } from "../../nova/src/http/server.js";

const TEST_AUTH_TOKEN = "secret";

describe("health endpoint", () => {
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

  it("returns status payload with ws connectivity and uptime", async () => {
    const router = createRouter();
    router.register(
      "GET",
      "/health",
      createHealthHandler({
        version: "1.0.0",
        wsClient: { isConnected: () => true },
        startedAtMs: 1_000,
        fileAccessMode: "off",
        snapshotRoot: "/nonexistent-snapshot-root-for-tests",
        now: () => 4_500
      })
    );

    const { baseUrl } = await startServer(servers, router);
    const response = await fetch(`${baseUrl}/health`, {
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`
      }
    });

    expect(response.status).toBe(200);
    await expect(response.json()).resolves.toEqual({
      ok: true,
      data: {
        status: "ok",
        ha_ws_connected: true,
        ha_ws_disconnect_reason: null,
        version: "1.0.0",
        uptime_s: 3,
        file_access: "off",
        snapshots: { files: 0, bytes: 0 }
      }
    });
  });

  it("surfaces the disconnect reason and snapshot counters", async () => {
    const snapshotRoot = mkdtempSync(join(tmpdir(), "nova-health-snapshots-"));
    mkdirSync(join(snapshotRoot, "scenes"), { recursive: true });
    writeFileSync(join(snapshotRoot, "scenes", "movie-20260714T120000000Z.json.gz"), gzipSync("{}"));

    const router = createRouter();
    router.register(
      "GET",
      "/health",
      createHealthHandler({
        version: "1.0.0",
        wsClient: {
          isConnected: () => false,
          getConnectionStatus: () => ({ connected: false, disconnect_reason: "auth" })
        },
        startedAtMs: 1_000,
        fileAccessMode: "readwrite",
        snapshotRoot,
        now: () => 4_500
      })
    );

    try {
      const { baseUrl } = await startServer(servers, router);
      const response = await fetch(`${baseUrl}/health`, {
        headers: { authorization: `Bearer ${TEST_AUTH_TOKEN}` }
      });
      const payload = (await response.json()) as {
        data: {
          ha_ws_connected: boolean;
          ha_ws_disconnect_reason: string | null;
          file_access: string;
          snapshots: { files: number; bytes: number };
        };
      };
      expect(payload.data.ha_ws_connected).toBe(false);
      expect(payload.data.ha_ws_disconnect_reason).toBe("auth");
      expect(payload.data.file_access).toBe("readwrite");
      expect(payload.data.snapshots.files).toBe(1);
      expect(payload.data.snapshots.bytes).toBeGreaterThan(0);
    } finally {
      rmSync(snapshotRoot, { recursive: true, force: true });
    }
  });

  it("returns 401 without token", async () => {
    const router = createRouter();
    router.register(
      "GET",
      "/health",
      createHealthHandler({
        version: "1.0.0",
        wsClient: { isConnected: () => false },
        startedAtMs: 1_000,
        fileAccessMode: "off",
        snapshotRoot: "/nonexistent-snapshot-root-for-tests",
        now: () => 2_000
      })
    );

    const { baseUrl } = await startServer(servers, router);
    const response = await fetch(`${baseUrl}/health`);

    expect(response.status).toBe(401);
    await expect(response.json()).resolves.toEqual({
      ok: false,
      error: {
        code: "UNAUTHORIZED",
        message: "Missing authorization header"
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
