import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { digestSecret, generateCredential } from "../../nova/src/security/device-credential.js";
import { openDeviceRegistry, type DeviceRegistry } from "../../nova/src/security/device-registry.js";
import { resolvePrincipal } from "../../nova/src/security/principal.js";

let dir: string;
let registry: DeviceRegistry;
const now = () => 1000;
const deps = () => ({ registry, now });
const auth = (t: string) => `Bearer ${t}`;

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-principal-"));
  registry = openDeviceRegistry(dir);
});
afterEach(() => rmSync(dir, { recursive: true, force: true }));

function activeDevice() {
  const c = generateCredential();
  registry.createPending(
    { deviceId: c.deviceId, secretDigest: c.secretDigest, clientInstallId: "i", name: "n", platform: "p", client: "c", createdAtMs: 1 },
    now()
  );
  registry.activate(c.deviceId, now());
  return c;
}

describe("principal resolver", () => {
  it("resolves an active device credential over the secure transport", () => {
    const c = activeDevice();
    const r = resolvePrincipal(auth(c.credential), "secure", deps());
    expect(r).toEqual({ ok: true, principal: { kind: "device", deviceId: c.deviceId } });
  });

  it("refuses a device credential over plain HTTP with SECURE_TRANSPORT_REQUIRED", () => {
    const c = activeDevice();
    const r = resolvePrincipal(auth(c.credential), "plain", deps());
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.status).toBe(403);
  });

  it("rejects a revoked device credential (401)", () => {
    const c = activeDevice();
    registry.revoke(c.deviceId);
    const r = resolvePrincipal(auth(c.credential), "secure", deps());
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.status).toBe(401);
  });

  it("resolves the legacy shared token on either transport while it exists", () => {
    const legacy = "legacy-shared-token-value";
    registry.importLegacy(digestSecret(legacy), now());
    expect(resolvePrincipal(auth(legacy), "plain", deps())).toEqual({ ok: true, principal: { kind: "legacy" } });
    expect(resolvePrincipal(auth(legacy), "secure", deps())).toEqual({ ok: true, principal: { kind: "legacy" } });
    // After revoke it no longer resolves.
    registry.revokeLegacy();
    expect(resolvePrincipal(auth(legacy), "plain", deps()).ok).toBe(false);
  });

  it("rejects missing/malformed authorization (401)", () => {
    for (const h of [undefined, "", "Basic xyz", "Bearer", "Bearer "]) {
      const r = resolvePrincipal(h, "secure", deps());
      expect(r.ok).toBe(false);
      if (!r.ok) expect(r.status).toBe(401);
    }
  });

  it("rejects an unknown non-device token (401)", () => {
    const r = resolvePrincipal(auth("some-random-token"), "secure", deps());
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.status).toBe(401);
  });
});
