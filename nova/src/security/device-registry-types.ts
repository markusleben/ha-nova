export type DeviceState = "pending" | "active";

export interface DeviceRecord {
  deviceId: string;
  secretDigest: string;
  state: DeviceState;
  clientInstallId: string;
  name: string;
  platform: string;
  client: string;
  createdAtMs: number;
  pendingExpiresAtMs?: number;
  cloudUserId?: string;
  cloudRelayInstanceId?: string;
}

export interface LegacyRecord {
  secretDigest: string;
  createdAtMs: number;
}

export interface PairingResponseRecord {
  handshakeId: string;
  contextKey: string;
  deviceId: string;
  ke3Digest: string;
  ciphertextB64: string;
  expiresAtMs: number;
}

export interface CloudRevocationRecord {
  deviceId: string;
  secretDigest: string;
  cloudUserId: string;
  cloudRelayInstanceId: string;
  revokedAtMs: number;
  expiresAtMs: number;
}

export interface ResolvedPrincipal {
  kind: "device" | "legacy";
  deviceId?: string;
}

export type CloudBindingResult =
  | { ok: true; record: DeviceRecord; changed: boolean }
  | { ok: false; reason: "unknown" | "conflict" };

export type CloudDeviceRevocationResult =
  | { ok: true; deviceId: string; changed: boolean }
  | { ok: false; reason: "unknown" };

export interface DeviceRegistry {
  list(): DeviceRecord[];
  hasLegacy(): boolean;
  legacyImportCompleted(): boolean;
  resolveDeviceSecret(
    deviceId: string,
    secretDigest: string,
    now: number,
  ): ResolvedPrincipal | null;
  resolveCloudDeviceSecret(
    deviceId: string,
    secretDigest: string,
    userId: string,
    relayInstanceId: string,
    now: number,
  ): ResolvedPrincipal | null;
  resolveLegacySecret(secretDigest: string): ResolvedPrincipal | null;
  getPairingResponse(
    handshakeId: string,
    contextKey: string,
    now: number,
  ): Pick<PairingResponseRecord, "ke3Digest" | "ciphertextB64"> | null;
  createPending(
    record: Omit<DeviceRecord, "state" | "pendingExpiresAtMs">,
    now: number,
  ): void;
  createPendingWithResponse(
    record: Omit<DeviceRecord, "state" | "pendingExpiresAtMs">,
    response: Omit<PairingResponseRecord, "expiresAtMs">,
    now: number,
  ): void;
  activate(deviceId: string, now: number): DeviceRecord;
  activatePending(
    deviceId: string,
    secretDigest: string,
    now: number,
  ): DeviceRecord | null;
  bindCloudUser(
    deviceId: string,
    secretDigest: string,
    userId: string,
    relayInstanceId: string,
  ): CloudBindingResult;
  activatePendingForCloud(
    deviceId: string,
    secretDigest: string,
    userId: string,
    relayInstanceId: string,
    now: number,
  ): CloudBindingResult;
  revokeCloudDevice(
    deviceId: string,
    secretDigest: string,
    userId: string,
    relayInstanceId: string,
    now: number,
  ): CloudDeviceRevocationResult;
  revoke(deviceId: string): boolean;
  importLegacy(secretDigest: string, now: number): void;
  markLegacyMigrated(): void;
  revokeLegacy(): void;
}
