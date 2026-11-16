import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { openDeviceRegistry, RegistryCorruptError } from "../../nova/src/security/device-registry.js";
import type { DeviceRecord } from "../../nova/src/security/device-registry-types.js";
import { decodePairingMetadata, GATE_INSTALL_ID, BRIDGE_INSTALL_ID } from "../../nova/src/security/pairing-metadata.js";
import { checkWindowConfirm, pairingWindowFields, windowStatus } from "../../nova/src/security/window-confirm.js";

const owner = { id: "owner", is_owner: true, is_active: true };
const principal = { kind: "device" as const, deviceId: "device" };
const eligible: DeviceRecord = {
  deviceId: "device", secretDigest: "digest", state: "active", clientInstallId: "install",
  name: "Computer", platform: "darwin", client: "cli", createdAtMs: 1000,
  credentialStore: "keychain", legacyCleanup: "complete", ownComputer: { userId: "owner", atMs: 1000 }, windowConfirm: true,
};
let dir: string;
beforeEach(() => { dir = mkdtempSync(join(tmpdir(), "ha-nova-window-confirm-")); });
afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

describe("window confirmation eligibility", () => {
  it.each(["keychain", "credential_manager", "secret_service"] as const)("accepts a declared owner's %s pairing", async (credentialStore) => {
    const list = () => [{ ...eligible, credentialStore }];
    expect(await checkWindowConfirm(principal, { list }, async () => [owner])).toEqual({ window: true, why: "on" });
  });

  it.each([
    { credentialStore: undefined }, { credentialStore: "file" as const }, { ownComputer: undefined },
    { windowConfirm: false }, { windowConfirm: undefined }, { state: "pending" as const },
    { cloudUserId: "other" }, { clientInstallId: GATE_INSTALL_ID }, { clientInstallId: BRIDGE_INSTALL_ID },
  ])("excludes a pairing with %j", (over) => {
    const record = structuredClone(eligible);
    Object.assign(record, over);
    for (const [key, value] of Object.entries(record)) if (value === undefined) Reflect.deleteProperty(record, key);
    expect(windowStatus(principal, record).window).toBe(false);
  });

  it("allows an owner-bound Link device but not shared or internal principals", () => {
    expect(windowStatus(principal, { ...eligible, cloudUserId: owner.id }).window).toBe(true);
    expect(windowStatus({ kind: "shared" }, eligible).window).toBe(false);
    expect(windowStatus({ ...principal, internal: "chat-bridge" }, eligible).window).toBe(false);
    expect(windowStatus(undefined, eligible).window).toBe(false);
    expect(windowStatus(principal, undefined).window).toBe(false);
  });

  it.each([
    { users: [] }, { users: [{ ...owner, is_owner: false }] }, { users: [{ ...owner, is_active: false }] },
    { users: [{ ...owner, system_generated: true }] }, { users: [{ ...owner, is_active: undefined }] },
  ])("requires a fresh positive owner read: %j", async ({ users }) => {
    expect((await checkWindowConfirm(principal, { list: () => [eligible] }, async () => users)).window).toBe(false);
  });

  it("rechecks the device after the owner read", async () => {
    let records = [eligible];
    const result = await checkWindowConfirm(principal, { list: () => records }, async () => { records = []; return [owner]; });
    expect(result.window).toBe(false);
  });

  it("does not cache owner reads or hide their failure", async () => {
    const fetch = vi.fn().mockResolvedValueOnce([owner]).mockResolvedValueOnce([]).mockRejectedValueOnce(new Error("offline"));
    const registry = { list: () => [eligible] };
    expect((await checkWindowConfirm(principal, registry, fetch)).window).toBe(true);
    expect((await checkWindowConfirm(principal, registry, fetch)).window).toBe(false);
    await expect(checkWindowConfirm(principal, registry, fetch)).rejects.toThrow("offline");
    await expect(checkWindowConfirm(principal, registry, async () => ({}))).rejects.toThrow("non-list");
  });
});

