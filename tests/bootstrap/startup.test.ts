import { describe, expect, it } from "vitest";

import { createApp } from "../../src/index.js";

describe("startup bootstrap", () => {
  it("exports createApp factory and returns server + router", () => {
    const app = createApp({
      authToken: "secret",
      version: "1.0.0",
      wsClient: {
        isConnected: () => true,
        sendMessage: async () => ({ ok: true })
      },
      coreClient: {
        request: async () => ({
          status: 200,
          body: null
        })
      },
      startedAtMs: 1_000,
      now: () => 2_000
    });

    expect(typeof createApp).toBe("function");
    expect(app.version).toBe("1.0.0");
    expect(typeof app.server.listen).toBe("function");
    expect(typeof app.router.register).toBe("function");
  });
});
