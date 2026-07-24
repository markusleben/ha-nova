// Public Census protocol: strict validation, pseudonymous-ID hashing, and the
// two mutation routes. Protected statistics are assembled separately.

export const SCHEMA_1_FIELDS = ["schema", "version", "relay", "os"] as const;
export const SCHEMA_2_FIELDS = [
  "schema",
  "installation_id",
  "version",
  "relay",
  "os",
] as const;
export const WITHDRAW_FIELDS = ["schema", "installation_id"] as const;
export const ALLOWED_FIELDS = SCHEMA_2_FIELDS;
export const ALLOWED_OS = ["macos", "linux", "windows"] as const;
export const MAX_BODY_BYTES = 512;
export const VERSION_PATTERN = /^\d+\.\d+\.\d+(-rc\d+)?$/;
export const INSTALLATION_ID_PATTERN = /^cns-[0-9a-f]{32}$/;
export const MAX_VERSION_LENGTH = 32;

export interface LegacyPing {
  schema: 1;
  version: string;
  os: string;
  relay?: string;
}

export interface InstallationPing {
  schema: 2;
  installation_id: string;
  version: string;
  os: string;
  relay?: string;
}

export interface InstallationRecord {
  id_hash: string;
  version: string;
  os: string;
  relay?: string;
  observed_at: number;
}

export interface WithdrawRequest {
  schema: 2;
  installation_id: string;
}

export interface LegacyCounterKey {
  iso_week: string;
  version: string;
  os: string;
  relay: string;
}

export interface LegacyCounterRow extends LegacyCounterKey {
  count: number;
}

export interface InstallationStats {
  active_21_days: number;
  known_60_days: number;
  by_version: Record<string, number>;
  by_os: Record<string, number>;
  relay_versions: Record<string, number>;
  relay_not_recently_observed: number;
  new_installation_rejections_today: number;
}

export interface CensusStore {
  upsertInstallation(record: InstallationRecord): Promise<void>;
  deleteInstallation(idHash: string): Promise<void>;
  incrementLegacy(key: LegacyCounterKey): Promise<void>;
  installationStats(now: Date): Promise<InstallationStats>;
  legacyRows(now: Date): Promise<LegacyCounterRow[]>;
}

export interface CensusRequestLike {
  method: string;
  path: string;
  contentType: string;
  bodyText: string;
  accessToken: string;
  localStatsToken: string;
  localRequest: boolean;
  contentLength?: number;
}

export type HandlerResult = {
  status: number;
  body?: string;
  headers?: Record<string, string>;
};

type PingValidation =
  | { ok: true; ping: LegacyPing | InstallationPing }
  | { ok: false; status: number; error: string };

type WithdrawValidation =
  | { ok: true; withdraw: WithdrawRequest }
  | { ok: false; status: number; error: string };

function parseObject(
  bodyText: string,
):
  | { ok: true; record: Record<string, unknown> }
  | { ok: false; status: number; error: string } {
  if (new TextEncoder().encode(bodyText).byteLength > MAX_BODY_BYTES) {
    return { ok: false, status: 413, error: "body too large" };
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(bodyText);
  } catch {
    return { ok: false, status: 400, error: "invalid JSON" };
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    return { ok: false, status: 400, error: "payload must be a JSON object" };
  }
  return { ok: true, record: parsed as Record<string, unknown> };
}

function validateExactFields(
  record: Record<string, unknown>,
  allowed: readonly string[],
  required: readonly string[],
): string | undefined {
  for (const key of Object.keys(record)) {
    if (!allowed.includes(key)) {
      return `unknown field: ${key}`;
    }
  }
  for (const key of required) {
    if (!(key in record)) {
      return `missing field: ${key}`;
    }
  }
  return undefined;
}

function isValidVersion(value: unknown): value is string {
  return (
    typeof value === "string" &&
    value.length <= MAX_VERSION_LENGTH &&
    VERSION_PATTERN.test(value)
  );
}

function commonPingFields(
  record: Record<string, unknown>,
):
  | { ok: true; version: string; os: string; relay?: string }
  | { ok: false; error: string } {
  if (!isValidVersion(record["version"])) {
    return { ok: false, error: "invalid version" };
  }
  const os = record["os"];
  if (
    typeof os !== "string" ||
    !(ALLOWED_OS as readonly string[]).includes(os)
  ) {
    return { ok: false, error: "invalid os" };
  }
  const result: { ok: true; version: string; os: string; relay?: string } = {
    ok: true,
    version: record["version"],
    os,
  };
  if ("relay" in record) {
    if (!isValidVersion(record["relay"])) {
      return { ok: false, error: "invalid relay version" };
    }
    result.relay = record["relay"];
  }
  return result;
}

