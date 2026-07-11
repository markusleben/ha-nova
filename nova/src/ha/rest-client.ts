import type { CoreProxyRequest, CoreProxyResponse } from "../types/api.js";
import { TimeoutError, withTimeout } from "../shared/timeout.js";

export type HaRestClientErrorCode = "UPSTREAM_HTTP_ERROR" | "UPSTREAM_HTTP_TIMEOUT";

export class HaRestClientError extends Error {
  public readonly code: HaRestClientErrorCode;

  public constructor(code: HaRestClientErrorCode, message: string) {
    super(message);
    this.code = code;
  }
}

export interface HaRestClient {
  request(input: CoreProxyRequest): Promise<CoreProxyResponse>;
}

export interface HaRestClientOptions {
  baseUrl: string;
  token: string;
  requestTimeoutMs?: number;
  maxResponseBytes?: number;
}

const DEFAULT_REQUEST_TIMEOUT_MS = 10_000;
// Mirrors the WS payload ceiling (nova/src/ha/socket.ts) so both proxy paths
// share one bound instead of the REST path buffering unbounded HA responses.
const DEFAULT_MAX_RESPONSE_BYTES = 256 * 1024 * 1024;
// Binary bodies (camera snapshots, downloads) are returned base64-encoded,
// which inflates them by ~33% and has to be held as a JS string. They get a
// much smaller ceiling than the text/JSON path: a single frame is far below
// this, and anything larger is a streaming use case the relay does not serve.
const DEFAULT_MAX_BINARY_RESPONSE_BYTES = 8 * 1024 * 1024;

export function createHaRestClient(options: HaRestClientOptions): HaRestClient {
  const baseUrl = options.baseUrl.endsWith("/") ? options.baseUrl.slice(0, -1) : options.baseUrl;
  const requestTimeoutMs = options.requestTimeoutMs ?? DEFAULT_REQUEST_TIMEOUT_MS;
  const maxResponseBytes = options.maxResponseBytes ?? DEFAULT_MAX_RESPONSE_BYTES;

  return {
    async request(input: CoreProxyRequest): Promise<CoreProxyResponse> {
      const url = `${baseUrl}${input.path}`;
      const method = input.method;
      const abortController = new AbortController();

      try {
        const init: RequestInit = {
          method,
          headers: buildHeaders(options.token, method),
          signal: abortController.signal
        };
        if (method === "POST") {
          init.body = JSON.stringify(input.body ?? {});
        }

        return await withTimeout(
          (async () => {
            const response = await fetch(url, init);

            return {
              status: response.status,
              ...(await parseResponseBody(response, maxResponseBytes))
            };
          })(),
          requestTimeoutMs,
          () => abortController.abort()
        );
      } catch (error) {
        if (error instanceof TimeoutError) {
          throw new HaRestClientError("UPSTREAM_HTTP_TIMEOUT", `HTTP request timed out after ${requestTimeoutMs}ms`);
        }
        if (error instanceof HaRestClientError) {
          throw error;
        }
        const detail = error instanceof Error && error.message ? error.message : "Upstream HTTP request failed";
        throw new HaRestClientError(
          "UPSTREAM_HTTP_ERROR",
          `${detail} — check that Home Assistant is running and reachable from the NOVA Relay App`
        );
      }
    }
  };
}

function buildHeaders(token: string, method: CoreProxyRequest["method"]): Headers {
  const headers = new Headers({
    authorization: `Bearer ${token}`
  });

  if (method === "POST") {
    headers.set("content-type", "application/json");
  }

  return headers;
}

type ParsedBody = Pick<CoreProxyResponse, "body" | "body_encoding" | "content_type">;

async function parseResponseBody(response: Response, maxBytes: number): Promise<ParsedBody> {
  const contentType = response.headers.get("content-type") ?? "";
  const normalizedType = contentType.toLowerCase();

  if (isBinaryContentType(normalizedType)) {
    // Dumb transport: a binary body is forwarded as honest bytes. Decoding it
    // as UTF-8 (the old path) silently corrupted every camera frame.
    const buffer = await readBodyBytesWithLimit(response, DEFAULT_MAX_BINARY_RESPONSE_BYTES);
    if (buffer.byteLength === 0) {
      return { body: null };
    }
    return {
      body: buffer.toString("base64"),
      body_encoding: "base64",
      content_type: contentType
    };
  }

  const text = (await readBodyBytesWithLimit(response, maxBytes)).toString("utf8");

  if (normalizedType.includes("application/json")) {
    try {
      return { body: JSON.parse(text) as unknown };
    } catch {
      return { body: null };
    }
  }

  return { body: text.length > 0 ? text : null };
}

// JSON and text stay on the text path (that includes HA's plain-text
// /api/error_log and every JSON API); everything else is treated as bytes.
function isBinaryContentType(normalizedContentType: string): boolean {
  if (normalizedContentType === "") {
    return false;
  }
  return !(
    normalizedContentType.includes("json") ||
    normalizedContentType.startsWith("text/") ||
    normalizedContentType.includes("xml") ||
    normalizedContentType.includes("x-www-form-urlencoded")
  );
}

async function readBodyBytesWithLimit(response: Response, maxBytes: number): Promise<Buffer> {
  if (!response.body) {
    return Buffer.alloc(0);
  }

  const chunks: Uint8Array[] = [];
  let totalBytes = 0;
  const reader = response.body.getReader();
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) {
        break;
      }
      totalBytes += value.byteLength;
      if (totalBytes > maxBytes) {
        // Cancel so undici tears down the upstream socket instead of keeping
        // it open until HA finishes sending the oversized body.
        await reader.cancel().catch(() => undefined);
        throw new HaRestClientError(
          "UPSTREAM_HTTP_ERROR",
          `HA response exceeded the ${maxBytes}-byte relay limit — narrow the request (filter, pagination, or a more specific path)`
        );
      }
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }

  return Buffer.concat(chunks);
}
