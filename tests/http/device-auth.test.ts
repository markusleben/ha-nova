import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { createDeviceActivateHandler, createDeviceRevokeSelfHandler } from "../../nova/src/http/handlers/device-auth.js";
import { HttpError } from "../../nova/src/http/errors.js";
import type { RouteContext } from "../../nova/src/http/router.js";
import { generateCredential } from "../../nova/src/security/device-credential.js";
import { openDeviceRegistry, type DeviceRegistry } from "../../nova/src/security/device-registry.js";

let dir: string;
let registry: DeviceRegistry;
const now = () => 1000;

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-devauth-"));
  registry = openDeviceRegistry(dir);
});
afterEach(() => rmSync(dir, { recursive: true, force: true }));

// Minimal context: the handlers only read request.headers.authorization.
function ctx(token: string | undefined): RouteContext {
  return {
    request: { headers: token === undefined ? {} : { authorization: `Bearer ${token}` } } as RouteContext["request"],
    response: {} as RouteContext["response"],
    path: "/auth/device",
    body: null,
  };
}

function pendingCredential() {
  const c = generateCredential();
  registry.createPending(
    { deviceId: c.deviceId, secretDigest: c.secretDigest, clientInstallId: "i", name: "n", platform: "p", client: "c", createdAtMs: 1 },
    now()
  );
  return c;
}

function status(fn: () => unknown): number | "ok" {
  try {
    fn();
    return "ok";
  } catch (e) {
    if (e instanceof HttpError) return e.status;
    throw e;
  }
}

describe("device-auth handlers", () => {
  const activate = () => createDeviceActivateHandler({ registry, now });
  const revoke = () => createDeviceRevokeSelfHandler({ registry, now });

  it("activates a provisional credential (idempotently)", () => {
    const c = pendingCredential();
    const r1 = activate()(ctx(c.credential)) as { activated: boolean; device_id: string };
    expect(r1.activated).toBe(true);
    expect(registry.resolveDeviceSecret(c.deviceId, c.secretDigest, now())).not.toBeNull();
    // Idempotent second activation.
    expect(status(() => activate()(ctx(c.credential)))).toBe("ok");
  });

  it("rejects activation with a wrong or unknown credential (401)", () => {
    pendingCredential();
    const wrong = generateCredential(); // never registered
    expect(status(() => activate()(ctx(wrong.credential)))).toBe(401);
  });

  it("revokes an active device, then a retry is 401 (treated as success by the CLI)", () => {
    const c = pendingCredential();
    activate()(ctx(c.credential));
    const r = revoke()(ctx(c.credential)) as { revoked: boolean };
    expect(r.revoked).toBe(true);
    expect(registry.resolveDeviceSecret(c.deviceId, c.secretDigest, now())).toBeNull();
    // Second revoke -> 401.
    expect(status(() => revoke()(ctx(c.credential)))).toBe(401);
  });

  it("rejects revoke-self of a merely-pending (not yet active) credential", () => {
    const c = pendingCredential();
    expect(status(() => revoke()(ctx(c.credential)))).toBe(401);
  });

  it("rejects missing or malformed bearer (401)", () => {
    expect(status(() => activate()(ctx(undefined)))).toBe(401);
    expect(status(() => activate()(ctx("not-a-credential")))).toBe(401);
  });
});
