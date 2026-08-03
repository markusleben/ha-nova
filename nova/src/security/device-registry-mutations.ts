import { isRelayInstanceId } from "../storage/relay-instance.js";
import {
  isValidCloudUserId,
  type RegistryData,
} from "./device-registry-schema.js";
import type {
  DeviceRecord,
  PairingResponseRecord,
} from "./device-registry-types.js";

export const MAX_ACTIVE_DEVICES = 64;
export const MAX_PENDING_DEVICES = 16;
export const MAX_PAIRING_RESPONSES = 32;
export const PENDING_TTL_MS = 15 * 60 * 1000;
// Last-used stamps persist at most once per window so the hot auth path does
// not rewrite the registry file on every request; the on-disk value may lag by
// up to the window, which is precise enough for the owner console's access
// review.
// ponytail: fixed 5-minute throttle; make it configurable only if a real
// deployment needs finer freshness.
export const LAST_USED_PERSIST_WINDOW_MS = 5 * 60 * 1000;

// Returns the registry data with the device's last-used stamp refreshed, or
// null while the current stamp is inside the throttle window (nothing to
// persist).
export function withLastUsedTouch(
  data: RegistryData,
  device: DeviceRecord,
  now: number,
): RegistryData | null {
  if (
    device.lastUsedAtMs !== undefined &&
    now - device.lastUsedAtMs < LAST_USED_PERSIST_WINDOW_MS
  ) {
    return null;
  }
  const touched = { ...device, lastUsedAtMs: now };
  return {
    ...data,
    devices: data.devices.map((d) =>
      d.deviceId === device.deviceId ? touched : d,
    ),
  };
}

export function isValidCloudBinding(
  userId: string,
  relayInstanceId: string,
): boolean {
  return isValidCloudUserId(userId) && isRelayInstanceId(relayInstanceId);
}

export function withPending(
  data: RegistryData,
  record: Omit<DeviceRecord, "state" | "pendingExpiresAtMs">,
  now: number,
): RegistryData {
  if (
    (record.cloudUserId === undefined) !==
      (record.cloudRelayInstanceId === undefined) ||
    (record.cloudUserId !== undefined &&
      !isValidCloudBinding(record.cloudUserId, record.cloudRelayInstanceId!))
  ) {
    throw new Error("invalid cloud binding");
  }
  const devices = data.devices.filter((device) => !pendingExpired(device, now));
  if (pendingCountIn(devices, now) >= MAX_PENDING_DEVICES) {
    throw new Error("pending device limit reached");
  }
  const isReplacement = devices.some(
    (device) =>
      device.state === "active" &&
      device.clientInstallId === record.clientInstallId,
  );
  if (!isReplacement && activeCountIn(devices) >= MAX_ACTIVE_DEVICES) {
    throw new Error("active device limit reached");
  }
  return {
    ...data,
    devices: [
      ...devices,
      { ...record, state: "pending", pendingExpiresAtMs: now + PENDING_TTL_MS },
    ],
  };
}

function pendingCountIn(devices: DeviceRecord[], now: number): number {
  return devices.filter(
    (device) => device.state === "pending" && !pendingExpired(device, now),
  ).length;
}

export function activeCountIn(devices: DeviceRecord[]): number {
  return devices.filter((device) => device.state === "active").length;
}

export function trimPairingResponses(
  responses: PairingResponseRecord[],
  devices: DeviceRecord[],
): PairingResponseRecord[] {
  const pendingDeviceIds = new Set(
    devices
      .filter((device) => device.state === "pending")
      .map((device) => device.deviceId),
  );
  const kept = [...responses];
  while (kept.length > MAX_PAIRING_RESPONSES) {
    const evictable = kept.findIndex(
      (response) => !pendingDeviceIds.has(response.deviceId),
    );
    if (evictable < 0) {
      throw new Error(
        "pairing response limit cannot preserve pending credentials",
      );
    }
    kept.splice(evictable, 1);
  }
  return kept;
}

// Pure activation: promotes the install's pending record and retires every
// other record for the SAME install (re-pairing replaces on activation).
// Throws on unknown/expired credentials, provenance mismatches, and the
// active-device cap; `changed: false` marks the idempotent re-activation.
export function withActivated(
  data: RegistryData,
  deviceId: string,
  now: number,
  cloudBinding?: { userId: string; relayInstanceId: string },
): { data: RegistryData; record: DeviceRecord; changed: boolean } {
  const devices = data.devices.filter(
    (d) => !(d.state === "pending" && pendingExpired(d, now)),
  );
  const target = devices.find((d) => d.deviceId === deviceId);
  if (!target) {
    throw new Error("no such pending credential");
  }
  if (target.state === "active") {
    if (
      cloudBinding === undefined ||
      (target.cloudUserId === cloudBinding.userId &&
        target.cloudRelayInstanceId === cloudBinding.relayInstanceId)
    ) {
      return { data, record: { ...target }, changed: false }; // idempotent re-activation
    }
    throw new Error("device is bound to another cloud identity");
  }
  if (
    cloudBinding === undefined
      ? target.cloudUserId !== undefined
      : target.cloudUserId !== cloudBinding.userId ||
        target.cloudRelayInstanceId !== cloudBinding.relayInstanceId
  ) {
    throw new Error("activation route does not match pairing provenance");
  }
  const promoted: DeviceRecord = {
    deviceId: target.deviceId,
    secretDigest: target.secretDigest,
    state: "active",
    clientInstallId: target.clientInstallId,
    name: target.name,
    platform: target.platform,
    client: target.client,
    createdAtMs: target.createdAtMs,
    // Activation is itself a use; seeds the owner console's "last used".
    lastUsedAtMs: now,
    ...(target.cloudUserId !== undefined
      ? {
          cloudUserId: target.cloudUserId,
          cloudRelayInstanceId: target.cloudRelayInstanceId!,
        }
      : {}),
  };
  // Drop the old active credential AND any older pending re-pair from the same
  // install. Leaving a stale same-install pending behind would let someone still
  // holding that older provisional credential activate it moments later and
  // silently replace this freshly promoted one without another owner code —
  // mirroring the same-install cleanup revoke() already performs.
  const kept = devices.filter(
    (d) =>
      d.deviceId !== deviceId && d.clientInstallId !== target.clientInstallId,
  );
  // Count AFTER retiring the same-install active record: re-pairing at the cap
  // is a replacement (net count unchanged) and must be allowed; only a
  // genuinely new install pushing past the cap is rejected.
  if (activeCountIn(kept) >= MAX_ACTIVE_DEVICES) {
    throw new Error("active device limit reached");
  }
  return {
    data: { ...data, devices: [...kept, promoted] },
    record: { ...promoted },
    changed: true,
  };
}

export function pendingExpired(device: DeviceRecord, now: number): boolean {
  return (
    device.state === "pending" &&
    typeof device.pendingExpiresAtMs === "number" &&
    now >= device.pendingExpiresAtMs
  );
}
