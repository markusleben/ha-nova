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
}

const DEFAULT_REQUEST_TIMEOUT_MS = 10_000;

export function createHaRestClient(options: HaRestClientOptions): HaRestClient {
  const baseUrl = options.baseUrl.endsWith("/") ? options.baseUrl.slice(0, -1) : options.baseUrl;
  const requestTimeoutMs = options.requestTimeoutMs ?? DEFAULT_REQUEST_TIMEOUT_MS;

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
              body: await parseResponseBody(response)
            };
          })(),
          requestTimeoutMs,
          () => abortController.abort()
        );
      } catch (error) {
        if (error instanceof TimeoutError) {
          throw new HaRestClientError("UPSTREAM_HTTP_TIMEOUT", `HTTP request timed out after ${requestTimeoutMs}ms`);
        }
        const message = error instanceof Error && error.message ? error.message : "Upstream HTTP request failed";
        throw new HaRestClientError("UPSTREAM_HTTP_ERROR", message);
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

async function parseResponseBody(response: Response): Promise<unknown> {
  const contentType = response.headers.get("content-type") ?? "";

  if (contentType.toLowerCase().includes("application/json")) {
    try {
      return (await response.json()) as unknown;
    } catch {
      return null;
    }
  }

  const text = await response.text();
  return text.length > 0 ? text : null;
}
