import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";

import { authorizeRequest } from "../security/auth.js";
import { invalidJson, payloadTooLarge, toErrorResponse } from "./errors.js";
import type { Router } from "./router.js";

export const DEFAULT_MAX_JSON_BODY_BYTES = 1_048_576;

export interface HttpServerOptions {
  authToken: string;
  router: Router;
  maxJsonBodyBytes?: number;
}

export function createHttpServer(options: HttpServerOptions): Server {
  const maxJsonBodyBytes = options.maxJsonBodyBytes ?? DEFAULT_MAX_JSON_BODY_BYTES;

  return createServer(async (request, response) => {
    try {
      const authResult = authorizeRequest(request.headers.authorization, options.authToken);
      if (!authResult.ok) {
        writeJson(response, authResult.status, {
          ok: false,
          error: {
            code: authResult.code,
            message: authResult.message
          }
        });
        return;
      }

      const method = request.method?.toUpperCase() ?? "GET";
      const path = toPathname(request.url);
      const body = await parseJsonBody(request, maxJsonBodyBytes);
      const data = await options.router.dispatch(method, path, {
        request,
        path,
        body
      });

      writeJson(response, 200, {
        ok: true,
        data: data ?? null
      });
    } catch (error) {
      const mapped = toErrorResponse(error);
      writeJson(response, mapped.status, mapped.body);
    }
  });
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
