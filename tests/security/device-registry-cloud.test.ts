import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
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

const RELAY_ID = "hanova-relay-v1.AAAAAAAAAAAAAAAAAAAAAA";
const OTHER_RELAY_ID = "hanova-relay-v1.BBBBBBBBBBBBBBBBBBBBBB";

let dir: string;

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-cloud-registry-"));
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

describe("Cloud device registry binding", () => {
  it("binds an active device to one HA user and the current Relay instance", () => {
    const registry = openDeviceRegistry(dir);
    const credential = generateCredential();
    registry.createPending(
      base(credential.deviceId, credential.secretDigest),
      1000,
    );
    registry.activate(credential.deviceId, 1000);

    expect(
      registry.resolveCloudDeviceSecret(
        credential.deviceId,
        credential.secretDigest,
        "user-1",
        RELAY_ID,
        1000,
      ),
    ).toBeNull();
    expect(
      registry.bindCloudUser(
        credential.deviceId,
        credential.secretDigest,
        "user-1",
        RELAY_ID,
      ),
    ).toMatchObject({ ok: true, changed: true });
    expect(
      registry.bindCloudUser(
        credential.deviceId,
        credential.secretDigest,
        "user-1",
        RELAY_ID,
      ),
    ).toMatchObject({ ok: true, changed: false });
    expect(
      registry.bindCloudUser(
        credential.deviceId,
        credential.secretDigest,
        "user-2",
        RELAY_ID,
      ),
    ).toEqual({ ok: false, reason: "conflict" });

    expect(
      registry.resolveCloudDeviceSecret(
        credential.deviceId,
        credential.secretDigest,
        "user-1",
        RELAY_ID,
        1000,
      ),
    ).not.toBeNull();
    expect(
      registry.resolveCloudDeviceSecret(
        credential.deviceId,
        credential.secretDigest,
        "user-2",
        RELAY_ID,
        1000,
      ),
    ).toBeNull();
    expect(
      registry.resolveCloudDeviceSecret(
        credential.deviceId,
        credential.secretDigest,
        "user-1",
        OTHER_RELAY_ID,
        1000,
      ),
    ).toBeNull();
    expect(openDeviceRegistry(dir).list()[0]).toMatchObject({
      cloudUserId: "user-1",
      cloudRelayInstanceId: RELAY_ID,
    });
  });

  it("requires explicit re-pairing when the Relay identity changes", () => {
    const registry = openDeviceRegistry(dir);
    const credential = generateCredential();
    registry.createPending(
      base(credential.deviceId, credential.secretDigest),
      1000,
    );
    registry.activate(credential.deviceId, 1000);
    registry.bindCloudUser(
      credential.deviceId,
      credential.secretDigest,
      "user-1",
      RELAY_ID,
    );

    expect(
      registry.bindCloudUser(
        credential.deviceId,
        credential.secretDigest,
        "user-1",
        OTHER_RELAY_ID,
      ),
    ).toEqual({ ok: false, reason: "conflict" });
    expect(
      registry.resolveCloudDeviceSecret(
        credential.deviceId,
        credential.secretDigest,
        "user-1",
        RELAY_ID,
        1000,
      ),
    ).not.toBeNull();
    expect(
      registry.resolveCloudDeviceSecret(
        credential.deviceId,
        credential.secretDigest,
        "user-1",
        OTHER_RELAY_ID,
        1000,
      ),
    ).toBeNull();
  });

  it("activates only through the exact Cloud identity persisted at pairing", () => {
    const registry = openDeviceRegistry(dir);
    const credential = generateCredential();
    registry.createPending(
      {
        ...base(credential.deviceId, credential.secretDigest),
        cloudUserId: "user-1",
        cloudRelayInstanceId: RELAY_ID,
      },
      1000,
    );

    expect(
      registry.activatePending(
        credential.deviceId,
        credential.secretDigest,
        1000,
      ),
    ).toBeNull();
    expect(() => registry.activate(credential.deviceId, 1000)).toThrow(
      /cloud activation/,
    );
    expect(
      registry.activatePendingForCloud(
        credential.deviceId,
        credential.secretDigest,
        "user-2",
        RELAY_ID,
        1000,
      ),
    ).toEqual({ ok: false, reason: "conflict" });
    expect(
      registry.activatePendingForCloud(
        credential.deviceId,
        credential.secretDigest,
        "user-1",
        OTHER_RELAY_ID,
        1000,
      ),
    ).toEqual({ ok: false, reason: "conflict" });
    expect(registry.list()[0]?.state).toBe("pending");

    expect(
      registry.activatePendingForCloud(
        credential.deviceId,
        credential.secretDigest,
        "user-1",
        RELAY_ID,
        1000,
      ),
    ).toMatchObject({ ok: true, changed: true });
    expect(
      registry.activatePendingForCloud(
        credential.deviceId,
        credential.secretDigest,
        "user-1",
        RELAY_ID,
        1000,
      ),
    ).toMatchObject({ ok: true, changed: false });
    expect(
      registry.activatePendingForCloud(
        credential.deviceId,
        credential.secretDigest,
        "user-2",
        RELAY_ID,
        1000,
      ),
    ).toEqual({ ok: false, reason: "conflict" });
    expect(
      registry.activatePendingForCloud(
        credential.deviceId,
        credential.secretDigest,
        "user-1",
        OTHER_RELAY_ID,
        1000,
      ),
    ).toEqual({ ok: false, reason: "conflict" });
    expect(
      openDeviceRegistry(dir).resolveCloudDeviceSecret(
        credential.deviceId,
        credential.secretDigest,
        "user-1",
        RELAY_ID,
        1000,
      ),
    ).not.toBeNull();
  });

  it("rejects bad secrets and invalid binding identities before mutation", () => {
    const registry = openDeviceRegistry(dir);
    const credential = generateCredential();
    registry.createPending(
      base(credential.deviceId, credential.secretDigest),
      1000,
    );
    registry.activate(credential.deviceId, 1000);

    expect(
      registry.bindCloudUser(
        credential.deviceId,
        digestSecret("wrong"),
        "user-1",
        RELAY_ID,
      ),
    ).toEqual({ ok: false, reason: "unknown" });
    expect(() =>
      registry.bindCloudUser(
        credential.deviceId,
        credential.secretDigest,
        "user-1",
        "invalid",
      ),
    ).toThrow(/invalid cloud binding/);
    expect(registry.list()[0]?.cloudUserId).toBeUndefined();
  });

  it("fails closed on incomplete or invalid persisted Cloud bindings", () => {
    const record = {
      deviceId: "x",
      secretDigest: "d",
      state: "active",
      clientInstallId: "i",
      name: "n",
      platform: "p",
      client: "c",
      createdAtMs: 1,
      cloudUserId: "user-1",
    };
    writeFileSync(
      join(dir, "device-registry.json"),
      JSON.stringify({
        version: 1,
        devices: [record],
        legacy: null,
      }),
    );
    expect(() => openDeviceRegistry(dir)).toThrow(RegistryCorruptError);

    writeFileSync(
      join(dir, "device-registry.json"),
      JSON.stringify({
        version: 1,
        devices: [{ ...record, cloudRelayInstanceId: "wrong-instance" }],
        legacy: null,
      }),
    );
    expect(() => openDeviceRegistry(dir)).toThrow(RegistryCorruptError);
  });

  it("reloads a complete Cloud-bound pending record and rejects incomplete provenance", () => {
    const pending = {
      deviceId: "x",
      secretDigest: "d",
      state: "pending",
      clientInstallId: "i",
      name: "n",
      platform: "p",
      client: "c",
      createdAtMs: 1,
      pendingExpiresAtMs: 10_000,
      cloudUserId: "user-1",
      cloudRelayInstanceId: RELAY_ID,
    };
    writeFileSync(
      join(dir, "device-registry.json"),
      JSON.stringify({
        version: 1,
        devices: [pending],
        legacy: null,
      }),
    );
    expect(openDeviceRegistry(dir).list()[0]).toMatchObject(pending);

    writeFileSync(
      join(dir, "device-registry.json"),
      JSON.stringify({
        version: 1,
        devices: [{ ...pending, cloudRelayInstanceId: undefined }],
        legacy: null,
      }),
    );
    expect(() => openDeviceRegistry(dir)).toThrow(RegistryCorruptError);
  });
});
