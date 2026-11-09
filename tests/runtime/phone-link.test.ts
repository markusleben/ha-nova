import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { phoneLinkOf } from "../../nova/src/runtime/app-mode.js";
import { openDeviceRegistry } from "../../nova/src/security/device-registry.js";

let dir: string;
beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-phone-link-"));
});
afterEach(() => rmSync(dir, { recursive: true, force: true }));

const device = (deviceId: string, state: string, confirmPhone?: object) => ({
  deviceId, secretDigest: "d-" + deviceId, clientInstallId: "i-" + deviceId, name: "Mac", platform: "darwin", client: "claude", createdAtMs: 1,
  state, ...(state === "pending" ? { pendingExpiresAtMs: 9e15 } : {}), ...(confirmPhone ? { confirmPhone } : {}),
});

// The App asks an active device's own linked phone, as the registry keeps it,
// on the worker's wire (cli/gate_ask.go phoneLink).
describe("phoneLinkOf", () => {
  it("maps an active device's link and nothing else", () => {
    const phone = { service: "mobile_app_iphone", deviceId: "ha-iphone", userId: "u1", platform: "ios" };
    writeFileSync(join(dir, "device-registry.json"), JSON.stringify({ version: 1, legacy: null, devices: [
      device("mac", "active", phone), device("bare", "active"), device("new", "pending", phone), device("old", "active", { ...phone, platform: undefined }),
    ] }));
    const link = phoneLinkOf(openDeviceRegistry(dir));
    expect(link({ kind: "device", deviceId: "mac" })).toEqual({ service: "mobile_app_iphone", device_id: "ha-iphone", user_id: "u1", platform: "ios" });
    expect(link({ kind: "device", deviceId: "old" })).toEqual({ service: "mobile_app_iphone", device_id: "ha-iphone", user_id: "u1" });
    expect(link({ kind: "device", deviceId: "bare" })).toBeUndefined();
    expect(link({ kind: "device", deviceId: "new" })).toBeUndefined();
    expect(link({ kind: "device", deviceId: "gone" })).toBeUndefined();
    expect(link({ kind: "legacy" })).toBeUndefined();
  });

  it("drops the link with its revoked device", () => {
    writeFileSync(join(dir, "device-registry.json"), JSON.stringify({ version: 1, legacy: null, devices: [
      device("mac", "active", { service: "mobile_app_iphone", deviceId: "ha-iphone", userId: "u1" }),
    ] }));
    const registry = openDeviceRegistry(dir);
    const link = phoneLinkOf(registry);
    expect(link({ kind: "device", deviceId: "mac" })).toBeDefined();
    registry.revoke("mac");
    expect(link({ kind: "device", deviceId: "mac" })).toBeUndefined();
  });
});
