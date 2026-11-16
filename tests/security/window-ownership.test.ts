import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import type { IncomingMessage, ServerResponse } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createGateAsks } from "../../nova/src/http/gate-asks.js";
import type { RouteContext } from "../../nova/src/http/router.js";
import { generateCredential } from "../../nova/src/security/device-credential.js";
import { openDeviceRegistry, RegistryCorruptError } from "../../nova/src/security/device-registry.js";
import { DEVICE_REGISTRY_FILE } from "../../nova/src/security/device-registry-schema.js";
import type { CredentialStore, DeviceRecord } from "../../nova/src/security/device-registry-types.js";
import { BRIDGE_INSTALL_ID, decodePairingMetadata } from "../../nova/src/security/pairing-metadata.js";
import type { Principal } from "../../nova/src/security/principal.js";
import { windowStatus } from "../../nova/src/security/window-confirm.js";

let dir: string;
beforeEach(() => { dir = mkdtempSync(join(tmpdir(), "ha-nova-window-own-")); });
afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

function pair(over: Partial<DeviceRecord> = {}, install = "install") {
  const registry = openDeviceRegistry(dir);
  const { deviceId, secretDigest } = generateCredential();
  registry.createPending({ deviceId, secretDigest, clientInstallId: install, name: "Computer", platform: "darwin", client: "cli", createdAtMs: 1, ...over }, 1);
  if (over.cloudUserId === undefined) registry.activate(deviceId, 2);
  else registry.activatePendingForCloud(deviceId, secretDigest, over.cloudUserId, over.cloudRelayInstanceId!, 2);
  const read = () => openDeviceRegistry(dir).list().find((d) => d.deviceId === deviceId)!; // from disk
  return { registry, deviceId, read };
}

describe("legacy sign-in cleanup on a Mac pairing", () => {
  it("reads only the two cleanup values from pairing metadata", () => {
    const meta = (legacy_cleanup: unknown) => decodePairingMetadata(Buffer.from(JSON.stringify({ name: "Mac", platform: "darwin", client: "cli", client_install_id: "i", credential_store: "keychain", legacy_cleanup })));
    expect(meta("pending")?.legacyCleanup).toBe("pending");
    expect(meta("complete")?.legacyCleanup).toBe("complete");
    for (const bad of [undefined, "done", true, null]) expect(meta(bad)).not.toHaveProperty("legacyCleanup");
  });

  it("keeps the report through activation and on disk, and refuses a corrupt value", () => {
    const { read } = pair({ credentialStore: "keychain", legacyCleanup: "pending" });
    expect(read().legacyCleanup).toBe("pending");
    const file = join(dir, DEVICE_REGISTRY_FILE);
    const data = JSON.parse(readFileSync(file, "utf8")) as { devices: Record<string, unknown>[] };
    data.devices[0]!.legacyCleanup = "done";
    writeFileSync(file, JSON.stringify(data));
    expect(() => openDeviceRegistry(dir)).toThrow(RegistryCorruptError);
  });

  it.each([[undefined, false], ["pending", false], ["complete", true]] as const)("a keychain computer with cleanup %s confirms in its window: %s", (legacyCleanup, ok) => {
    const record: DeviceRecord = { deviceId: "d", secretDigest: "s", state: "active", clientInstallId: "i", name: "Mac", platform: "darwin", client: "cli", createdAtMs: 1,
      credentialStore: "keychain", ownComputer: { userId: "owner", atMs: 1 }, windowConfirm: true, ...(legacyCleanup ? { legacyCleanup } : {}) };
    expect(windowStatus({ kind: "device", deviceId: "d" }, record)).toEqual(ok ? { window: true, why: "on" } : { window: false, why: "legacy_cleanup_pending" });
    expect(windowStatus({ kind: "device", deviceId: "d" }, { ...record, credentialStore: "secret_service" }).window).toBe(true);
  });

  it("completes only the cleanup field, once, for an active computer", () => {
    const { registry, deviceId, read } = pair({ credentialStore: "keychain", legacyCleanup: "pending" });
    const before = read();
    expect(registry.completeLegacyCleanup(deviceId)).toBe(true);
    expect(registry.completeLegacyCleanup(deviceId)).toBe(true);
    expect(read()).toEqual({ ...before, legacyCleanup: "complete" });
    expect(read().ownComputer).toBeUndefined();
    expect(registry.completeLegacyCleanup("unknown")).toBe(false);
    const bridge = pair({}, BRIDGE_INSTALL_ID);
    expect(bridge.registry.completeLegacyCleanup(bridge.deviceId)).toBe(false);
  });

  it("answers the authenticated computer's own report on the ask route, and nobody else's", async () => {
    const complete = vi.fn((p: Principal) => p.kind === "device" && p.deviceId === "mac");
    const asks = createGateAsks({ ask: async () => null, prepare: {}, deviceName: () => "Mac", channels: () => [], link: () => undefined, phone: async () => null, now: () => 1, legacyCleanupComplete: complete });
    const call = (principal: Principal, body: unknown) => asks.handler({ request: {} as IncomingMessage, response: {} as ServerResponse, path: "/gate/v1/ask", body, principal } as RouteContext);
    expect(await call({ kind: "device", deviceId: "mac" }, { op: "legacy_cleanup_complete", device_id: "other" })).toEqual({ legacy_cleanup: "complete" });
    expect(complete).toHaveBeenCalledWith({ kind: "device", deviceId: "mac" });
    for (const who of [{ kind: "device", deviceId: "other" }, { kind: "shared" }, { kind: "device", deviceId: "mac", internal: "gate" }] as Principal[]) {
      await expect(call(who, { op: "legacy_cleanup_complete" })).rejects.toMatchObject({ status: 404 });
    }
  });
});

