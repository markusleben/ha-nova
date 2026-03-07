import type { CoreProxyRequest, CoreProxyResponse } from "../types/api.js";

export class HaRestClientError extends Error {
  public readonly code: string;

  public constructor(code: string, message: string) {
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
}

export function createHaRestClient(options: HaRestClientOptions): HaRestClient {
  const baseUrl = options.baseUrl.endsWith("/") ? options.baseUrl.slice(0, -1) : options.baseUrl;

  return {
    async request(input: CoreProxyRequest): Promise<CoreProxyResponse> {
      const url = `${baseUrl}${input.path}`;
      const method = input.method;

      try {
        const init: RequestInit = {
          method,
          headers: buildHeaders(options.token, method)
        };
        if (method === "POST") {
          init.body = JSON.stringify(input.body ?? {});
        }

        const response = await fetch(url, init);

        return {
          status: response.status,
          body: await parseResponseBody(response)
        };
      } catch (error) {
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
