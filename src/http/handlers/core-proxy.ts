import { HaRestClientError } from "../../ha/rest-client.js";
import type { CoreProxyMethod, CoreProxyRequest, CoreProxyResponse } from "../../types/api.js";
import { HttpError } from "../errors.js";
import type { RouteContext, RouteHandler } from "../router.js";

export interface CoreProxyHandlerOptions {
  coreClient: {
    request(input: CoreProxyRequest): Promise<CoreProxyResponse>;
  };
}

export function createCoreProxyHandler(options: CoreProxyHandlerOptions): RouteHandler {
  return async ({ body }: RouteContext) => {
    const request = parseCoreRequestBody(body);
    const normalizedPath = normalizeCorePath(request.path);

    try {
      return await options.coreClient.request({
        method: request.method,
        path: normalizedPath,
        body: request.body
      });
    } catch (error) {
      if (error instanceof HaRestClientError) {
        throw new HttpError(502, error.code, error.message);
      }

      const message = error instanceof Error && error.message ? error.message : "Core upstream request failed";
      throw new HttpError(502, "UPSTREAM_HTTP_ERROR", message);
    }
  };
}

function parseCoreRequestBody(body: unknown): CoreProxyRequest {
  if (!body || typeof body !== "object") {
    throw validationError();
  }

  const methodRawValue = (body as { method?: unknown }).method;
  const pathValue = (body as { path?: unknown }).path;
  const bodyValue = (body as { body?: unknown }).body;
  const methodValue = typeof methodRawValue === "string" ? methodRawValue.toUpperCase() : methodRawValue;

  if (!isCoreMethod(methodValue) || typeof pathValue !== "string" || pathValue.trim().length === 0) {
    throw validationError();
  }

  return {
    method: methodValue,
    path: pathValue,
    body: bodyValue
  };
}

function normalizeCorePath(pathValue: string): string {
  if (pathValue.length > 2048) {
    throw new HttpError(400, "CORE_PATH_INVALID", "Core request path is invalid");
  }

  if (/^\w+:\/\//i.test(pathValue)) {
    throw new HttpError(400, "CORE_PATH_INVALID", "Core request path is invalid");
  }

  const parsed = safeParsePath(pathValue);
  if (parsed.origin !== "http://relay.local") {
    throw new HttpError(400, "CORE_PATH_INVALID", "Core request path is invalid");
  }

  if (!parsed.pathname.startsWith("/api/")) {
    throw new HttpError(400, "CORE_PATH_INVALID", "Core request path is invalid");
  }

  const rawPath = parsed.pathname;
  if (/%2f|%5c/i.test(rawPath)) {
    throw new HttpError(400, "CORE_PATH_INVALID", "Core request path is invalid");
  }

  const decodedPath = safeDecode(rawPath);
  if (decodedPath.includes("..") || decodedPath.includes("\\")) {
    throw new HttpError(400, "CORE_PATH_INVALID", "Core request path is invalid");
  }

  if (containsControlChars(decodedPath)) {
    throw new HttpError(400, "CORE_PATH_INVALID", "Core request path is invalid");
  }

  return `${decodedPath}${parsed.search}`;
}

function safeParsePath(pathValue: string): URL {
  try {
    return new URL(pathValue, "http://relay.local");
  } catch {
    throw new HttpError(400, "CORE_PATH_INVALID", "Core request path is invalid");
  }
}

function safeDecode(pathValue: string): string {
  try {
    return decodeURIComponent(pathValue);
  } catch {
    throw new HttpError(400, "CORE_PATH_INVALID", "Core request path is invalid");
  }
}

function containsControlChars(value: string): boolean {
  return /[\u0000-\u001F\u007F]/.test(value);
}

function isCoreMethod(value: unknown): value is CoreProxyMethod {
  return value === "GET" || value === "POST" || value === "DELETE";
}

function validationError(): HttpError {
  return new HttpError(
    400,
    "VALIDATION_ERROR",
    "Request body must contain: method ('GET'|'POST'|'DELETE') and path ('/api/...')"
  );
}
