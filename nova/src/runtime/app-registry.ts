import type { DeviceRegistry } from "../security/device-registry.js";

// A no-op registry used only when the on-disk registry is corrupt: every read
// is empty and every mutation throws, so device auth and pairing fail closed
// while the owner page stays reachable to trigger a reset.
export function openInertRegistry(): DeviceRegistry {
  const nope = (): never => {
    throw new Error("device registry is corrupt");
  };
  return {
    list: () => [],
    hasLegacy: () => false,
    legacyImportCompleted: () => false,
    resolveDeviceSecret: () => null,
    resolveCloudDeviceSecret: () => null,
    resolveLegacySecret: () => null,
    getPairingResponse: () => null,
    createPending: nope,
    createPendingWithResponse: nope,
    activate: nope,
    activatePending: () => null,
    bindCloudUser: () => ({ ok: false, reason: "unknown" }),
    activatePendingForCloud: () => ({ ok: false, reason: "unknown" }),
    revokeCloudDevice: () => ({ ok: false, reason: "unknown" }),
    revoke: () => false,
    setConfirmPhone: () => false,
    importLegacy: nope,
    markLegacyMigrated: nope,
    revokeLegacy: nope,
  };
}

// A stable DeviceRegistry facade that always delegates to the current store, so
// a corrupt-registry reset can swap the underlying registry without rebuilding
// the listeners and pairing manager that captured this reference.
export function swappableRegistry(get: () => DeviceRegistry): DeviceRegistry {
  return {
    list: () => get().list(),
    hasLegacy: () => get().hasLegacy(),
    legacyImportCompleted: () => get().legacyImportCompleted(),
    resolveDeviceSecret: (deviceId, secretDigest, now) =>
      get().resolveDeviceSecret(deviceId, secretDigest, now),
    resolveCloudDeviceSecret: (
      deviceId,
      secretDigest,
      userId,
      relayInstanceId,
      now,
    ) =>
      get().resolveCloudDeviceSecret(
        deviceId,
        secretDigest,
        userId,
        relayInstanceId,
        now,
      ),
    resolveLegacySecret: (secretDigest) =>
      get().resolveLegacySecret(secretDigest),
    getPairingResponse: (handshakeId, contextKey, now) =>
      get().getPairingResponse(handshakeId, contextKey, now),
    createPending: (record, now) => get().createPending(record, now),
    createPendingWithResponse: (record, response, now) =>
      get().createPendingWithResponse(record, response, now),
    activate: (deviceId, now) => get().activate(deviceId, now),
    activatePending: (deviceId, secretDigest, now) =>
      get().activatePending(deviceId, secretDigest, now),
    bindCloudUser: (deviceId, secretDigest, userId, relayInstanceId) =>
      get().bindCloudUser(deviceId, secretDigest, userId, relayInstanceId),
    activatePendingForCloud: (
      deviceId,
      secretDigest,
      userId,
      relayInstanceId,
      now,
    ) =>
      get().activatePendingForCloud(
        deviceId,
        secretDigest,
        userId,
        relayInstanceId,
        now,
      ),
    revokeCloudDevice: (deviceId, secretDigest, userId, relayInstanceId, now) =>
      get().revokeCloudDevice(
        deviceId,
        secretDigest,
        userId,
        relayInstanceId,
        now,
      ),
    revoke: (deviceId) => get().revoke(deviceId),
    setConfirmPhone: (deviceId, link, expected) => get().setConfirmPhone(deviceId, link, expected),
    importLegacy: (secretDigest, now) => get().importLegacy(secretDigest, now),
    markLegacyMigrated: () => get().markLegacyMigrated(),
    revokeLegacy: () => get().revokeLegacy(),
  };
}
