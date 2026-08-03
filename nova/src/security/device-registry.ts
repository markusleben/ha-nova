import { renameSync } from "node:fs";
import { join } from "node:path";

import { writeFileAtomicSync } from "../storage/atomic-file.js";
import {
  DEVICE_REGISTRY_FILE,
  RegistryCorruptError,
  loadRegistryData,
  type RegistryData,
} from "./device-registry-schema.js";
import { withCloudDeviceRevoked } from "./device-registry-cloud.js";
import type { DeviceRecord, DeviceRegistry } from "./device-registry-types.js";
import { digestsEqual } from "./device-credential.js";
import {
  PENDING_TTL_MS,
  isValidCloudBinding,
  pendingExpired,
  trimPairingResponses,
  withActivated,
  withLastUsedTouch,
  withPending,
} from "./device-registry-mutations.js";

// Private, versioned device registry under /data. Device secrets appear only as
// SHA-256 digests or inside OPAQUE-session-sealed finish responses, never as
// plaintext. All mutations use one atomic, durable write. Corruption fails
// closed and recovery requires a deliberate owner reset.

export {
  LAST_USED_PERSIST_WINDOW_MS,
  MAX_ACTIVE_DEVICES,
  MAX_PAIRING_RESPONSES,
  MAX_PENDING_DEVICES,
  PENDING_TTL_MS,
} from "./device-registry-mutations.js";
export { RegistryCorruptError } from "./device-registry-schema.js";
export type {
  CloudBindingResult,
  CloudDeviceRevocationResult,
  CloudRevocationRecord,
  DeviceRecord,
  DeviceRegistry,
  DeviceState,
  LegacyRecord,
  PairingResponseRecord,
  ResolvedPrincipal,
} from "./device-registry-types.js";

// Moves a corrupt registry file aside so a fresh one can be created. Used by the
// owner-triggered reset: the damaged file is preserved (for forensics) under a
// timestamped name rather than deleted. Best-effort — a subsequent
// openDeviceRegistry surfaces any remaining problem.
export function archiveCorruptRegistry(dataDir: string, now: number): void {
  const path = join(dataDir, DEVICE_REGISTRY_FILE);
  try {
    renameSync(path, `${path}.corrupt-${now}`);
  } catch {
    // Already gone or unmovable; the fresh open decides the outcome.
  }
}

