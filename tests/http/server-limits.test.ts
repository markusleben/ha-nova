import { createConnection } from "node:net";

import { afterEach, describe, expect, it } from "vitest";

import { createRouter, type RouteHandler } from "../../nova/src/http/router.js";
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
    handler?: RouteHandler;
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

  it("rejects invalid UTF-8 before JSON parsing or route dispatch", async () => {
    let dispatches = 0;
    const baseUrl = await start({
      handler: ({ body }) => {
        dispatches += 1;
        return body;
      }
    });
    const body = Buffer.concat([
      Buffer.from('{"title":"', "utf8"),
      Buffer.from([0xdc]),
      Buffer.from('bersicht"}', "utf8")
    ]);

    const response = await fetch(`${baseUrl}/echo`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json"
      },
      body
    });

    expect(response.status).toBe(400);
    await expect(response.json()).resolves.toMatchObject({
      ok: false,
      error: { code: "INVALID_UTF8" }
    });
    expect(dispatches).toBe(0);
  });

  it("accepts one UTF-8 BOM and preserves legitimate replacement characters", async () => {
    const baseUrl = await start({});
    const title = "Übersicht ☕ \uFFFD \uFEFF";
    const body = Buffer.concat([
      Buffer.from([0xef, 0xbb, 0xbf]),
      Buffer.from(JSON.stringify({ title }), "utf8")
    ]);

    const response = await fetch(`${baseUrl}/echo`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json"
      },
      body
    });

    expect(response.status).toBe(200);
    await expect(response.json()).resolves.toEqual({ ok: true, data: { title } });
  });

  it("distinguishes a genuinely empty body from a BOM-only JSON body", async () => {
    let dispatches = 0;
    const baseUrl = await start({
      handler: ({ body }) => {
        dispatches += 1;
        return body;
      }
    });
    const headers = {
      authorization: `Bearer ${TEST_AUTH_TOKEN}`,
      "content-type": "application/json"
    };

    const empty = await fetch(`${baseUrl}/echo`, { method: "POST", headers });
    expect(empty.status).toBe(200);
    await expect(empty.json()).resolves.toEqual({ ok: true, data: null });
    expect(dispatches).toBe(1);

    const bomOnly = await fetch(`${baseUrl}/echo`, {
      method: "POST",
      headers,
      body: Buffer.from([0xef, 0xbb, 0xbf])
    });
    expect(bomOnly.status).toBe(400);
    await expect(bomOnly.json()).resolves.toMatchObject({
      ok: false,
      error: { code: "INVALID_JSON" }
    });
    expect(dispatches).toBe(1);
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

  it("rejects a malformed absolute request target without escaping the error boundary", async () => {
    const baseUrl = await start({});
    const port = Number.parseInt(new URL(baseUrl).port, 10);

    const rawResponse = await sendRawRequest(
      port,
      "GET http://[ HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"
    );
    expect(rawResponse).toContain("HTTP/1.1 400 Bad Request");
    expect(rawResponse).toContain("INVALID_REQUEST_URL");

    const healthy = await fetch(`${baseUrl}/echo`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TEST_AUTH_TOKEN}`,
        "content-type": "application/json"
      },
      body: "{}"
    });
    expect(healthy.status).toBe(200);
  });

  it("does not append a JSON envelope after a handler ends a raw response", async () => {
    const baseUrl = await start({
      handler: ({ response }) => {
        response.statusCode = 200;
        response.setHeader("content-type", "text/plain");
        response.end("raw response");
      }
    });

    const response = await fetch(`${baseUrl}/echo`, {
      method: "POST",
      headers: { authorization: `Bearer ${TEST_AUTH_TOKEN}` }
    });
    expect(response.status).toBe(200);
    expect(response.headers.get("content-type")).toBe("text/plain");
    await expect(response.text()).resolves.toBe("raw response");
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

async function sendRawRequest(port: number, request: string): Promise<string> {
  return await new Promise<string>((resolve, reject) => {
    const chunks: Buffer[] = [];
    const socket = createConnection({ host: "127.0.0.1", port }, () => socket.end(request));
    socket.on("data", (chunk: Buffer) => chunks.push(chunk));
    socket.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    socket.on("error", reject);
  });
}