describe("window pairing provenance and persistence", () => {
  const metadata = { name: "Computer", platform: "darwin", client: "cli", clientInstallId: "install", credentialStore: "keychain" as const };

  it("keeps an older entry to retire only on the owner's sign-in pairing, and persists it", () => {
    const decoded = decodePairingMetadata(Buffer.from(JSON.stringify({ ...metadata, client_install_id: "install-home", retire_device_id: "old" })))!;
    expect(decoded.retireDeviceId).toBe("old");
    expect(pairingWindowFields(decoded, undefined, false, 1000)).not.toHaveProperty("retireDeviceId");
    expect(pairingWindowFields(decoded, owner.id, false, 1000)).toMatchObject({ retireDeviceId: "old" });
    expect(decodePairingMetadata(Buffer.from(JSON.stringify({ ...metadata, client_install_id: "install", retire_device_id: 7 })))).not.toHaveProperty("retireDeviceId");
    const write = (retireDeviceId: unknown) =>
      writeFileSync(join(dir, "device-registry.json"), JSON.stringify({ version: 1, devices: [{ ...eligible, state: "pending", pendingExpiresAtMs: 9_000, retireDeviceId }], legacy: null }));
    write("old");
    expect(openDeviceRegistry(dir).list()[0]?.retireDeviceId).toBe("old");
    write(7);
    expect(() => openDeviceRegistry(dir)).toThrow(RegistryCorruptError);
  });

  it("does not infer ownership from client metadata, a code pairing, or a missing choice", () => {
    const decoded = decodePairingMetadata(Buffer.from(JSON.stringify({ ...metadata, client_install_id: "install", own_computer: true, credential_store: "keychain" })))!;
    expect(pairingWindowFields(decoded, undefined, true, 1000)).toEqual({ credentialStore: "keychain" });
    expect(pairingWindowFields(decoded, owner.id, false, 1000)).not.toHaveProperty("ownComputer");
    expect(pairingWindowFields({ ...metadata, credentialStore: "file" }, owner.id, true, 1000)).not.toHaveProperty("ownComputer");
    const { credentialStore: _store, ...unknownStore } = metadata;
    expect(pairingWindowFields(unknownStore, owner.id, true, 1000)).not.toHaveProperty("ownComputer");
    for (const credential_store of ["other", null, 3]) {
      const future = decodePairingMetadata(Buffer.from(JSON.stringify({ ...metadata, client_install_id: "install", credential_store })))!;
      expect(future).not.toBeNull();
      expect(future).not.toHaveProperty("credentialStore");
      expect(pairingWindowFields(future, owner.id, true, 1000)).not.toHaveProperty("ownComputer");
    }
  });

  it("persists the new fields through activation without exposing mutable authority", () => {
    const registry = openDeviceRegistry(dir);
    const record = { ...eligible, deviceId: "new", ...pairingWindowFields(metadata, owner.id, true, 1000) };
    registry.createPending(record, 1000);
    const active = registry.activate("new", 1000);
    expect(active).toMatchObject({ ownComputer: eligible.ownComputer, credentialStore: "keychain", windowConfirm: true });
    expect(active).not.toHaveProperty("pairingOwnerId");
    active.ownComputer!.userId = "changed";
    registry.list()[0]!.ownComputer!.userId = "changed";
    expect(openDeviceRegistry(dir).list()[0]?.ownComputer?.userId).toBe(owner.id);
    expect(registry.list()[0]?.ownComputer?.userId).toBe(owner.id);
  });

  it.each([true, false])("carries the predecessor phone only for an owner sign-in: %s", (signedIn) => {
    const registry = openDeviceRegistry(dir);
    const phone = { service: "mobile_app_phone", deviceId: "phone", userId: "admin" };
    registry.createPending({ ...eligible, deviceId: "old" }, 1000);
    registry.activate("old", 1000);
    registry.setConfirmPhone("old", phone, undefined);
    registry.createPending({ ...eligible, deviceId: "new", ...(signedIn ? { pairingOwnerId: owner.id } : {}) }, 1001);
    const active = registry.activate("new", 1001);
    expect(active.confirmPhone).toEqual(signedIn ? phone : undefined);
    expect(registry.list().map((d) => d.deviceId)).toEqual(["new"]);
    expect(registry.activate("new", 1002).confirmPhone).toEqual(active.confirmPhone);
  });

  it("persists turning off only an active computer's own window", () => {
    const registry = openDeviceRegistry(dir);
    registry.createPending(eligible, 1000);
    expect(registry.disableWindowConfirm(eligible.deviceId)).toBe(false);
    registry.activate(eligible.deviceId, 1000);
    expect(registry.disableWindowConfirm("unknown")).toBe(false);
    expect(registry.disableWindowConfirm(eligible.deviceId)).toBe(true);
    expect(registry.disableWindowConfirm(eligible.deviceId)).toBe(true);
    expect(openDeviceRegistry(dir).list()[0]?.windowConfirm).toBe(false);
    expect(registry.list()[0]?.ownComputer).toEqual(eligible.ownComputer);
  });

  it.each([
    { credentialStore: "unknown" }, { ownComputer: null }, { ownComputer: { userId: 4, atMs: 1 } },
    { ownComputer: { userId: "", atMs: 1 } }, { windowConfirm: "true" }, { pairingOwnerId: "" },
  ])("fails closed on corrupt window fields %j", (fields) => {
    openDeviceRegistry(dir);
    const path = join(dir, "device-registry.json");
    const data = JSON.parse(readFileSync(path, "utf8"));
    data.devices = [{ ...eligible, ...fields }];
    writeFileSync(path, JSON.stringify(data));
    expect(() => openDeviceRegistry(dir)).toThrow(RegistryCorruptError);
  });
});
