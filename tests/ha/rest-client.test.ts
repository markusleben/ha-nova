import { afterEach, describe, expect, it, vi } from "vitest";

import { HaRestClientError, createHaRestClient } from "../../src/ha/rest-client.js";

describe("ha rest client", () => {
  afterEach(() => {
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
      message: "network down"
    } satisfies Partial<HaRestClientError>);
  });
});
