import type {
  CloudRevocationRecord,
  DeviceRecord,
  LegacyRecord,
  PairingResponseRecord,
} from "./device-registry-types.js";
import {
  InsecureFileError,
  readPrivateFileSync,
} from "../storage/atomic-file.js";
import { isRelayInstanceId } from "../storage/relay-instance.js";

export const DEVICE_REGISTRY_FILE = "device-registry.json";
const MAX_REGISTRY_BYTES = 512 * 1024;
const SCHEMA_VERSION = 1;

export interface RegistryData {
  version: number;
  devices: DeviceRecord[];
  legacy: LegacyRecord | null;
  legacyImportCompleted: boolean;
  pairingResponses: PairingResponseRecord[];
  cloudRevocations: CloudRevocationRecord[];
}

export class RegistryCorruptError extends Error {}

export function loadRegistryData(path: string): RegistryData {
  let raw: Buffer | null;
  try {
    raw = readPrivateFileSync(path, MAX_REGISTRY_BYTES);
  } catch (error) {
    if (error instanceof InsecureFileError) {
      throw new RegistryCorruptError(
        `device registry file is not a safe regular file: ${error.message}`,
      );
    }
    throw error;
  }
  if (raw === null) {
    return {
      version: SCHEMA_VERSION,
      devices: [],
      legacy: null,
      legacyImportCompleted: false,
      pairingResponses: [],
      cloudRevocations: [],
    };
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw.toString("utf8"));
  } catch {
    throw new RegistryCorruptError("device registry is not valid JSON");
  }
  return validate(parsed);
}

function validate(parsed: unknown): RegistryData {
  if (typeof parsed !== "object" || parsed === null) {
    throw new RegistryCorruptError("device registry is not an object");
  }
  const obj = parsed as Record<string, unknown>;
  if (obj.version !== SCHEMA_VERSION) {
    throw new RegistryCorruptError(
      `unsupported registry version: ${String(obj.version)}`,
    );
  }
  if (!Array.isArray(obj.devices)) {
    throw new RegistryCorruptError("registry.devices is not an array");
  }
  const devices = obj.devices.map(validateDevice);
  const seen = new Set<string>();
  for (const device of devices) {
    if (seen.has(device.deviceId)) {
      throw new RegistryCorruptError("duplicate deviceId in registry");
    }
    seen.add(device.deviceId);
  }
  const legacy =
    obj.legacy === null || obj.legacy === undefined
      ? null
      : validateLegacy(obj.legacy);
  const pairingResponses =
    obj.pairingResponses === undefined
      ? []
      : validatePairingResponses(obj.pairingResponses);
  const cloudRevocations =
    obj.cloudRevocations === undefined
      ? []
      : validateCloudRevocations(obj.cloudRevocations);
  return {
    version: SCHEMA_VERSION,
    devices,
    legacy,
    legacyImportCompleted: obj.legacyImportCompleted === true,
    pairingResponses,
    cloudRevocations,
  };
}

function validateDevice(value: unknown): DeviceRecord {
  if (typeof value !== "object" || value === null) {
    throw new RegistryCorruptError("device record is not an object");
  }
  const raw = value as Record<string, unknown>;
  const stringField = (key: string): string => {
    if (typeof raw[key] !== "string") {
      throw new RegistryCorruptError(`device.${key} is not a string`);
    }
    return raw[key] as string;
  };
  const numberField = (key: string): number => {
    if (typeof raw[key] !== "number" || !Number.isFinite(raw[key])) {
      throw new RegistryCorruptError(`device.${key} is not a number`);
    }
    return raw[key] as number;
  };
  if (raw.state !== "pending" && raw.state !== "active") {
    throw new RegistryCorruptError("device.state is invalid");
  }

  const record: DeviceRecord = {
    deviceId: stringField("deviceId"),
    secretDigest: stringField("secretDigest"),
    state: raw.state,
    clientInstallId: stringField("clientInstallId"),
    name: stringField("name"),
    platform: stringField("platform"),
    client: stringField("client"),
    createdAtMs: numberField("createdAtMs"),
  };
  if (raw.lastUsedAtMs !== undefined) {
    record.lastUsedAtMs = numberField("lastUsedAtMs");
  }
  if (raw.pendingExpiresAtMs !== undefined) {
    record.pendingExpiresAtMs = numberField("pendingExpiresAtMs");
  }
  if (raw.cloudUserId !== undefined) {
    record.cloudUserId = stringField("cloudUserId");
  }
  if (raw.cloudRelayInstanceId !== undefined) {
    record.cloudRelayInstanceId = stringField("cloudRelayInstanceId");
  }
  if (record.state === "pending" && record.pendingExpiresAtMs === undefined) {
    throw new RegistryCorruptError("pending device without pendingExpiresAtMs");
  }
  if (
    record.cloudUserId !== undefined &&
    !isValidCloudUserId(record.cloudUserId)
  ) {
    throw new RegistryCorruptError("device.cloudUserId is invalid");
  }
  if (
    (record.cloudUserId === undefined) !==
    (record.cloudRelayInstanceId === undefined)
  ) {
    throw new RegistryCorruptError("device cloud binding is incomplete");
  }
  if (
    record.cloudRelayInstanceId !== undefined &&
    !isRelayInstanceId(record.cloudRelayInstanceId)
  ) {
    throw new RegistryCorruptError("device.cloudRelayInstanceId is invalid");
  }
  return record;
}

