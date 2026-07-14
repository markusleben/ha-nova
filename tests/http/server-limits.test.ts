import { afterEach, describe, expect, it } from "vitest";

import { createRouter } from "../../nova/src/http/router.js";
import {
  createHttpServer,
  SERVER_HEADERS_TIMEOUT_MS,
  SERVER_REQUEST_TIMEOUT_MS
} from "../../nova/src/http/server.js";
import { levelAtLeast } from "../../nova/src/runtime/start.js";

const TEST_AUTH_TOKEN = "secret";

type LoggedLine = { level: string; message: string; context?: Record<string, unknown> };

describe("http server limits and logging", () => {
  const servers: Array<ReturnType<typeof createHttpServer>> = [];

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

  async function start(options: {
    maxJsonBodyBytes?: number;
    logged?: LoggedLine[];
    handler?: () => unknown;
  }): Promise<string> {
    const router = createRouter();
    router.register("POST", "/echo", options.handler ?? (({ body }) => body));
    const logged = options.logged;
    const server = createHttpServer({
      authToken: TEST_AUTH_TOKEN,
      router,
      ...(options.maxJsonBodyBytes ? { maxJsonBodyBytes: options.maxJsonBodyBytes } : {}),
      ...(logged
        ? {
            logger: {
              warn: (message: string, context?: Record<string, unknown>) =>
                logged.push({ level: "warn", message, ...(context ? { context } : {}) }),
              error: (message: string, context?: Record<string, unknown>) =>
                logged.push({ level: "error", message, ...(context ? { context } : {}) })
            }
          }
        : {})
    });
    servers.push(server);
    const port = await new Promise<number>((resolve, reject) => {
      server.listen(0, "127.0.0.1", () => {
        const address = server.address();
        if (!address || typeof address === "string") {
          reject(new Error("no tcp address"));
          return;
        }
        resolve(address.port);
      });
      server.on("error", reject);
    });
    return `http://127.0.0.1:${port}`;
  }

  it("rejects an oversized body with 413 before dispatch", async () => {
    const baseUrl = await start({ maxJsonBodyBytes: 64 });
    const response = await fetch(`${baseUrl}/echo`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json"
      },
      body: JSON.stringify({ filler: "x".repeat(256) })
    });
    expect(response.status).toBe(413);
    const payload = (await response.json()) as { ok: boolean; error: { code: string } };
    expect(payload.ok).toBe(false);
    expect(payload.error.code).toBe("PAYLOAD_TOO_LARGE");
  });

  it("logs rejected 401s with method, path, and remote", async () => {
    const logged: LoggedLine[] = [];
    const baseUrl = await start({ logged });
    const response = await fetch(`${baseUrl}/echo`, {
      method: "POST",
      headers: { authorization: "Bearer wrong" },
      body: "{}"
    });
    expect(response.status).toBe(401);
    expect(logged).toHaveLength(1);
    expect(logged[0]?.level).toBe("warn");
    expect(logged[0]?.context).toMatchObject({ method: "POST", path: "/echo" });
    expect(logged[0]?.context?.remote).toBeTruthy();
  });

  it("logs the cause of an unhandled 500 while keeping the envelope generic", async () => {
    const logged: LoggedLine[] = [];
    const baseUrl = await start({
      logged,
      handler: () => {
        throw new TypeError("boom from the handler");
      }
    });
    const response = await fetch(`${baseUrl}/echo`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json"
      },
      body: "{}"
    });
    expect(response.status).toBe(500);
    const payload = (await response.json()) as { error: { code: string; message: string } };
    expect(payload.error.code).toBe("INTERNAL_ERROR");
    expect(payload.error.message).toBe("Internal server error");
    expect(logged).toHaveLength(1);
    expect(logged[0]?.level).toBe("error");
    expect(logged[0]?.context?.error).toContain("boom from the handler");
  });

  it("sets explicit request and headers timeouts", async () => {
    const router = createRouter();
    const server = createHttpServer({ authToken: TEST_AUTH_TOKEN, router });
    servers.push(server);
    expect(server.requestTimeout).toBe(SERVER_REQUEST_TIMEOUT_MS);
    expect(server.headersTimeout).toBe(SERVER_HEADERS_TIMEOUT_MS);
  });

  it("orders log levels for the LOG_LEVEL filter", () => {
    expect(levelAtLeast("error", "info")).toBe(true);
    expect(levelAtLeast("info", "info")).toBe(true);
    expect(levelAtLeast("debug", "info")).toBe(false);
    expect(levelAtLeast("trace", "warn")).toBe(false);
    expect(levelAtLeast("warn", "trace")).toBe(true);
  });
});
