import { afterEach, describe, expect, it, vi } from "vitest";

import { HaRestClientError, createHaRestClient } from "../../nova/src/ha/rest-client.js";

describe("ha rest client", () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("forwards GET request without request body", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      expect(String(input)).toBe("http://ha.local/api/states");
      expect(init?.method).toBe("GET");
      expect(init?.body).toBeUndefined();
      expect(new Headers(init?.headers).get("authorization")).toBe("Bearer upstream-token");
      expect(new Headers(init?.headers).get("content-type")).toBeNull();

      return new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: {
          "content-type": "application/json"
        }
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    const client = createHaRestClient({
      baseUrl: "http://ha.local",
      token: "upstream-token"
    });

    const response = await client.request({
      method: "GET",
      path: "/api/states"
    });

    expect(response).toEqual({
      status: 200,
      body: { ok: true }
    });
  });

  it("forwards POST request with json body and parses text response", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      expect(init?.method).toBe("POST");
      expect(init?.body).toBe(JSON.stringify({ alias: "test" }));
      expect(new Headers(init?.headers).get("content-type")).toBe("application/json");

      return new Response("created", {
        status: 201,
        headers: {
          "content-type": "text/plain"
        }
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    const client = createHaRestClient({
      baseUrl: "http://ha.local/",
      token: "upstream-token"
    });

    const response = await client.request({
      method: "POST",
      path: "/api/config/automation/config/test_id",
      body: { alias: "test" }
    });

    expect(response).toEqual({
      status: 201,
      body: "created"
    });
  });

  it("returns null body on invalid upstream json payload", async () => {
    const fetchMock = vi.fn(async () => {
      return new Response("not-json", {
        status: 200,
        headers: {
          "content-type": "application/json"
        }
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    const client = createHaRestClient({
      baseUrl: "http://ha.local",
      token: "upstream-token"
    });

    const response = await client.request({
      method: "GET",
      path: "/api/states"
    });

    expect(response).toEqual({
      status: 200,
      body: null
    });
  });

  it("maps network errors to HaRestClientError", async () => {
    const fetchMock = vi.fn(async () => {
      throw new Error("network down");
    });
    vi.stubGlobal("fetch", fetchMock);

    const client = createHaRestClient({
      baseUrl: "http://ha.local",
      token: "upstream-token"
    });

    await expect(
      client.request({
        method: "GET",
        path: "/api/states"
      })
    ).rejects.toMatchObject({
      code: "UPSTREAM_HTTP_ERROR",
      // The raw error stays first; the appended hint tells the agent what to check.
      message: "network down — check that Home Assistant is running and reachable from the NOVA Relay App"
    } satisfies Partial<HaRestClientError>);
  });

  it("rejects upstream responses above the configured size ceiling", async () => {
    const fetchMock = vi.fn(async () => {
      return new Response("x".repeat(64), {
        status: 200,
        headers: {
          "content-type": "text/plain"
        }
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    const client = createHaRestClient({
      baseUrl: "http://ha.local",
      token: "upstream-token",
      maxResponseBytes: 32
    });

    await expect(
      client.request({
        method: "GET",
        path: "/api/states"
      })
    ).rejects.toMatchObject({
      code: "UPSTREAM_HTTP_ERROR",
      message: "HA response exceeded the 32-byte relay limit — narrow the request (filter, pagination, or a more specific path)"
    } satisfies Partial<HaRestClientError>);
  });

  it("accepts upstream responses exactly at the size ceiling", async () => {
    const fetchMock = vi.fn(async () => {
      return new Response("x".repeat(32), {
        status: 200,
        headers: {
          "content-type": "text/plain"
        }
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    const client = createHaRestClient({
      baseUrl: "http://ha.local",
      token: "upstream-token",
      maxResponseBytes: 32
    });

    const response = await client.request({
      method: "GET",
      path: "/api/states"
    });

    expect(response).toEqual({
      status: 200,
      body: "x".repeat(32)
    });
  });

  it("maps stalled upstream requests to HaRestClientError timeout", async () => {
    vi.useFakeTimers();
    const fetchMock = vi.fn(async () => await new Promise<Response>(() => {}));
    vi.stubGlobal("fetch", fetchMock);

    const client = createHaRestClient({
      baseUrl: "http://ha.local",
      token: "upstream-token",
      requestTimeoutMs: 10
    });

    const pending = client.request({
      method: "GET",
      path: "/api/states"
    });
    const expectation = expect(pending).rejects.toMatchObject({
      code: "UPSTREAM_HTTP_TIMEOUT",
      message: "HTTP request timed out after 10ms"
    } satisfies Partial<HaRestClientError>);

    await vi.advanceTimersByTimeAsync(11);

    await expectation;
  });
});
