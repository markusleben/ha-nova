import { createServer, type IncomingMessage, type RequestListener, type Server, type ServerResponse } from "node:http";

import { authorizeRequest } from "../security/auth.js";
import type { Principal } from "../security/principal.js";
import { invalidJson, invalidRequestUrl, payloadTooLarge, toErrorResponse } from "./errors.js";
import type { Router } from "./router.js";

export const DEFAULT_MAX_JSON_BODY_BYTES = 1_048_576;
export const DEFAULT_MAX_FORM_BODY_BYTES = 16 * 1024;

// A client must send headers and body within these windows; a stalled or
// drip-feeding connection is dropped instead of holding a socket forever.
// Upstream HA calls are bounded at 10 s, so 30 s leaves generous margin.
export const SERVER_REQUEST_TIMEOUT_MS = 30_000;
export const SERVER_HEADERS_TIMEOUT_MS = 31_000;

// Lets CLI callers of /ws and /core detect an outdated relay without an
// extra /health round-trip; /health remains the full health signal.
export const RELAY_VERSION_HEADER = "x-ha-nova-relay-version";

export interface RelayLogger {
  info?(message: string, context?: Record<string, unknown>): void;
  warn(message: string, context?: Record<string, unknown>): void;
  error(message: string, context?: Record<string, unknown>): void;
}

// The result of a listener's auth hook: ok (optionally carrying a resolved
// principal for the handler) or a failure to render. Failures may add headers
// (e.g. retry-after) before the envelope is sent.
export type AuthOutcome =
  | { ok: true; principal?: Principal }
  | { ok: false; status: number; code: string; message: string; headers?: Record<string, string> };

export type BodyPolicy = { type: "json" | "form" | "none"; maxBytes: number };

export interface RequestListenerOptions {
  router: Router;
  version?: string;
  noStorePaths?: ReadonlySet<string>;
  logger?: RelayLogger;
  // Per-request authorization for this listener. The closure captures the
  // listener's transport (secure/plain/ingress), so the core stays transport-
  // agnostic. Returns ok (with an optional principal) or a failure to render.
  authorize: (request: IncomingMessage, routeKey: string) => AuthOutcome | Promise<AuthOutcome>;
  // Per-route body handling; defaults to JSON at the standard limit.
  bodyPolicy?: (routeKey: string) => BodyPolicy;
}

// The extracted request-handling pipeline shared by every listener: version
// header, no-store, pluggable auth, per-route body parsing, dispatch, and the
// standard {ok,data}/{ok,error} envelope.
export function createRequestListener(options: RequestListenerOptions): RequestListener {
  return async (request, response) => {
    const method = request.method?.toUpperCase() ?? "GET";
    let path = "<invalid>";
    if (options.version) {
      response.setHeader(RELAY_VERSION_HEADER, options.version);
    }
    try {
      path = toPathname(request.url);
      if (options.noStorePaths?.has(path)) {
        response.setHeader("cache-control", "no-store");
      }
      const routeKey = `${method} ${path}`;

      const auth = await options.authorize(request, routeKey);
      if (!auth.ok) {
        for (const [name, value] of Object.entries(auth.headers ?? {})) {
          response.setHeader(name, value);
        }
        options.logger?.warn("Rejected unauthorized request", {
          method,
          path,
          remote: request.socket.remoteAddress ?? "unknown",
        });
        writeJson(response, auth.status, { ok: false, error: { code: auth.code, message: auth.message } });
        return;
      }

      const policy = options.bodyPolicy?.(routeKey) ?? { type: "json", maxBytes: DEFAULT_MAX_JSON_BODY_BYTES };
      const body = await parseBody(request, policy);
      const data = await options.router.dispatch(method, path, {
        request,
        response,
        path,
        body,
        ...(auth.principal ? { principal: auth.principal } : {}),
      });

      if (response.writableEnded) {
        return;
      }
      writeJson(response, 200, { ok: true, data: data ?? null });
    } catch (error) {
      const mapped = toErrorResponse(error);
      if (mapped.status === 500) {
        options.logger?.error("Unhandled relay error", {
          method,
          path,
          error: error instanceof Error ? `${error.name}: ${error.message}` : String(error),
        });
      }
      writeJson(response, mapped.status, mapped.body);
    }
  };
}

export interface HttpServerOptions {
  authToken: string;
  router: Router;
  maxJsonBodyBytes?: number;
  version?: string;
  bearerExemptRoutes?: ReadonlySet<string>;
  noStorePaths?: ReadonlySet<string>;
  logger?: RelayLogger;
}

// Backward-compatible single-token server: the historical auth model expressed
// as one listener over createRequestListener.
export function createHttpServer(options: HttpServerOptions): Server {
  const maxJsonBodyBytes = options.maxJsonBodyBytes ?? DEFAULT_MAX_JSON_BODY_BYTES;
  const listener = createRequestListener({
    router: options.router,
    ...(options.version !== undefined ? { version: options.version } : {}),
    ...(options.noStorePaths ? { noStorePaths: options.noStorePaths } : {}),
    ...(options.logger ? { logger: options.logger } : {}),
    authorize: (request, routeKey) => {
      if (options.bearerExemptRoutes?.has(routeKey)) {
        return { ok: true };
      }
      const result = authorizeRequest(request.headers.authorization, options.authToken);
      return result.ok ? { ok: true } : { ok: false, status: result.status, code: result.code, message: result.message };
    },
    bodyPolicy: () => ({ type: "json", maxBytes: maxJsonBodyBytes }),
  });

  const server = createServer(listener);
  server.requestTimeout = SERVER_REQUEST_TIMEOUT_MS;
  server.headersTimeout = SERVER_HEADERS_TIMEOUT_MS;
  return server;
}

function toPathname(urlValue: string | undefined): string {
  if (!urlValue) {
    return "/";
  }
  try {
    return new URL(urlValue, "http://localhost").pathname;
  } catch {
    throw invalidRequestUrl();
  }
}

async function parseBody(request: IncomingMessage, policy: BodyPolicy): Promise<unknown> {
  const method = request.method?.toUpperCase() ?? "GET";
  if (method === "GET" || method === "HEAD" || policy.type === "none") {
    return null;
  }
  const rawBody = await readBody(request, policy.maxBytes);
  if (!rawBody) {
    return null;
  }
  if (policy.type === "form") {
    return parseForm(request, rawBody);
  }
  try {
    return JSON.parse(rawBody) as unknown;
  } catch {
    throw invalidJson();
  }
}

function parseForm(request: IncomingMessage, rawBody: string): Record<string, string> {
  const contentType = request.headers["content-type"] ?? "";
  if (!contentType.includes("application/x-www-form-urlencoded")) {
    throw invalidJson();
  }
  const params = new URLSearchParams(rawBody);
  const out: Record<string, string> = {};
  for (const [key, value] of params) {
    out[key] = value;
  }
  return out;
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
