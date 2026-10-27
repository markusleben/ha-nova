import { describe, expect, it } from "vitest";

import { HttpError } from "../../nova/src/http/errors.js";
import { createCoreProxyHandler } from "../../nova/src/http/handlers/core-proxy.js";
import { createWsProxyHandler } from "../../nova/src/http/handlers/ws-proxy.js";
import type { RouteContext } from "../../nova/src/http/router.js";

// The Relay itself refuses Home Assistant's Supervisor paths, as the
// Supervisor's App filter does, so a standalone Relay container pointed at a
// Supervised Home Assistant cannot reach them either (supervisor-role spec).

async function code(run: () => unknown): Promise<string> {
  try {
    await run();
  } catch (error) {
    expect(error).toBeInstanceOf(HttpError);
    return (error as HttpError).code;
  }
  return "SENT";
}

describe("Supervisor deny on /ws and /core", () => {
  it("refuses supervisor/* and hassio/* WS types, inside collect_events too", async () => {
    const sent: unknown[] = [];
    const ws = createWsProxyHandler({
      wsClient: {
        sendMessage: async (m) => (sent.push(m), {}),
        collectMessageEvents: async (m) => (sent.push(m), { events: [], truncated: false }),
      },
    });
    for (const type of ["supervisor/api", "supervisor/subscribe", "hassio/update/addon", "Supervisor/api", " hassio/addon/info", "HASSIO/x"]) {
      expect(await code(() => ws({ body: { type, endpoint: "/info", method: "get" } } as RouteContext)), type).toBe("WS_TYPE_REFUSED");
      expect(await code(() => ws({ body: { message: { type }, collect_events: { max_events: 1 } } } as RouteContext)), type).toBe("WS_TYPE_REFUSED");
    }
    expect(sent).toEqual([]);
    expect(await code(() => ws({ body: { type: "get_states" } } as RouteContext))).toBe("SENT");
  });

  it("refuses Home Assistant's /api/hassio proxy paths after decoding", async () => {
    const sent: unknown[] = [];
    const core = createCoreProxyHandler({ coreClient: { request: async (input) => (sent.push(input), { status: 200, body: {} }) } });
    for (const path of ["/api/hassio/addons", "/api/hassio_ingress/abc/", "/api/HASSIO/info", "/api//hassio/info", "/api/hassio%2Finfo", "/api/%68assio/info", "/api/hassio_push/x"]) {
      expect(["CORE_PATH_REFUSED", "CORE_PATH_INVALID"], path).toContain(await code(() => core({ body: { method: "GET", path } } as RouteContext)));
    }
    expect(sent).toEqual([]);
    expect(await code(() => core({ body: { method: "GET", path: "/api/states" } } as RouteContext))).toBe("SENT");
  });
});
