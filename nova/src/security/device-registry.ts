import { renameSync } from "node:fs";
import { join } from "node:path";

import { readPrivateFileSync, writeFileAtomicSync } from "../storage/atomic-file.js";
import { digestsEqual } from "./device-credential.js";

// Private, versioned device registry under /data. Holds only SHA-256 digests of
// device secrets — never plaintext. All mutations go through a single in-process
// serialization gate and are persisted atomically (temp + fsync + rename). A
// corrupt file is fail-closed: it is never silently recreated and never falls
// back to the legacy token; recovery requires a deliberate reset by the owner.

export const MAX_ACTIVE_DEVICES = 64;
export const MAX_PENDING_DEVICES = 16;
export const PENDING_TTL_MS = 15 * 60 * 1000;
const REGISTRY_FILE = "device-registry.json";
const MAX_REGISTRY_BYTES = 512 * 1024;
const SCHEMA_VERSION = 1;

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
}

export interface LegacyRecord {
  secretDigest: string;
  createdAtMs: number;
}

interface RegistryData {
  version: number;
  devices: DeviceRecord[];
  legacy: LegacyRecord | null;
  legacyImportCompleted: boolean;
}

export class RegistryCorruptError extends Error {}

export interface ResolvedPrincipal {
  kind: "device" | "legacy";
  deviceId?: string;
}

export interface DeviceRegistry {
  list(): DeviceRecord[];
  hasLegacy(): boolean;
  legacyImportCompleted(): boolean;
  // Auth: resolve a presented secret (already parsed) to a principal, or null.
  resolveDeviceSecret(deviceId: string, secretDigest: string, now: number): ResolvedPrincipal | null;
  resolveLegacySecret(secretDigest: string): ResolvedPrincipal | null;
  createPending(record: Omit<DeviceRecord, "state" | "pendingExpiresAtMs">, now: number): void;
  activate(deviceId: string, now: number): DeviceRecord;
  // Authenticated activation for /auth/device/activate: verifies the presented
  // secret against the pending (or already-active, for idempotency) record
  // before promoting. Returns null when the credential is unknown/wrong.
  activatePending(deviceId: string, secretDigest: string, now: number): DeviceRecord | null;
  revoke(deviceId: string): boolean;
  importLegacy(secretDigest: string, now: number): void;
  revokeLegacy(): void;
}

// Moves a corrupt registry file aside so a fresh one can be created. Used by the
// owner-triggered reset: the damaged file is preserved (for forensics) under a
// timestamped name rather than deleted. Best-effort — a subsequent
// openDeviceRegistry surfaces any remaining problem.
export function archiveCorruptRegistry(dataDir: string, now: number): void {
  const path = join(dataDir, REGISTRY_FILE);
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
  const path = join(dataDir, REGISTRY_FILE);
  let data = loadOrInit(path);
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
      const device = data.devices.find((d) => d.deviceId === deviceId && d.state === "active");
      if (!device || !digestsEqual(device.secretDigest, secretDigest)) {
        return null;
      }
      void now;
      return { kind: "device", deviceId };
    },
    resolveLegacySecret(secretDigest) {
      if (!data.legacy || !digestsEqual(data.legacy.secretDigest, secretDigest)) {
        return null;
      }
      return { kind: "legacy" };
    },
    createPending(record, now) {
      const devices = data.devices.filter((d) => !(d.state === "pending" && pendingExpired(d, now)));
      if (pendingCountIn(devices, now) >= MAX_PENDING_DEVICES) {
        throw new Error("pending device limit reached");
      }
      // Re-pairing an existing install keeps the old active record untouched; the
      // new pending record for the same install is what gets promoted later.
      const pending: DeviceRecord = {
        ...record,
        state: "pending",
        pendingExpiresAtMs: now + PENDING_TTL_MS,
      };
      persist({ ...data, devices: [...devices, pending] });
    },
    activate(deviceId, now) {
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
      return doActivate(deviceId, now);
    },
    revoke(deviceId) {
      const next = data.devices.filter((d) => d.deviceId !== deviceId);
      if (next.length === data.devices.length) {
        return false;
      }
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
    revokeLegacy() {
      persist({ ...data, legacy: null });
    },
  };

  function pendingCountIn(devices: DeviceRecord[], now: number): number {
    return devices.filter((d) => d.state === "pending" && !pendingExpired(d, now)).length;
  }
  function activeCountIn(devices: DeviceRecord[]): number {
    return devices.filter((d) => d.state === "active").length;
  }

  function doActivate(deviceId: string, now: number): DeviceRecord {
    const devices = data.devices.filter((d) => !(d.state === "pending" && pendingExpired(d, now)));
    const target = devices.find((d) => d.deviceId === deviceId);
    if (!target) {
      throw new Error("no such pending credential");
    }
    if (target.state === "active") {
      return { ...target }; // idempotent re-activation
    }
    // Activation promotes this install's pending record and retires any older
    // active record for the SAME install (re-pairing replaces on activation).
    const promoted: DeviceRecord = {
      deviceId: target.deviceId,
      secretDigest: target.secretDigest,
      state: "active",
      clientInstallId: target.clientInstallId,
      name: target.name,
      platform: target.platform,
      client: target.client,
      createdAtMs: target.createdAtMs,
    };
    const kept = devices.filter(
      (d) => d.deviceId !== deviceId && !(d.state === "active" && d.clientInstallId === target.clientInstallId)
    );
    // Count AFTER retiring the same-install active record: re-pairing at the cap
    // is a replacement (net count unchanged) and must be allowed; only a
    // genuinely new install pushing past the cap is rejected.
    if (activeCountIn(kept) >= MAX_ACTIVE_DEVICES) {
      throw new Error("active device limit reached");
    }
    persist({ ...data, devices: [...kept, promoted] });
    return { ...promoted };
  }
}

