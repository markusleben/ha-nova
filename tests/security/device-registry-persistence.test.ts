import {
  chmodSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  digestSecret,
  generateCredential,
} from "../../nova/src/security/device-credential.js";
import {
  RegistryCorruptError,
  openDeviceRegistry,
} from "../../nova/src/security/device-registry.js";

let dir: string;
beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-registry-persistence-"));
});
afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

const base = (deviceId: string, secretDigest: string) => ({
  deviceId,
  secretDigest,
  clientInstallId: "install-1",
  name: "MacBook",
  platform: "darwin",
  client: "claude",
  createdAtMs: 1000,
});

describe("device-registry persistence and integrity", () => {
  it("persists across reopen and never stores a plaintext secret", () => {
    const cred = generateCredential();
    {
      const reg = openDeviceRegistry(dir);
      reg.createPending(base(cred.deviceId, cred.secretDigest), 1000);
      reg.activate(cred.deviceId, 1000);
    }
    const onDisk = readFileSync(join(dir, "device-registry.json"), "utf8");
    const secret = cred.credential.split(".")[2];
    expect(onDisk).toContain(cred.secretDigest);
    expect(onDisk).not.toContain(secret);

    const reopened = openDeviceRegistry(dir);
    expect(
      reopened.resolveDeviceSecret(cred.deviceId, cred.secretDigest, 2000),
    ).not.toBeNull();
  });

  it("retires the old shared access: a fresh open writes the file with the tombstone", () => {
    const path = join(dir, "device-registry.json");
    openDeviceRegistry(dir);
    expect(JSON.parse(readFileSync(path, "utf8"))).toMatchObject({ legacy: null, legacyImportCompleted: true, devices: [] });
  });

  it("drops a legacy record from an older App at open, keeps the devices, and rewrites the file", () => {
    const path = join(dir, "device-registry.json");
    const cred = generateCredential();
    writeFileSync(
      path,
      JSON.stringify({
        version: 1,
        devices: [{ ...base(cred.deviceId, cred.secretDigest), state: "active" }],
        legacy: { secretDigest: digestSecret("old-shared"), createdAtMs: 1 },
        legacyImportCompleted: true,
      }),
    );
    const reg = openDeviceRegistry(dir);
    expect(reg.list().map((d) => d.deviceId)).toEqual([cred.deviceId]);
    const onDisk = readFileSync(path, "utf8");
    expect(JSON.parse(onDisk)).toMatchObject({ legacy: null, legacyImportCompleted: true });
    expect(onDisk).not.toContain(digestSecret("old-shared"));
  });

  it("keeps the tombstone on every later write, so an older App never imports the shared token again", () => {
    const path = join(dir, "device-registry.json");
    const reg = openDeviceRegistry(dir);
    const cred = generateCredential();
    reg.createPending(base(cred.deviceId, cred.secretDigest), 1000);
    reg.activate(cred.deviceId, 1000);
    expect(JSON.parse(readFileSync(path, "utf8"))).toMatchObject({ legacy: null, legacyImportCompleted: true });
    expect(reg).not.toHaveProperty("importLegacy");
    expect(reg).not.toHaveProperty("resolveLegacySecret");
  });

  it.skipIf(process.getuid?.() === 0)("fails loud when the retiring write fails", () => {
    writeFileSync(join(dir, "device-registry.json"), JSON.stringify({ version: 1, devices: [], legacy: null }));
    chmodSync(dir, 0o500);
    try {
      expect(() => openDeviceRegistry(dir)).toThrow();
    } finally {
      chmodSync(dir, 0o700);
    }
  });

  it("revoking a device also removes an in-flight re-pair from the same install", () => {
    const reg = openDeviceRegistry(dir);
    const rec = (deviceId: string, installId: string) => ({
      ...base(deviceId, "d-" + deviceId),
      clientInstallId: installId,
    });
    reg.createPending(rec("active-dev", "install-1"), 1000);
    reg.activate("active-dev", 1000);
    reg.createPending(rec("pending-dev", "install-1"), 1000);
    reg.createPending(rec("other-pending", "install-2"), 1000);

    expect(reg.revoke("active-dev")).toBe(true);
    const ids = reg.list().map((device) => device.deviceId);
    expect(ids).not.toContain("active-dev");
    expect(ids).not.toContain("pending-dev");
    expect(ids).toContain("other-pending");
  });

  it("activating a re-pair retires an older pending re-pair from the same install", () => {
    const reg = openDeviceRegistry(dir);
    const rec = (deviceId: string, installId: string) => ({
      ...base(deviceId, "d-" + deviceId),
      clientInstallId: installId,
    });
    reg.createPending(rec("v1", "install-1"), 1000);
    reg.activate("v1", 1000);
    reg.createPending(rec("v2", "install-1"), 1000);
    reg.createPending(rec("v3", "install-1"), 1000);
    reg.createPending(rec("other", "install-2"), 1000);

    reg.activate("v3", 2000);
    const ids = reg.list().map((device) => device.deviceId);
    expect(ids).toContain("v3");
    expect(ids).not.toContain("v1");
    expect(ids).not.toContain("v2");
    expect(ids).toContain("other");
    expect(() => reg.activate("v2", 3000)).toThrow(
      /no such pending credential/,
    );
    expect(
      reg
        .list()
        .filter(
          (device) =>
            device.state === "active" && device.clientInstallId === "install-1",
        ),
    ).toHaveLength(1);
  });

  it("fails closed on a corrupt registry (never silently recreates)", () => {
    writeFileSync(join(dir, "device-registry.json"), "{ this is not json");
    expect(() => openDeviceRegistry(dir)).toThrow(RegistryCorruptError);
  });

  it("treats an insecure (symlinked) registry file as recoverable corruption, not a crash", () => {
    symlinkSync(join(dir, "elsewhere"), join(dir, "device-registry.json"));
    expect(() => openDeviceRegistry(dir)).toThrow(RegistryCorruptError);
  });

  it("fails closed on an unknown schema version", () => {
    writeFileSync(
      join(dir, "device-registry.json"),
      JSON.stringify({ version: 99, devices: [], legacy: null }),
    );
    expect(() => openDeviceRegistry(dir)).toThrow(RegistryCorruptError);
  });

  it("fails closed on a duplicate deviceId in a crafted registry", () => {
    const record = (deviceId: string) => ({
      ...base(deviceId, "d"),
      state: "active",
    });
    writeFileSync(
      join(dir, "device-registry.json"),
      JSON.stringify({
        version: 1,
        devices: [record("dup"), record("dup")],
        legacy: null,
      }),
    );
    expect(() => openDeviceRegistry(dir)).toThrow(RegistryCorruptError);
  });

  it("keeps a device's linked phone, and fails closed on a malformed one", () => {
    const write = (confirmPhone: unknown) =>
      writeFileSync(join(dir, "device-registry.json"), JSON.stringify({ version: 1, devices: [{ ...base("x", "d"), state: "active", confirmPhone }], legacy: null }));
    const phone = { service: "mobile_app_iphone", deviceId: "ha-iphone", userId: "u1", platform: "ios" };
    write(phone);
    expect(openDeviceRegistry(dir).list()[0]?.confirmPhone).toEqual(phone);
    for (const bad of [{ ...phone, service: "notify.mobile_app_iphone" }, { ...phone, userId: "" }, { ...phone, platform: 1 }, { ...phone, platform: "windows" }, { ...phone, deviceId: "d".repeat(129) }, "phone"]) {
      write(bad);
      expect(() => openDeviceRegistry(dir)).toThrow(RegistryCorruptError);
    }
  });

  it("sets a phone link only on an active device that still has the expected one, durably", () => {
    writeFileSync(join(dir, "device-registry.json"), JSON.stringify({ version: 1, devices: [{ ...base("x", "d"), state: "active" }, { ...base("p", "e"), state: "pending", pendingExpiresAtMs: Date.now() + 60_000 }], legacy: null }));
    const registry = openDeviceRegistry(dir);
    const phone = { service: "mobile_app_iphone", deviceId: "ha-iphone", userId: "u1", platform: "ios" };
    const pixel = { service: "mobile_app_pixel", deviceId: "ha-pixel", userId: "u2" };
    expect(registry.setConfirmPhone("p", phone, undefined)).toBe(false);
    expect(registry.setConfirmPhone("gone", phone, undefined)).toBe(false);
    expect(registry.setConfirmPhone("x", pixel, phone)).toBe(false); // it has none yet
    expect(registry.setConfirmPhone("x", phone, undefined)).toBe(true);
    expect(registry.setConfirmPhone("x", pixel, undefined)).toBe(false); // it has one now
    expect(registry.setConfirmPhone("x", pixel, { ...phone, platform: "android" })).toBe(false);
    expect(registry.setConfirmPhone("x", pixel, phone)).toBe(true);
    expect(openDeviceRegistry(dir).list().find((d) => d.deviceId === "x")?.confirmPhone).toEqual(pixel);
    expect(() => registry.setConfirmPhone("x", { ...phone, service: "notify.x" }, pixel)).toThrow(RegistryCorruptError);
    expect(openDeviceRegistry(dir).list().find((d) => d.deviceId === "x")?.confirmPhone).toEqual(pixel);
  });

  it("fails closed on a pending record with no expiry (would occupy the cap forever)", () => {
    writeFileSync(
      join(dir, "device-registry.json"),
      JSON.stringify({
        version: 1,
        devices: [
          {
            ...base("x", "d"),
            state: "pending",
          },
        ],
        legacy: null,
      }),
    );
    expect(() => openDeviceRegistry(dir)).toThrow(RegistryCorruptError);
  });

  it("ignores a malformed legacy record instead of failing: the old shared access is gone", () => {
    const path = join(dir, "device-registry.json");
    writeFileSync(path, `{"version":1,"devices":[],"legacy":{"secretDigest":"d","createdAtMs":null}}`);
    openDeviceRegistry(dir);
    expect(JSON.parse(readFileSync(path, "utf8"))).toMatchObject({ legacy: null, legacyImportCompleted: true });
  });
});