function validateLegacy(value: unknown): LegacyRecord {
  if (typeof value !== "object" || value === null) {
    throw new RegistryCorruptError("legacy record is not an object");
  }
  const raw = value as Record<string, unknown>;
  if (
    typeof raw.secretDigest !== "string" ||
    typeof raw.createdAtMs !== "number" ||
    !Number.isFinite(raw.createdAtMs)
  ) {
    throw new RegistryCorruptError("legacy record fields invalid");
  }
  return { secretDigest: raw.secretDigest, createdAtMs: raw.createdAtMs };
}

function validatePairingResponses(value: unknown): PairingResponseRecord[] {
  if (!Array.isArray(value)) {
    throw new RegistryCorruptError("registry.pairingResponses is not an array");
  }
  return value.map((entry) => {
    if (typeof entry !== "object" || entry === null) {
      throw new RegistryCorruptError("pairing response is not an object");
    }
    const raw = entry as Record<string, unknown>;
    if (
      typeof raw.handshakeId !== "string" ||
      typeof raw.contextKey !== "string" ||
      typeof raw.deviceId !== "string" ||
      typeof raw.ke3Digest !== "string" ||
      typeof raw.ciphertextB64 !== "string" ||
      typeof raw.expiresAtMs !== "number" ||
      !Number.isFinite(raw.expiresAtMs)
    ) {
      throw new RegistryCorruptError("pairing response fields are invalid");
    }
    return {
      handshakeId: raw.handshakeId,
      contextKey: raw.contextKey,
      deviceId: raw.deviceId,
      ke3Digest: raw.ke3Digest,
      ciphertextB64: raw.ciphertextB64,
      expiresAtMs: raw.expiresAtMs,
    };
  });
}

function validateCloudRevocations(value: unknown): CloudRevocationRecord[] {
  if (!Array.isArray(value) || value.length > 128) {
    throw new RegistryCorruptError("registry.cloudRevocations is invalid");
  }
  return value.map((entry) => {
    if (typeof entry !== "object" || entry === null) {
      throw new RegistryCorruptError("cloud revocation is not an object");
    }
    const raw = entry as Record<string, unknown>;
    if (
      typeof raw.deviceId !== "string" ||
      typeof raw.secretDigest !== "string" ||
      typeof raw.cloudUserId !== "string" ||
      !isValidCloudUserId(raw.cloudUserId) ||
      typeof raw.cloudRelayInstanceId !== "string" ||
      !isRelayInstanceId(raw.cloudRelayInstanceId) ||
      typeof raw.revokedAtMs !== "number" ||
      !Number.isFinite(raw.revokedAtMs) ||
      typeof raw.expiresAtMs !== "number" ||
      !Number.isFinite(raw.expiresAtMs) ||
      raw.expiresAtMs <= raw.revokedAtMs
    ) {
      throw new RegistryCorruptError("cloud revocation fields are invalid");
    }
    return {
      deviceId: raw.deviceId,
      secretDigest: raw.secretDigest,
      cloudUserId: raw.cloudUserId,
      cloudRelayInstanceId: raw.cloudRelayInstanceId,
      revokedAtMs: raw.revokedAtMs,
      expiresAtMs: raw.expiresAtMs,
    };
  });
}

export function isValidCloudUserId(value: string): boolean {
  return (
    value.length > 0 &&
    value.length <= 256 &&
    value === value.trim() &&
    !value.includes(",") &&
    !/[\u0000-\u001f\u007f]/.test(value)
  );
}
