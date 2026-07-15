import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";

import { authorizeRequest } from "../security/auth.js";
import { invalidJson, payloadTooLarge, toErrorResponse } from "./errors.js";
import type { Router } from "./router.js";

export const DEFAULT_MAX_JSON_BODY_BYTES = 1_048_576;

export interface HttpServerOptions {
  authToken: string;
  router: Router;
  maxJsonBodyBytes?: number;
  version?: string;
  bearerExemptRoutes?: ReadonlySet<string>;
  noStorePaths?: ReadonlySet<string>;
  logger?: {
    warn(message: string, context?: Record<string, unknown>): void;
    error(message: string, context?: Record<string, unknown>): void;
  };
}

// A client must send headers and body within these windows; a stalled or
// drip-feeding connection is dropped instead of holding a socket forever.
// Upstream HA calls are bounded at 10 s, so 30 s leaves generous margin.
export const SERVER_REQUEST_TIMEOUT_MS = 30_000;
export const SERVER_HEADERS_TIMEOUT_MS = 31_000;

// Lets CLI callers of /ws and /core detect an outdated relay without an
// extra /health round-trip; /health remains the full health signal.
export const RELAY_VERSION_HEADER = "x-ha-nova-relay-version";

export function createHttpServer(options: HttpServerOptions): Server {
  const maxJsonBodyBytes = options.maxJsonBodyBytes ?? DEFAULT_MAX_JSON_BODY_BYTES;

  const server = createServer(async (request, response) => {
    const method = request.method?.toUpperCase() ?? "GET";
    const path = toPathname(request.url);
    if (options.noStorePaths?.has(path)) {
      response.setHeader("cache-control", "no-store");
    }
    if (options.version) {
      response.setHeader(RELAY_VERSION_HEADER, options.version);
    }
    try {
      const routeKey = `${method} ${path}`;
      const authResult = options.bearerExemptRoutes?.has(routeKey)
        ? { ok: true as const }
        : authorizeRequest(request.headers.authorization, options.authToken);
      if (!authResult.ok) {
        // Visible in the App log: repeated 401s are the operator's only signal
        // for a misconfigured client or someone probing the port.
        options.logger?.warn("Rejected unauthorized request", {
          method,
          path,
          remote: request.socket.remoteAddress ?? "unknown"
        });
        writeJson(response, authResult.status, {
          ok: false,
          error: {
            code: authResult.code,
            message: authResult.message
          }
        });
        return;
      }

      const body = await parseJsonBody(request, maxJsonBodyBytes);
      const data = await options.router.dispatch(method, path, {
        request,
        response,
        path,
        body
      });

      writeJson(response, 200, {
        ok: true,
        data: data ?? null
      });
    } catch (error) {
      const mapped = toErrorResponse(error);
      if (mapped.status === 500) {
        // The envelope stays generic on purpose; the App log carries the cause
        // so an unexpected crash path is diagnosable.
        options.logger?.error("Unhandled relay error", {
          method,
          path,
          error: error instanceof Error ? `${error.name}: ${error.message}` : String(error)
        });
      }
      writeJson(response, mapped.status, mapped.body);
    }
  });

  server.requestTimeout = SERVER_REQUEST_TIMEOUT_MS;
  server.headersTimeout = SERVER_HEADERS_TIMEOUT_MS;
  return server;
}

function toPathname(urlValue: string | undefined): string {
  if (!urlValue) {
    return "/";
  }

  return new URL(urlValue, "http://localhost").pathname;
}

async function parseJsonBody(request: IncomingMessage, maxBytes: number): Promise<unknown> {
  const method = request.method?.toUpperCase() ?? "GET";
  if (method === "GET" || method === "HEAD") {
    return null;
  }

  const rawBody = await readBody(request, maxBytes);
  if (!rawBody) {
    return null;
  }

  try {
    return JSON.parse(rawBody) as unknown;
  } catch {
    throw invalidJson();
  }
}

async function readBody(request: IncomingMessage, maxBytes: number): Promise<string> {
  const chunks: Buffer[] = [];
  let totalBytes = 0;

  for await (const chunk of request) {
    const buffer = typeof chunk === "string" ? Buffer.from(chunk) : chunk;
    totalBytes += buffer.byteLength;
    if (totalBytes > maxBytes) {
      throw payloadTooLarge(maxBytes);
    }
    chunks.push(buffer);
  }

  return Buffer.concat(chunks).toString("utf8");
}

function writeJson(response: ServerResponse, status: number, payload: unknown): void {
  const json = JSON.stringify(payload);
  response.statusCode = status;
  response.setHeader("content-type", "application/json; charset=utf-8");
  response.setHeader("content-length", Buffer.byteLength(json));
  response.end(json);
}
