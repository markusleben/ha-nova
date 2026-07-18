import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { IncomingMessage, ServerResponse } from "node:http";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { createNovaActionHandler, createNovaPageHandler, type NovaPageDeps } from "../../nova/src/http/handlers/nova-page.js";
import { createCsrfStore } from "../../nova/src/security/csrf.js";
import { generateCredential } from "../../nova/src/security/device-credential.js";
import { openDeviceRegistry, type DeviceRegistry } from "../../nova/src/security/device-registry.js";
import { createPairingV1Manager, type PairingV1Manager } from "../../nova/src/security/pairing-v1.js";
import type { HaAuthUser } from "../../nova/src/security/owner-check.js";

const OWNER: HaAuthUser = { id: "owner-1", name: "Owner", is_owner: true, is_active: true, system_generated: false };

let dir: string;
let registry: DeviceRegistry;
let pairing: PairingV1Manager;
let csrf: ReturnType<typeof createCsrfStore>;
let deps: NovaPageDeps;
const now = () => 1000;

function req(over: { userId?: string; body?: Record<string, string>; secFetch?: string; owner?: HaAuthUser[] } = {}): IncomingMessage {
  return {
    socket: { remoteAddress: "172.30.32.2" },
    headers: {
      // HA sends the ingress BASE path (no page suffix); the console lives at "/".
      "x-ingress-path": "/api/hassio_ingress/tok",
      "x-remote-user-id": over.userId ?? "owner-1",
      ...(over.secFetch ? { "sec-fetch-site": over.secFetch } : {}),
    },
  } as unknown as IncomingMessage;
}

interface FakeRes {
  statusCode: number;
  headers: Record<string, string | number>;
  body: string;
  setHeader(k: string, v: string | number): void;
  end(b?: string): void;
}
function res(): FakeRes & ServerResponse {
  const r: FakeRes = {
    statusCode: 0,
    headers: {},
    body: "",
    setHeader(k, v) { this.headers[k.toLowerCase()] = v; },
    end(b) { if (b) this.body = b; },
  };
  return r as unknown as FakeRes & ServerResponse;
}

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-novapage-"));
  registry = openDeviceRegistry(dir);
  pairing = createPairingV1Manager({ registry, secureEndpoint: () => ({ spkiPin: "p", securePort: 8792 }), now });
  csrf = createCsrfStore();
  deps = {
    fetchAuthUsers: async () => [OWNER],
    csrf,
    pairing,
    registry,
    connection: () => ({ haConnected: true }),
    update: async () => ({ version: "0.7.0", versionLatest: "0.7.0", updateAvailable: false, error: false }),
    relayVersion: "0.7.0",
    now,
  };
});
afterEach(() => rmSync(dir, { recursive: true, force: true }));

async function call(handler: ReturnType<typeof createNovaPageHandler>, request: IncomingMessage, body: unknown = null) {
  const r = res();
  await handler({ request, response: r, path: "/home", body });
  return r as unknown as FakeRes;
}

describe("nova-page", () => {
  it("renders the owner console with a generate-code form", async () => {
    const r = await call(createNovaPageHandler(deps), req());
    expect(r.statusCode).toBe(200);
    expect(r.headers["content-security-policy"]).toContain("form-action 'self'");
    expect(r.headers["cache-control"]).toBe("no-store");
    expect(r.body).toContain("Connect a device");
    expect(r.body).toContain('name="csrf"');
  });

  it("auto-refreshes only while a code is active, and never uses JavaScript", async () => {
    // Inactive: no auto-refresh.
    let r = await call(createNovaPageHandler(deps), req());
    expect(r.body).not.toContain('http-equiv="refresh"');
    // Active: a meta refresh appears so the device shows up without a manual reload.
    pairing.generateCode();
    r = await call(createNovaPageHandler(deps), req());
    expect(r.body).toContain('http-equiv="refresh"');
    // Still no JavaScript anywhere (CSP forbids it; refresh is pure HTML).
    expect(r.body).not.toContain("<script");
  });

  it("denies a non-owner (403) and never renders the console", async () => {
    deps.fetchAuthUsers = async () => [{ ...OWNER, is_owner: false }];
    const r = await call(createNovaPageHandler(deps), req());
    expect(r.statusCode).toBe(403);
    expect(r.body).not.toContain("Connect a device");
  });

  it("generates a code via a valid CSRF form (PRG redirect)", async () => {
    const token = csrf.issue("owner-1", "generate_code", now());
    const r = await call(createNovaActionHandler(deps), req(), { action: "generate_code", csrf: token });
    expect(r.statusCode).toBe(303);
    // The PRG target must keep the ingress base path's trailing slash, or HA 404s it.
    expect(r.headers["location"]).toBe("/api/hassio_ingress/tok/");
    expect(pairing.getStatus().phase).toBe("active");
  });

  it("refuses a cross-site POST", async () => {
    const token = csrf.issue("owner-1", "generate_code", now());
    const r = await call(createNovaActionHandler(deps), req({ secFetch: "cross-site" }), { action: "generate_code", csrf: token });
    expect(r.statusCode).toBe(403);
    expect(pairing.getStatus().phase).toBe("inactive");
  });

  it("rejects a missing/invalid CSRF token", async () => {
    const r = await call(createNovaActionHandler(deps), req(), { action: "generate_code", csrf: "bogus" });
    expect(r.statusCode).toBe(403);
    expect(pairing.getStatus().phase).toBe("inactive");
  });

  it("rejects a token replayed for a different action", async () => {
    const token = csrf.issue("owner-1", "generate_code", now());
    const r = await call(createNovaActionHandler(deps), req(), { action: "cancel_code", csrf: token });
    expect(r.statusCode).toBe(403);
  });

  it("revokes a device via its form", async () => {
    const c = generateCredential();
    registry.createPending({ deviceId: c.deviceId, secretDigest: c.secretDigest, clientInstallId: "i", name: "Mac", platform: "darwin", client: "claude", createdAtMs: 1 }, now());
    registry.activate(c.deviceId, now());
    const token = csrf.issue("owner-1", "revoke_device", now());
    const r = await call(createNovaActionHandler(deps), req(), { action: "revoke_device", csrf: token, device_id: c.deviceId });
    expect(r.statusCode).toBe(303);
    expect(registry.list()).toHaveLength(0);
  });
});
