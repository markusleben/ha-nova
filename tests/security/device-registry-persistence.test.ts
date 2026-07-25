import {
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

  it("imports a legacy record once and never re-imports (tombstone)", () => {
    const reg = openDeviceRegistry(dir);
    const legacyDigest = digestSecret("legacy-token");
    reg.importLegacy(legacyDigest, 1000);
    expect(reg.resolveLegacySecret(legacyDigest)).toEqual({ kind: "legacy" });
    reg.importLegacy(digestSecret("different"), 2000);
    expect(reg.resolveLegacySecret(legacyDigest)).toEqual({ kind: "legacy" });
    reg.revokeLegacy();
    expect(reg.resolveLegacySecret(legacyDigest)).toBeNull();
    expect(reg.legacyImportCompleted()).toBe(true);
  });

  it("markLegacyMigrated tombstones without importing a record (a reset cuts legacy)", () => {
    const reg = openDeviceRegistry(dir);
    reg.markLegacyMigrated();
    expect(reg.legacyImportCompleted()).toBe(true);
    expect(reg.hasLegacy()).toBe(false);
    expect(reg.resolveLegacySecret(digestSecret("old-shared"))).toBeNull();
    expect(openDeviceRegistry(dir).legacyImportCompleted()).toBe(true);
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

  it("fails closed on a legacy record with a non-finite timestamp", () => {
    writeFileSync(
      join(dir, "device-registry.json"),
      `{"version":1,"devices":[],"legacy":{"secretDigest":"d","createdAtMs":null}}`,
    );
    expect(() => openDeviceRegistry(dir)).toThrow(RegistryCorruptError);
  });
});
