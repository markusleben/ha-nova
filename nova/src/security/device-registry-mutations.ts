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

export function pendingExpired(device: DeviceRecord, now: number): boolean {
  return (
    device.state === "pending" &&
    typeof device.pendingExpiresAtMs === "number" &&
    now >= device.pendingExpiresAtMs
  );
}