describe("the owner's window declaration and switch", () => {
  const eligible = (store: CredentialStore = "secret_service", over: Partial<DeviceRecord> = {}) => pair({ credentialStore: store, ...over });

  it("turning on an undeclared computer declares the clicking owner and turns the switch on in one write; clearing it clears both", () => {
    const { registry, deviceId, read } = eligible();
    expect(registry.setOwnComputer(deviceId, "owner", 50)).toBeNull();
    expect(read()).toMatchObject({ ownComputer: { userId: "owner", atMs: 50 }, windowConfirm: true });
    expect(windowStatus({ kind: "device", deviceId }, read())).toEqual({ window: true, why: "on" });
    expect(registry.setOwnComputer(deviceId, null, 60)).toBeNull();
    expect(read().ownComputer).toBeUndefined();
    expect(read().windowConfirm).toBe(false);
  });

  it("turns on only the clicking owner's own declaration; off always works", () => {
    const { registry, deviceId, read } = eligible();
    expect(registry.setWindowConfirm(deviceId, true, "owner")).toBe("not_yours");
    registry.setOwnComputer(deviceId, "owner", 50);
    expect(registry.setWindowConfirm(deviceId, false, "someone")).toBeNull();
    expect(read().windowConfirm).toBe(false);
    expect(registry.setWindowConfirm(deviceId, true, "new-owner")).toBe("not_yours");
    expect(registry.setWindowConfirm(deviceId, true, "owner")).toBeNull();
    expect(read().windowConfirm).toBe(true);
    expect(registry.disableWindowConfirm(deviceId)).toBe(true);
    expect(read().windowConfirm).toBe(false);
  });

  it.each([
    ["an unknown store", {}, "pair_again"], ["a key file", { credentialStore: "file" as const }, "file"],
    ["a pending Mac cleanup", { credentialStore: "keychain" as const, legacyCleanup: "pending" as const }, "legacy_cleanup_pending"],
    ["a missing Mac cleanup", { credentialStore: "keychain" as const }, "legacy_cleanup_pending"],
    ["another owner's Link sign-in", { credentialStore: "secret_service" as const, cloudUserId: "someone", cloudRelayInstanceId: "hanova-relay-v1.AAAAAAAAAAAAAAAAAAAAAA" }, "owner"],
  ])("refuses to enable %s and writes nothing", (_name, over, why) => {
    const { registry, deviceId, read } = pair(over);
    const before = read();
    expect(registry.setOwnComputer(deviceId, "owner", 50)).toBe(why);
    expect(read()).toEqual(before);
    expect(registry.setOwnComputer(deviceId, null, 50)).toBeNull(); // reducing authority stays possible
    expect(read().legacyCleanup).toBe(before.legacyCleanup);
  });

  it("refuses unknown, pending, and reserved devices", () => {
    const registry = openDeviceRegistry(dir);
    const { deviceId, secretDigest } = generateCredential();
    registry.createPending({ deviceId, secretDigest, clientInstallId: "i", name: "C", platform: "linux", client: "cli", createdAtMs: 1, credentialStore: "secret_service" }, 1);
    expect(registry.setOwnComputer(deviceId, "owner", 1)).toBe("device");
    expect(registry.setWindowConfirm("unknown", true, "owner")).toBe("device");
    expect(registry.disableWindowConfirm("unknown")).toBe(false);
    const bridge = pair({ credentialStore: "secret_service" }, BRIDGE_INSTALL_ID);
    expect(bridge.registry.setOwnComputer(bridge.deviceId, "owner", 1)).toBe("device");
  });
});
