export type StateRow = {
  entity_id: string;
  state: string;
  attributes?: { restored?: boolean; friendly_name?: string };
};

export type RegistryRow = {
  entity_id: string;
  config_entry_id?: string | null;
  device_id?: string | null;
  platform?: string | null;
  name?: string;
  original_name?: string;
};

export type ConfigEntry = {
  entry_id: string;
  domain: string;
  state: string;
  disabled_by?: string | null;
  title: string;
  error_reason?: string;
  error_reason_translation_key?: string;
};

export type DeviceRow = {
  id: string;
  name: string;
  configuration_url?: string;
};

export type AvailabilityFixture = {
  states: StateRow[];
  registry?: RegistryRow[];
  entries: ConfigEntry[];
  devices?: DeviceRow[];
  activeRepairs?: number;
  lowBatteries?: number;
  failedSystemHealth?: number;
  unavailableSources?: string[];
  privacyMode?: "private" | "shareable" | "aggregate";
};

export const inventoryDomains = new Set([
  "device_tracker",
  "person",
  "geo_location",
  "button",
  "event",
  "scene",
  "stt",
]);

export const lowSignalDomains = new Set(["button", "event", "scene", "stt"]);

export const attentionEntryStates = new Set([
  "setup_error",
  "setup_retry",
  "migration_error",
  "failed_unload",
  "not_loaded",
]);

export function isSafeHASlug(value: unknown): value is string {
  return typeof value === "string" && /^[a-z0-9_]{1,128}$/.test(value);
}

// Display-name precedence lives with the caller; this only decides whether a
// candidate name is safely renderable next to the exact entity ID: printable,
// single-line, bounded, adding information beyond the ID itself, and free of
// the patterns the privacy contract forbids in every mode (URLs, hostnames,
// IPs, Markdown delimiters, secret-like values). Fail closed — dropping a
// name loses nothing, the exact ID is always shown.
const FORBIDDEN_NAME_PATTERN = new RegExp(
  [
    String.raw`https?://`,
    String.raw`www\.`,
    String.raw`\d{1,3}(?:\.\d{1,3}){3}`,
    // Dot-joined tokens are hostname-shaped (host.local, nas.lan, x.example.com).
    String.raw`[^\s.]+\.[^\s.]{2,}`,
    String.raw`[\\|\x60*_#\[\]<>]`,
    String.raw`(?:token|secret|passwor|api[-_ ]?key)`,
  ].join("|"),
  "iu",
);

export function safeDisplayName(candidate: unknown, entityID: string): string {
  if (typeof candidate !== "string") return "";
  const trimmed = candidate.trim();
  if (
    trimmed === "" ||
    trimmed === entityID ||
    trimmed.length > 64 ||
    /[\p{Cc}\p{Cf}]/u.test(trimmed) ||
    FORBIDDEN_NAME_PATTERN.test(trimmed)
  ) {
    return "";
  }
  return trimmed;
}

export function isSafeEntityID(value: unknown): value is string {
  // The domain bound mirrors the user-visible domain contract (max 128);
  // the object part gets a generous but finite bound.
  return (
    typeof value === "string" &&
    /^[a-z0-9_]{1,128}\.[a-z0-9_]{1,255}$/.test(value)
  );
}

const knownEntryStates = new Set([
  ...attentionEntryStates,
  "setup_in_progress",
  "unload_in_progress",
  "loaded",
]);

export function compareCodePoint(left: string, right: string): number {
  return left < right ? -1 : left > right ? 1 : 0;
}

export function isAttentionEntry(entry: ConfigEntry | undefined): boolean {
  return Boolean(
    entry && !entry.disabled_by && attentionEntryStates.has(entry.state),
  );
}

export function percent(count: number, total: number): string {
  return total === 0 ? "0%" : `${Math.floor((count * 100) / total)}%`;
}

const safeTranslationReasons = new Map([
  ["invalid_auth", "authentication failed"],
  ["authentication_failed", "authentication failed"],
  ["cannot_connect", "connection failed"],
  ["connection_failed", "connection failed"],
  ["timeout", "connection timed out"],
]);

export function safeReason(entry: ConfigEntry): string {
  const mapped = entry.error_reason_translation_key
    ? safeTranslationReasons.get(entry.error_reason_translation_key)
    : undefined;
  if (mapped) return `; ${mapped}`;
  return entry.error_reason || entry.error_reason_translation_key
    ? "; technical setup error"
    : "";
}

export function safeEntryState(entry: ConfigEntry | undefined): string {
  if (!entry) return "entry metadata unavailable";
  if (entry.disabled_by) return "intentionally disabled";
  return knownEntryStates.has(entry.state)
    ? entry.state
    : "unknown config-entry state";
}
