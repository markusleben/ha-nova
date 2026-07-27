import { digestsEqual } from "./device-credential.js";
import type { RegistryData } from "./device-registry-schema.js";
import type {
  CloudDeviceRevocationResult,
  CloudRevocationRecord,
} from "./device-registry-types.js";

export const CLOUD_REVOCATION_TTL_MS = 24 * 60 * 60 * 1000;
export const MAX_CLOUD_REVOCATIONS = 128;

export function withCloudDeviceRevoked(
  data: RegistryData,
  deviceId: string,
  secretDigest: string,
  userId: string,
  relayInstanceId: string,
  now: number,
): { data: RegistryData; result: CloudDeviceRevocationResult } {
  const target = data.devices.find(
    (device) =>
      device.deviceId === deviceId &&
      (device.state === "active" || device.state === "pending") &&
      digestsEqual(device.secretDigest, secretDigest) &&
      device.cloudUserId === userId &&
      device.cloudRelayInstanceId === relayInstanceId,
  );
  const sameDeviceId = data.devices.some(
    (device) => device.deviceId === deviceId,
  );
  const liveRevocations = data.cloudRevocations.filter(
    (revocation) => revocation.expiresAtMs > now,
  );
  if (target === undefined) {
    const sameDeviceIdRevocation = liveRevocations.some(
      (revocation) => revocation.deviceId === deviceId,
    );
    const exactReplay = liveRevocations.some(
      (revocation) =>
        revocation.deviceId === deviceId &&
        digestsEqual(revocation.secretDigest, secretDigest) &&
        revocation.cloudUserId === userId &&
        revocation.cloudRelayInstanceId === relayInstanceId,
    );
    return {
      data,
      result:
        !sameDeviceId && (!sameDeviceIdRevocation || exactReplay)
          ? { ok: true, deviceId, changed: false }
          : { ok: false, reason: "unknown" },
    };
  }

  const revocation: CloudRevocationRecord = {
    deviceId,
    secretDigest,
    cloudUserId: userId,
    cloudRelayInstanceId: relayInstanceId,
    revokedAtMs: now,
    expiresAtMs: now + CLOUD_REVOCATION_TTL_MS,
  };
  const distinct = liveRevocations.filter(
    (entry) =>
      entry.deviceId !== deviceId ||
      !digestsEqual(entry.secretDigest, secretDigest) ||
      entry.cloudUserId !== userId ||
      entry.cloudRelayInstanceId !== relayInstanceId,
  );
  distinct.push(revocation);
  const cloudRevocations = distinct.slice(-MAX_CLOUD_REVOCATIONS);
  const devices = data.devices.filter(
    (device) =>
      device.deviceId !== deviceId &&
      !(
        device.state === "pending" &&
        device.clientInstallId === target.clientInstallId
      ),
  );
  return {
    data: { ...data, devices, cloudRevocations },
    result: { ok: true, deviceId, changed: true },
  };
}
