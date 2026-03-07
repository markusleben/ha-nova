import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";

import { authorizeRequest } from "../security/auth.js";
import { invalidJson, toErrorResponse } from "./errors.js";
import type { Router } from "./router.js";

export interface HttpServerOptions {
  authToken: string;
  router: Router;
}

export function createHttpServer(options: HttpServerOptions): Server {
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
      const body = await parseJsonBody(request);
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

async function parseJsonBody(request: IncomingMessage): Promise<unknown> {
  const method = request.method?.toUpperCase() ?? "GET";
  if (method === "GET" || method === "HEAD") {
    return null;
  }

  const rawBody = await readBody(request);
  if (!rawBody) {
    return null;
  }

  try {
    return JSON.parse(rawBody) as unknown;
  } catch {
    throw invalidJson();
  }
}

async function readBody(request: IncomingMessage): Promise<string> {
  const chunks: Buffer[] = [];

  for await (const chunk of request) {
    chunks.push(typeof chunk === "string" ? Buffer.from(chunk) : chunk);
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
