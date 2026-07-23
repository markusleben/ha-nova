import type { IncomingMessage, ServerResponse } from "node:http";

import { afterEach, describe, expect, it } from "vitest";

import { createApp } from "../../nova/src/index.js";
import {
  createHomeHandler,
  HOME_CONTENT_SECURITY_POLICY,
  isSupervisorIngressRequest
} from "../../nova/src/http/handlers/home.js";
import { createPairingManager } from "../../nova/src/security/pairing.js";

describe("Home Base", () => {
  const servers: Array<ReturnType<typeof createApp>["server"]> = [];

  afterEach(async () => {
    await Promise.all(
      servers.map((server) => new Promise<void>((resolve) => server.close(() => resolve())))
    );
    servers.length = 0;
  });

  it("renders truthful, script-free status for authenticated Supervisor ingress", async () => {
    const now = 60_000;
    const pairing = createPairingManager({
      relayToken: "never-render-this-token",
      now: () => now,
      generateCodeNumber: () => 123_456
    });
    const handler = createHomeHandler({
      health: {
        version: "0.5.0",
        wsClient: {
          isConnected: () => false,
          getConnectionStatus: () => ({ connected: false, disconnect_reason: "network" })
        },
        startedAtMs: 0,
        fileAccessMode: "read",
        snapshotRoot: "/path/that/does/not/exist",
        now: () => now
      },
      pairing,
      requiredRelayVersion: "0.4.0",
      now: () => now
    });
    const recorder = createResponseRecorder();

    await handler({
      request: ingressRequest("::ffff:172.30.32.2"),
      response: recorder.response,
      path: "/home",
      body: null
    });

    expect(recorder.response.statusCode).toBe(200);
    expect(recorder.header("content-type")).toBe("text/html; charset=utf-8");
    expect(recorder.header("cache-control")).toBe("no-store");
    expect(recorder.header("content-security-policy")).toBe(HOME_CONTENT_SECURITY_POLICY);
    expect(recorder.header("x-content-type-options")).toBe("nosniff");
    expect(recorder.body).toContain("123 456");
    expect(recorder.body).toContain("Home Assistant is not reachable");
    expect(recorder.body).toContain("<dd>0.5.0</dd>");
    expect(recorder.body).toContain("<dd>0.4.0</dd>");
    expect(recorder.body).toContain("<dd>read</dd>");
    expect(recorder.body).toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.sh");
    expect(recorder.body).toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1");
    expect(recorder.body).toContain("standard latest-stable installer");
    expect(recorder.body).toContain("First update the NOVA Relay App to the latest version");
    expect(recorder.body).not.toContain("HA_NOVA_VERSION");
    expect(recorder.body).not.toContain("v0.17.0/install.sh");
    expect(recorder.body).not.toContain("v0.5.0/install.sh");
    expect(recorder.body).not.toContain("never-render-this-token");
    expect(recorder.body).not.toContain("<script");
    expect(recorder.body).not.toContain("<link");
    expect(recorder.body).not.toContain("<img");
  });

  it("accepts only the Supervisor ingress peer with both identity headers", () => {
    expect(isSupervisorIngressRequest(ingressRequest("172.30.32.2"))).toBe(true);
    expect(isSupervisorIngressRequest(ingressRequest("::ffff:172.30.32.2"))).toBe(true);
    expect(isSupervisorIngressRequest(ingressRequest("::ffff:ac1e:2002"))).toBe(true);
    expect(isSupervisorIngressRequest(ingressRequest("127.0.0.1"))).toBe(false);
    expect(isSupervisorIngressRequest(ingressRequest("172.30.32.3"))).toBe(false);
    expect(
      isSupervisorIngressRequest(ingressRequest("172.30.32.2", { "x-ingress-path": "" }))
    ).toBe(false);
    expect(
      isSupervisorIngressRequest(ingressRequest("172.30.32.2", { "x-remote-user-id": "" }))
    ).toBe(false);
  });

  it("rejects direct-port requests even when ingress headers are spoofed", async () => {
    const app = createTestApp();
    servers.push(app.server);
    const baseUrl = await startServer(app.server);

    const response = await fetch(`${baseUrl}/home`, {
      headers: {
        authorization: "Bearer relay-secret",
        "x-ingress-path": "/api/hassio_ingress/fake",
        "x-remote-user-id": "admin"
      }
    });

    expect(response.status).toBe(403);
    expect(response.headers.get("cache-control")).toBe("no-store");
    expect(response.headers.get("content-security-policy")).toBe(HOME_CONTENT_SECURITY_POLICY);
    expect(response.headers.get("x-content-type-options")).toBe("nosniff");
    await expect(response.json()).resolves.toMatchObject({
      ok: false,
      error: { code: "INGRESS_REQUIRED" }
    });
  });
});

function ingressRequest(
  remoteAddress: string,
  overrides: Record<string, string> = {}
): IncomingMessage {
  return {
    socket: { remoteAddress },
    headers: {
      "x-ingress-path": "/api/hassio_ingress/example",
      "x-remote-user-id": "user-id",
      ...overrides
    }
  } as unknown as IncomingMessage;
}

function createResponseRecorder(): {
  response: ServerResponse;
  body: string;
  header(name: string): string | undefined;
} {
  const headers = new Map<string, string>();
  const recorder = {
    statusCode: 0,
    writableEnded: false,
    body: "",
    setHeader(name: string, value: string | number) {
      headers.set(name.toLowerCase(), String(value));
      return this;
    },
    end(body?: string) {
      this.body = body ?? "";
      this.writableEnded = true;
      return this;
    }
  };
  return {
    response: recorder as unknown as ServerResponse,
    get body() {
      return recorder.body;
    },
    header: (name) => headers.get(name.toLowerCase())
  };
}

function createTestApp(): ReturnType<typeof createApp> {
  return createApp({
    authToken: "relay-secret",
    version: "0.5.0",
    fileAccess: { mode: "off", configRoot: "", warnings: [] },
    snapshotRoot: "/path/that/does/not/exist",
    wsClient: {
      isConnected: () => true,
      sendMessage: async () => ({}),
      collectMessageEvents: async () => ({ events: [], truncated: false })
    },
    coreClient: { request: async () => ({ status: 200, body: null }) }
  });
}

async function startServer(server: ReturnType<typeof createApp>["server"]): Promise<string> {
  const port = await new Promise<number>((resolve, reject) => {
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      if (!address || typeof address === "string") {
        reject(new Error("Server did not return a TCP address"));
        return;
      }
      resolve(address.port);
    });
    server.on("error", reject);
  });
  return `http://127.0.0.1:${port}`;
}
