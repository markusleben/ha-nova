import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  digestSecret,
  generateCredential,
  parseCredential,
} from "../../nova/src/security/device-credential.js";
import {
  MAX_ACTIVE_DEVICES,
  MAX_PAIRING_RESPONSES,
  MAX_PENDING_DEVICES,
  PENDING_TTL_MS,
  openDeviceRegistry,
} from "../../nova/src/security/device-registry.js";

let dir: string;
beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-registry-"));
});
afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

const base = (over: Partial<Record<string, string>> = {}) => ({
  deviceId: over.deviceId ?? generateCredential().deviceId,
  secretDigest: over.secretDigest ?? digestSecret("s"),
  clientInstallId: over.clientInstallId ?? "install-1",
  name: over.name ?? "MacBook",
  platform: over.platform ?? "darwin",
  client: over.client ?? "claude",
  createdAtMs: 1000,
});

describe("device-credential", () => {
  it("round-trips generate -> parse and matches the stored digest", () => {
    const g = generateCredential();
    const parsed = parseCredential(g.credential);
    expect(parsed).not.toBeNull();
    expect(parsed?.deviceId).toBe(g.deviceId);
    expect(digestSecret(parsed!.secret)).toBe(g.secretDigest);
  });

  it("rejects malformed credentials", () => {
    for (const bad of [
      "",
      "x",
      "hanova-dev-v1.short.secret",
      "wrong.aaa.bbb",
      "hanova-dev-v1..",
      `hanova-dev-v1.${"A".repeat(22)}.${"A".repeat(43)}.extra`,
    ]) {
      expect(parseCredential(bad)).toBeNull();
    }
  });
});