function pendingExpired(d: DeviceRecord, now: number): boolean {
  return d.state === "pending" && typeof d.pendingExpiresAtMs === "number" && now >= d.pendingExpiresAtMs;
}

function loadOrInit(path: string): RegistryData {
  const raw = readPrivateFileSync(path, MAX_REGISTRY_BYTES);
  if (raw === null) {
    return { version: SCHEMA_VERSION, devices: [], legacy: null, legacyImportCompleted: false };
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
    throw new RegistryCorruptError(`unsupported registry version: ${String(obj.version)}`);
  }
  if (!Array.isArray(obj.devices)) {
    throw new RegistryCorruptError("registry.devices is not an array");
  }
  const devices = obj.devices.map(validateDevice);
  const seen = new Set<string>();
  for (const d of devices) {
    if (seen.has(d.deviceId)) {
      throw new RegistryCorruptError("duplicate deviceId in registry");
    }
    seen.add(d.deviceId);
  }
  const legacy = obj.legacy === null || obj.legacy === undefined ? null : validateLegacy(obj.legacy);
  return {
    version: SCHEMA_VERSION,
    devices,
    legacy,
    legacyImportCompleted: obj.legacyImportCompleted === true,
  };
}

function validateDevice(value: unknown): DeviceRecord {
  if (typeof value !== "object" || value === null) {
    throw new RegistryCorruptError("device record is not an object");
  }
  const d = value as Record<string, unknown>;
  const str = (k: string): string => {
    if (typeof d[k] !== "string") {
      throw new RegistryCorruptError(`device.${k} is not a string`);
    }
    return d[k] as string;
  };
  const num = (k: string): number => {
    if (typeof d[k] !== "number" || !Number.isFinite(d[k])) {
      throw new RegistryCorruptError(`device.${k} is not a number`);
    }
    return d[k] as number;
  };
  if (d.state !== "pending" && d.state !== "active") {
    throw new RegistryCorruptError("device.state is invalid");
  }
  const record: DeviceRecord = {
    deviceId: str("deviceId"),
    secretDigest: str("secretDigest"),
    state: d.state,
    clientInstallId: str("clientInstallId"),
    name: str("name"),
    platform: str("platform"),
    client: str("client"),
    createdAtMs: num("createdAtMs"),
  };
  if (d.pendingExpiresAtMs !== undefined) {
    record.pendingExpiresAtMs = num("pendingExpiresAtMs");
  }
  // A pending record without an expiry would occupy the cap forever and never
  // prune — treat it as corruption rather than an immortal pending slot.
  if (record.state === "pending" && record.pendingExpiresAtMs === undefined) {
    throw new RegistryCorruptError("pending device without pendingExpiresAtMs");
  }
  return record;
}

function validateLegacy(value: unknown): LegacyRecord {
  if (typeof value !== "object" || value === null) {
    throw new RegistryCorruptError("legacy record is not an object");
  }
  const l = value as Record<string, unknown>;
  if (typeof l.secretDigest !== "string" || typeof l.createdAtMs !== "number" || !Number.isFinite(l.createdAtMs)) {
    throw new RegistryCorruptError("legacy record fields invalid");
  }
  return { secretDigest: l.secretDigest, createdAtMs: l.createdAtMs };
}