export function validatePing(bodyText: string): PingValidation {
  const parsed = parseObject(bodyText);
  if (!parsed.ok) {
    return parsed;
  }
  const { record } = parsed;
  const schema = record["schema"];
  const allowed = schema === 1 ? SCHEMA_1_FIELDS : SCHEMA_2_FIELDS;
  const required =
    schema === 1
      ? ["schema", "version", "os"]
      : ["schema", "installation_id", "version", "os"];
  const fieldError = validateExactFields(record, allowed, required);
  if (fieldError) {
    return { ok: false, status: 400, error: fieldError };
  }
  if (schema !== 1 && schema !== 2) {
    return { ok: false, status: 400, error: "unsupported schema" };
  }
  const common = commonPingFields(record);
  if (!common.ok) {
    return { ok: false, status: 400, error: common.error };
  }
  if (schema === 1) {
    const ping: LegacyPing = {
      schema: 1,
      version: common.version,
      os: common.os,
    };
    if (common.relay !== undefined) {
      ping.relay = common.relay;
    }
    return { ok: true, ping };
  }
  if (
    typeof record["installation_id"] !== "string" ||
    !INSTALLATION_ID_PATTERN.test(record["installation_id"])
  ) {
    return { ok: false, status: 400, error: "invalid installation_id" };
  }
  const ping: InstallationPing = {
    schema: 2,
    installation_id: record["installation_id"],
    version: common.version,
    os: common.os,
  };
  if (common.relay !== undefined) {
    ping.relay = common.relay;
  }
  return { ok: true, ping };
}

export function validateWithdraw(bodyText: string): WithdrawValidation {
  const parsed = parseObject(bodyText);
  if (!parsed.ok) {
    return parsed;
  }
  const fieldError = validateExactFields(
    parsed.record,
    WITHDRAW_FIELDS,
    WITHDRAW_FIELDS,
  );
  if (fieldError) {
    return { ok: false, status: 400, error: fieldError };
  }
  const installationID = parsed.record["installation_id"];
  if (
    parsed.record["schema"] !== 2 ||
    typeof installationID !== "string" ||
    !INSTALLATION_ID_PATTERN.test(installationID)
  ) {
    return { ok: false, status: 400, error: "invalid withdrawal" };
  }
  return {
    ok: true,
    withdraw: { schema: 2, installation_id: installationID },
  };
}

export async function hashInstallationID(
  installationID: string,
): Promise<string> {
  const bytes = new TextEncoder().encode(installationID);
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return [...new Uint8Array(digest)]
    .map((byte) => byte.toString(16).padStart(2, "0"))
    .join("");
}

export function isoWeekUTC(date: Date): string {
  const probe = new Date(
    Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate()),
  );
  const isoDay = (probe.getUTCDay() + 6) % 7;
  probe.setUTCDate(probe.getUTCDate() - isoDay + 3);
  const isoYear = probe.getUTCFullYear();
  const yearStart = Date.UTC(isoYear, 0, 1);
  const week = Math.ceil(((probe.getTime() - yearStart) / 86400000 + 1) / 7);
  return `${String(isoYear).padStart(4, "0")}-W${String(week).padStart(2, "0")}`;
}

export function isJSONMediaType(contentType: string): boolean {
  return (
    contentType.split(";", 1)[0]?.trim().toLowerCase() === "application/json"
  );
}

function validationError(validation: {
  status: number;
  error: string;
}): HandlerResult {
  return {
    status: validation.status,
    body: JSON.stringify({ error: validation.error }),
    headers: { "Content-Type": "application/json" },
  };
}

export async function handleMutationRequest(
  request: CensusRequestLike,
  store: CensusStore,
  now: Date,
): Promise<HandlerResult> {
  if (request.path !== "/ping" && request.path !== "/withdraw") {
    return { status: 404 };
  }
  if (request.method !== "POST") {
    return { status: 405, headers: { Allow: "POST" } };
  }
  if (!isJSONMediaType(request.contentType)) {
    return { status: 415 };
  }
  if (
    request.contentLength !== undefined &&
    request.contentLength > MAX_BODY_BYTES
  ) {
    return { status: 413 };
  }
  try {
    if (request.path === "/withdraw") {
      const validation = validateWithdraw(request.bodyText);
      if (!validation.ok) {
        return validationError(validation);
      }
      await store.deleteInstallation(
        await hashInstallationID(validation.withdraw.installation_id),
      );
      return { status: 204 };
    }
    const validation = validatePing(request.bodyText);
    if (!validation.ok) {
      return validationError(validation);
    }
    if (validation.ping.schema === 1) {
      await store.incrementLegacy({
        iso_week: isoWeekUTC(now),
        version: validation.ping.version,
        os: validation.ping.os,
        relay: validation.ping.relay ?? "not-reported",
      });
    } else {
      const record: InstallationRecord = {
        id_hash: await hashInstallationID(validation.ping.installation_id),
        version: validation.ping.version,
        os: validation.ping.os,
        observed_at: now.getTime(),
      };
      if (validation.ping.relay !== undefined) {
        record.relay = validation.ping.relay;
      }
      await store.upsertInstallation(record);
    }
    return { status: 204 };
  } catch {
    return {
      status: 500,
      body: JSON.stringify({ error: "census storage failed" }),
      headers: { "Content-Type": "application/json" },
    };
  }
}

export async function readBodyCapped(
  body: ReadableStream<Uint8Array> | null,
  maxBytes: number,
): Promise<{ text: string; overflow: boolean }> {
  if (body === null) {
    return { text: "", overflow: false };
  }
  const reader = body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    if (value !== undefined) {
      total += value.byteLength;
      if (total > maxBytes) {
        await reader.cancel();
        return { text: "", overflow: true };
      }
      chunks.push(value);
    }
  }
  const merged = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    merged.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return { text: new TextDecoder().decode(merged), overflow: false };
}