describe("device-registry", () => {
  it("creates a pending device, activates it, then resolves auth", () => {
    const reg = openDeviceRegistry(dir);
    const cred = generateCredential();
    reg.createPending(
      base({ deviceId: cred.deviceId, secretDigest: cred.secretDigest }),
      1000,
    );
    expect(reg.list()).toHaveLength(1);
    // Not authenticable while pending.
    expect(
      reg.resolveDeviceSecret(cred.deviceId, cred.secretDigest, 1000),
    ).toBeNull();
    reg.activate(cred.deviceId, 1000);
    expect(
      reg.resolveDeviceSecret(cred.deviceId, cred.secretDigest, 1000),
    ).toEqual({ kind: "device", deviceId: cred.deviceId });
    // Wrong secret rejected.
    expect(
      reg.resolveDeviceSecret(cred.deviceId, digestSecret("other"), 1000),
    ).toBeNull();
  });

  it("activation is idempotent", () => {
    const reg = openDeviceRegistry(dir);
    const c = generateCredential();
    reg.createPending(
      base({ deviceId: c.deviceId, secretDigest: c.secretDigest }),
      1000,
    );
    const a1 = reg.activate(c.deviceId, 1000);
    const a2 = reg.activate(c.deviceId, 2000);
    expect(a1.deviceId).toBe(a2.deviceId);
    expect(reg.list().filter((d) => d.state === "active")).toHaveLength(1);
  });

  it("re-pairing keeps the old active credential until the new one activates", () => {
    const reg = openDeviceRegistry(dir);
    const old = generateCredential();
    reg.createPending(
      base({ deviceId: old.deviceId, secretDigest: old.secretDigest }),
      1000,
    );
    reg.activate(old.deviceId, 1000);

    const fresh = generateCredential();
    reg.createPending(
      base({ deviceId: fresh.deviceId, secretDigest: fresh.secretDigest }),
      2000,
    );
    // Old still works while the new one is only pending.
    expect(
      reg.resolveDeviceSecret(old.deviceId, old.secretDigest, 2000),
    ).not.toBeNull();

    reg.activate(fresh.deviceId, 2000);
    // After activation the same install's old active record is retired.
    expect(
      reg.resolveDeviceSecret(old.deviceId, old.secretDigest, 2000),
    ).toBeNull();
    expect(
      reg.resolveDeviceSecret(fresh.deviceId, fresh.secretDigest, 2000),
    ).not.toBeNull();
  });

  it("expires pending credentials after the TTL", () => {
    const reg = openDeviceRegistry(dir);
    const c = generateCredential();
    reg.createPending(
      base({ deviceId: c.deviceId, secretDigest: c.secretDigest }),
      1000,
    );
    expect(() => reg.activate(c.deviceId, 1000 + PENDING_TTL_MS + 1)).toThrow();
  });

  it("allows re-pairing an existing install at exactly the active cap", () => {
    const reg = openDeviceRegistry(dir);
    // Fill to the active cap, each a distinct install.
    let firstCred = generateCredential();
    for (let i = 0; i < MAX_ACTIVE_DEVICES; i++) {
      const c = i === 0 ? firstCred : generateCredential();
      if (i === 0) firstCred = c;
      reg.createPending(
        base({
          deviceId: c.deviceId,
          secretDigest: c.secretDigest,
          clientInstallId: `install-${i}`,
        }),
        1000,
      );
      reg.activate(c.deviceId, 1000);
    }
    expect(reg.list().filter((d) => d.state === "active")).toHaveLength(
      MAX_ACTIVE_DEVICES,
    );
    expect(
      reg.resolveDeviceSecret(firstCred.deviceId, firstCred.secretDigest, 1000),
    ).not.toBeNull();

    // Re-pair install-0: a replacement, net count unchanged — must succeed.
    const fresh = generateCredential();
    reg.createPending(
      base({
        deviceId: fresh.deviceId,
        secretDigest: fresh.secretDigest,
        clientInstallId: "install-0",
      }),
      2000,
    );
    expect(() => reg.activate(fresh.deviceId, 2000)).not.toThrow();
    expect(reg.list().filter((d) => d.state === "active")).toHaveLength(
      MAX_ACTIVE_DEVICES,
    );
    // Old credential of install-0 retired; the new one works.
    expect(
      reg.resolveDeviceSecret(firstCred.deviceId, firstCred.secretDigest, 2000),
    ).toBeNull();
    expect(
      reg.resolveDeviceSecret(fresh.deviceId, fresh.secretDigest, 2000),
    ).not.toBeNull();

    // But a genuinely NEW install past the cap is rejected up front — at
    // createPending, BEFORE /pair/v1/finish would consume the owner's one-time
    // code — rather than being deferred to activation (which would leave the code
    // spent and the CLI unable to finish).
    const overflow = generateCredential();
    expect(() =>
      reg.createPending(
        base({
          deviceId: overflow.deviceId,
          secretDigest: overflow.secretDigest,
          clientInstallId: "install-new",
        }),
        2000,
      ),
    ).toThrow(/active device limit/);
    // Nothing was recorded for the rejected install.
    expect(reg.list().some((d) => d.deviceId === overflow.deviceId)).toBe(
      false,
    );
  });

  it("enforces the pending cap", () => {
    const reg = openDeviceRegistry(dir);
    for (let i = 0; i < MAX_PENDING_DEVICES; i++) {
      const c = generateCredential();
      reg.createPending(
        base({ deviceId: c.deviceId, secretDigest: c.secretDigest }),
        1000,
      );
    }
    const extra = generateCredential();
    expect(() =>
      reg.createPending(
        base({ deviceId: extra.deviceId, secretDigest: extra.secretDigest }),
        1000,
      ),
    ).toThrow();
  });

  it("never evicts a replay response while its credential is still pending", () => {
    const reg = openDeviceRegistry(dir);
    const protectedCredential = generateCredential();
    reg.createPendingWithResponse(
      base({
        deviceId: protectedCredential.deviceId,
        secretDigest: protectedCredential.secretDigest,
        clientInstallId: "protected-install",
      }),
      {
        handshakeId: "protected-handshake",
        contextKey: "local",
        deviceId: protectedCredential.deviceId,
        ke3Digest: "protected-digest",
        ciphertextB64: "protected-ciphertext",
      },
      1000,
    );

    for (let i = 0; i < MAX_PAIRING_RESPONSES + 5; i++) {
      const credential = generateCredential();
      reg.createPendingWithResponse(
        base({
          deviceId: credential.deviceId,
          secretDigest: credential.secretDigest,
          clientInstallId: `activated-install-${i}`,
        }),
        {
          handshakeId: `activated-handshake-${i}`,
          contextKey: "local",
          deviceId: credential.deviceId,
          ke3Digest: `digest-${i}`,
          ciphertextB64: `ciphertext-${i}`,
        },
        1000,
      );
      reg.activate(credential.deviceId, 1000);
    }

    expect(
      reg.getPairingResponse("protected-handshake", "local", 1000),
    ).toEqual({
      ke3Digest: "protected-digest",
      ciphertextB64: "protected-ciphertext",
    });
    expect(
      reg.getPairingResponse("activated-handshake-0", "local", 1000),
    ).toBeNull();
    expect(
      reg.getPairingResponse(
        `activated-handshake-${MAX_PAIRING_RESPONSES + 4}`,
        "local",
        1000,
      ),
    ).not.toBeNull();
  });

  it("rejects a response linked to a different pending credential", () => {
    const reg = openDeviceRegistry(dir);
    const credential = generateCredential();
    expect(() =>
      reg.createPendingWithResponse(
        base({
          deviceId: credential.deviceId,
          secretDigest: credential.secretDigest,
        }),
        {
          handshakeId: "handshake",
          contextKey: "local",
          deviceId: "different-device",
          ke3Digest: "digest",
          ciphertextB64: "ciphertext",
        },
        1000,
      ),
    ).toThrow(/does not match/);
    expect(reg.list()).toHaveLength(0);
  });
});
