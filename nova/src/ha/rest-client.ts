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
              body: await parseResponseBody(response, maxResponseBytes)
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

async function parseResponseBody(response: Response, maxBytes: number): Promise<unknown> {
  const contentType = response.headers.get("content-type") ?? "";
  const text = await readBodyWithLimit(response, maxBytes);

  if (contentType.toLowerCase().includes("application/json")) {
    try {
      return JSON.parse(text) as unknown;
    } catch {
      return null;
    }
  }

  return text.length > 0 ? text : null;
}

async function readBodyWithLimit(response: Response, maxBytes: number): Promise<string> {
  if (!response.body) {
    return "";
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

  return Buffer.concat(chunks).toString("utf8");
}