// Loads the registry (fail-closed on corruption) or starts an empty one when the
// file is absent. An absent file is a fresh install; a present-but-unparseable
// file is corruption and must not be masked.
export function openDeviceRegistry(dataDir: string): DeviceRegistry {
  const path = join(dataDir, DEVICE_REGISTRY_FILE);
  let data = loadRegistryData(path);
  let mutating = false;

  function persist(next: RegistryData): void {
    // Single-writer gate: mutations are synchronous and never re-enter, but the
    // flag turns any accidental nesting into a loud failure rather than a lost
    // write.
    if (mutating) {
      throw new Error("registry mutation re-entered");
    }
    mutating = true;
    try {
      writeFileAtomicSync(path, JSON.stringify(next));
      data = next;
    } finally {
      mutating = false;
    }
  }

  // Records a successful device authentication (throttling lives in
  // withLastUsedTouch).
  function touchLastUsed(device: DeviceRecord, now: number): void {
    const next = withLastUsedTouch(data, device, now);
    if (next === null) {
      return;
    }
    try {
      persist(next);
    } catch {
      // Auth must survive a broken disk (ENOSPC/EROFS): the stamp is
      // best-effort metadata, never worth failing authentication over. Keep
      // the fresh value in memory so retries stay throttled to once per
      // window; the durable write catches up once /data is writable again.
      data = next;
    }
  }

  return {
    list() {
      return data.devices.map((d) => ({ ...d }));
    },
    hasLegacy() {
      return data.legacy !== null;
    },
    legacyImportCompleted() {
      return data.legacyImportCompleted;
    },
    resolveDeviceSecret(deviceId, secretDigest, now) {
      const device = data.devices.find(
        (d) => d.deviceId === deviceId && d.state === "active",
      );
      if (!device || !digestsEqual(device.secretDigest, secretDigest)) {
        return null;
      }
      touchLastUsed(device, now);
      return { kind: "device", deviceId };
    },
    resolveCloudDeviceSecret(
      deviceId,
      secretDigest,
      userId,
      relayInstanceId,
      now,
    ) {
      const device = data.devices.find(
        (d) => d.deviceId === deviceId && d.state === "active",
      );
      if (!device) {
        return null;
      }
      const secretMatches = digestsEqual(device.secretDigest, secretDigest);
      if (
        !secretMatches ||
        device.cloudUserId !== userId ||
        device.cloudRelayInstanceId !== relayInstanceId
      ) {
        return null;
      }
      touchLastUsed(device, now);
      return { kind: "device", deviceId };
    },
    resolveLegacySecret(secretDigest) {
      if (
        !data.legacy ||
        !digestsEqual(data.legacy.secretDigest, secretDigest)
      ) {
        return null;
      }
      return { kind: "legacy" };
    },
    getPairingResponse(handshakeId, contextKey, now) {
      const response = data.pairingResponses.find(
        (entry) =>
          entry.handshakeId === handshakeId &&
          entry.contextKey === contextKey &&
          entry.expiresAtMs > now,
      );
      return response === undefined
        ? null
        : {
            ke3Digest: response.ke3Digest,
            ciphertextB64: response.ciphertextB64,
          };
    },
    createPending(record, now) {
      persist(withPending(data, record, now));
    },
    createPendingWithResponse(record, response, now) {
      if (response.deviceId !== record.deviceId) {
        throw new Error(
          "pairing response device does not match pending credential",
        );
      }
      const next = withPending(data, record, now);
      const pairingResponses = next.pairingResponses.filter(
        (entry) =>
          entry.expiresAtMs > now &&
          !(
            entry.handshakeId === response.handshakeId &&
            entry.contextKey === response.contextKey
          ),
      );
      pairingResponses.push({ ...response, expiresAtMs: now + PENDING_TTL_MS });
      persist({
        ...next,
        pairingResponses: trimPairingResponses(pairingResponses, next.devices),
      });
    },
    activate(deviceId, now) {
      const target = data.devices.find((d) => d.deviceId === deviceId);
      if (target?.state === "pending" && target.cloudUserId !== undefined) {
        throw new Error("cloud pairing requires cloud activation");
      }
      return doActivate(deviceId, now);
    },
    activatePending(deviceId, secretDigest, now) {
      const target = data.devices.find((d) => d.deviceId === deviceId);
      if (!target || !digestsEqual(target.secretDigest, secretDigest)) {
        return null; // unknown device or wrong secret
      }
      if (target.state === "pending" && pendingExpired(target, now)) {
        return null; // expired pending is not activatable
      }
      if (target.state === "pending" && target.cloudUserId !== undefined) {
        return null; // Cloud provenance cannot be bypassed through local TLS
      }
      return doActivate(deviceId, now);
    },
    bindCloudUser(deviceId, secretDigest, userId, relayInstanceId) {
      if (!isValidCloudBinding(userId, relayInstanceId)) {
        throw new Error("invalid cloud binding");
      }
      const target = data.devices.find(
        (d) => d.deviceId === deviceId && d.state === "active",
      );
      if (!target || !digestsEqual(target.secretDigest, secretDigest)) {
        return { ok: false, reason: "unknown" };
      }
      if (
        target.cloudUserId !== undefined &&
        (target.cloudUserId !== userId ||
          target.cloudRelayInstanceId !== relayInstanceId)
      ) {
        return { ok: false, reason: "conflict" };
      }
      if (
        target.cloudUserId === userId &&
        target.cloudRelayInstanceId === relayInstanceId
      ) {
        return { ok: true, record: { ...target }, changed: false };
      }
      const bound = {
        ...target,
        cloudUserId: userId,
        cloudRelayInstanceId: relayInstanceId,
      };
      persist({
        ...data,
        devices: data.devices.map((d) => (d.deviceId === deviceId ? bound : d)),
      });
      return { ok: true, record: { ...bound }, changed: true };
    },
    activatePendingForCloud(
      deviceId,
      secretDigest,
      userId,
      relayInstanceId,
      now,
    ) {
      if (!isValidCloudBinding(userId, relayInstanceId)) {
        throw new Error("invalid cloud binding");
      }
      const target = data.devices.find((d) => d.deviceId === deviceId);
      if (!target || !digestsEqual(target.secretDigest, secretDigest)) {
        return { ok: false, reason: "unknown" };
      }
      if (target.state === "pending" && pendingExpired(target, now)) {
        return { ok: false, reason: "unknown" };
      }
      if (
        target.cloudUserId !== userId ||
        target.cloudRelayInstanceId !== relayInstanceId
      ) {
        return { ok: false, reason: "conflict" };
      }
      const changed = target.state !== "active";
      return {
        ok: true,
        record: doActivate(deviceId, now, { userId, relayInstanceId }),
        changed,
      };
    },
    revokeCloudDevice(deviceId, secretDigest, userId, relayInstanceId, now) {
      if (!isValidCloudBinding(userId, relayInstanceId)) {
        throw new Error("invalid cloud binding");
      }
      const mutation = withCloudDeviceRevoked(
        data,
        deviceId,
        secretDigest,
        userId,
        relayInstanceId,
        now,
      );
      if (mutation.data !== data) {
        persist(mutation.data);
      }
      return mutation.result;
    },
    revoke(deviceId) {
      const target = data.devices.find((d) => d.deviceId === deviceId);
      if (!target) {
        return false;
      }
      // Drop the device AND any pending re-pair from the SAME install: otherwise
      // that pending credential could be activated a moment later and restore the
      // access the owner just revoked.
      const next = data.devices.filter(
        (d) =>
          d.deviceId !== deviceId &&
          !(
            d.state === "pending" &&
            d.clientInstallId === target.clientInstallId
          ),
      );
      persist({ ...data, devices: next });
      return true;
    },
    importLegacy(secretDigest, now) {
      if (data.legacyImportCompleted) {
        return; // tombstone wins; never re-import
      }
      persist({
        ...data,
        legacy: { secretDigest, createdAtMs: now },
        legacyImportCompleted: true,
      });
    },
    markLegacyMigrated() {
      // Stamp the tombstone WITHOUT importing a record, so an owner reset of a
      // corrupt registry cuts pre-pairing shared access instead of re-importing
      // the lingering plaintext token on the next boot.
      if (data.legacyImportCompleted) {
        return;
      }
      persist({ ...data, legacyImportCompleted: true });
    },
    revokeLegacy() {
      persist({ ...data, legacy: null });
    },
  };

  function doActivate(
    deviceId: string,
    now: number,
    cloudBinding?: { userId: string; relayInstanceId: string },
  ): DeviceRecord {
    const activated = withActivated(data, deviceId, now, cloudBinding);
    if (activated.changed) {
      persist(activated.data);
    }
    return activated.record;
  }
}
