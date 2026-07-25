import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  digestSecret,
  generateCredential,
} from "../../nova/src/security/device-credential.js";
import { openDeviceRegistry } from "../../nova/src/security/device-registry.js";
import { CLOUD_REVOCATION_TTL_MS } from "../../nova/src/security/device-registry-cloud.js";

const RELAY_ID = "hanova-relay-v1.AAAAAAAAAAAAAAAAAAAAAA";
const OTHER_RELAY_ID = "hanova-relay-v1.BBBBBBBBBBBBBBBBBBBBBB";

let dir: string;

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-cloud-registry-revoke-"));
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

describe("Cloud device registry revocation", () => {
  it("revokes only the exact Cloud identity and persists an idempotent restart replay", () => {
    const registry = openDeviceRegistry(dir);
    const credential = generateCredential();
    const replacement = generateCredential();
    const absent = generateCredential();
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
    registry.createPending(
      {
        ...base(replacement.deviceId, replacement.secretDigest),
        cloudUserId: "user-1",
        cloudRelayInstanceId: RELAY_ID,
      },
      1000,
    );

    expect(
      registry.revokeCloudDevice(
        absent.deviceId,
        absent.secretDigest,
        "user-1",
        RELAY_ID,
        1000,
      ),
    ).toEqual({ ok: true, deviceId: absent.deviceId, changed: false });

    expect(
      registry.revokeCloudDevice(
        credential.deviceId,
        credential.secretDigest,
        "user-2",
        RELAY_ID,
        1000,
      ),
    ).toEqual({ ok: false, reason: "unknown" });
    expect(
      registry.revokeCloudDevice(
        credential.deviceId,
        credential.secretDigest,
        "user-1",
        OTHER_RELAY_ID,
        1000,
      ),
    ).toEqual({ ok: false, reason: "unknown" });
    expect(
      registry.revokeCloudDevice(
        credential.deviceId,
        digestSecret("wrong-secret"),
        "user-1",
        RELAY_ID,
        1000,
      ),
    ).toEqual({ ok: false, reason: "unknown" });
    expect(registry.list()).toHaveLength(2);

    expect(
      registry.revokeCloudDevice(
        credential.deviceId,
        credential.secretDigest,
        "user-1",
        RELAY_ID,
        1000,
      ),
    ).toEqual({ ok: true, deviceId: credential.deviceId, changed: true });
    expect(registry.list()).toEqual([]);

    const reopened = openDeviceRegistry(dir);
    expect(
      reopened.revokeCloudDevice(
        credential.deviceId,
        credential.secretDigest,
        "user-1",
        RELAY_ID,
        1001,
      ),
    ).toEqual({ ok: true, deviceId: credential.deviceId, changed: false });
    expect(
      reopened.revokeCloudDevice(
        credential.deviceId,
        credential.secretDigest,
        "user-2",
        RELAY_ID,
        1001,
      ),
    ).toEqual({ ok: false, reason: "unknown" });
    expect(
      reopened.revokeCloudDevice(
        credential.deviceId,
        credential.secretDigest,
        "user-1",
        OTHER_RELAY_ID,
        1001,
      ),
    ).toEqual({ ok: false, reason: "unknown" });
    expect(
      reopened.revokeCloudDevice(
        credential.deviceId,
        digestSecret("wrong-secret"),
        "user-1",
        RELAY_ID,
        1001,
      ),
    ).toEqual({ ok: false, reason: "unknown" });
    expect(
      reopened.revokeCloudDevice(
        credential.deviceId,
        digestSecret("wrong-secret"),
        "user-2",
        OTHER_RELAY_ID,
        1000 + CLOUD_REVOCATION_TTL_MS + 1,
      ),
    ).toEqual({
      ok: true,
      deviceId: credential.deviceId,
      changed: false,
    });
  });

  it("revokes an exact Cloud-bound pending device before activation", () => {
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
      registry.revokeCloudDevice(
        credential.deviceId,
        digestSecret("wrong-secret"),
        "user-1",
        RELAY_ID,
        1000,
      ),
    ).toEqual({ ok: false, reason: "unknown" });
    expect(
      registry.revokeCloudDevice(
        credential.deviceId,
        credential.secretDigest,
        "user-1",
        RELAY_ID,
        1000,
      ),
    ).toEqual({
      ok: true,
      deviceId: credential.deviceId,
      changed: true,
    });
    expect(registry.list()).toEqual([]);
  });

  it.each(["before activation", "after activation"] as const)(
    "leaves no active record when uncertain cleanup revokes replacement and current %s",
    (activation) => {
      const registry = openDeviceRegistry(dir);
      const current = generateCredential();
      const replacement = generateCredential();
      registry.createPending(
        base(current.deviceId, current.secretDigest),
        1000,
      );
      registry.activate(current.deviceId, 1000);
      registry.bindCloudUser(
        current.deviceId,
        current.secretDigest,
        "user-1",
        RELAY_ID,
      );
      registry.createPending(
        {
          ...base(replacement.deviceId, replacement.secretDigest),
          cloudUserId: "user-1",
          cloudRelayInstanceId: RELAY_ID,
        },
        1000,
      );
      if (activation === "after activation") {
        registry.activatePendingForCloud(
          replacement.deviceId,
          replacement.secretDigest,
          "user-1",
          RELAY_ID,
          1000,
        );
      }

      expect(
        registry.revokeCloudDevice(
          replacement.deviceId,
          replacement.secretDigest,
          "user-1",
          RELAY_ID,
          1000,
        ),
      ).toMatchObject({ ok: true });
      expect(
        registry.revokeCloudDevice(
          current.deviceId,
          current.secretDigest,
          "user-1",
          RELAY_ID,
          1000,
        ),
      ).toMatchObject({ ok: true });
      expect(registry.list()).toEqual([]);
    },
  );
});
