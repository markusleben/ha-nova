import { afterEach, describe, expect, it, vi } from "vitest";

import { createSupervisorClient } from "../../nova/src/ha/supervisor-client.js";

// Shape mirrors a real Supervisor response captured live (hassio_role: default).
const infoBody = (network: Record<string, number | null>, update = false) => ({
  data: {
    version: "0.7.0",
    version_latest: update ? "0.7.1" : "0.7.0",
    update_available: update,
    ingress_port: 8791,
    network,
  },
});

function mockFetch(
  handler: (
    url: string,
    init?: RequestInit,
  ) => { status?: number; body?: unknown },
) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const { status = 200, body = null } = handler(url, init);
      return {
        ok: status >= 200 && status < 300,
        status,
        text: async () => (body === null ? "" : JSON.stringify(body)),
      } as Response;
    }),
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("supervisor-client", () => {
  it("reads update status from self/info", async () => {
    mockFetch(() => ({ body: infoBody({ "8791/tcp": 8791 }, true) }));
    const info = await createSupervisorClient("tok").getSelfInfo();
    expect(info.version).toBe("0.7.0");
    expect(info.versionLatest).toBe("0.7.1");
    expect(info.updateAvailable).toBe(true);
  });

  it("parses ingress_panel from self/info and defaults it to false when absent", async () => {
    const withPanel = infoBody({ "8791/tcp": 8791 });
    (withPanel.data as Record<string, unknown>).ingress_panel = true;
    mockFetch(() => ({ body: withPanel }));
    expect(
      (await createSupervisorClient("tok").getSelfInfo()).ingressPanel,
    ).toBe(true);
    // The captured live shape has no ingress_panel field — that must read as false.
    mockFetch(() => ({ body: infoBody({ "8791/tcp": 8791 }) }));
    expect(
      (await createSupervisorClient("tok").getSelfInfo()).ingressPanel,
    ).toBe(false);
  });

  it("posts ingress_panel as a sibling field, not inside options", async () => {
    const calls: Array<{ url: string; body: string }> = [];
    mockFetch((url, init) => {
      calls.push({ url, body: String(init?.body ?? "") });
      return { body: null };
    });
    await createSupervisorClient("tok").setIngressPanel(true);
    expect(calls).toHaveLength(1);
    const call = calls[0]!;
    expect(call.url).toContain("/addons/self/options");
    expect(JSON.parse(call.body)).toEqual({ ingress_panel: true });
  });

  it("returns the mapped host port for the secure container port", async () => {
    mockFetch(() => ({
      body: infoBody({ "8791/tcp": 8791, "8792/tcp": 18792 }),
    }));
    expect(
      await createSupervisorClient("tok").getMappedHostPort("8792/tcp"),
    ).toBe(18792);
  });

  it("returns null when the secure port is unmapped", async () => {
    mockFetch(() => ({
      body: infoBody({ "8791/tcp": 8791, "8792/tcp": null }),
    }));
    expect(
      await createSupervisorClient("tok").getMappedHostPort("8792/tcp"),
    ).toBeNull();
    // Absent entirely also yields null.
    mockFetch(() => ({ body: infoBody({ "8791/tcp": 8791 }) }));
    expect(
      await createSupervisorClient("tok").getMappedHostPort("8792/tcp"),
    ).toBeNull();
  });

  it("sends the bearer token and wraps options for setOptions", async () => {
    let seen: { url: string; init: RequestInit } | null = null;
    mockFetch((url, init) => {
      seen = { url, init: init ?? {} };
      return { status: 200 };
    });
    await createSupervisorClient("secret-token").setOptions({
      relay_auth_token: "x",
      ha_llat: "",
    });
    expect(seen!.url).toContain("/addons/self/options");
    expect(seen!.init!.redirect).toBe("error");
    expect((seen!.init!.headers as Record<string, string>).authorization).toBe(
      "Bearer secret-token",
    );
    expect(JSON.parse(seen!.init!.body as string)).toEqual({
      options: { relay_auth_token: "x", ha_llat: "" },
    });
  });

  it("throws on a non-2xx supervisor response", async () => {
    mockFetch(() => ({ status: 403, body: { message: "denied" } }));
    await expect(createSupervisorClient("tok").getSelfInfo()).rejects.toThrow(
      /403/,
    );
  });
});
