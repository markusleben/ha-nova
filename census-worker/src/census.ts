// Pure census core — validation, ISO-week math, and stats aggregation.
// No Cloudflare types in here: everything is unit-testable without the
// workers runtime (tests/census-worker/worker.test.ts). The thin Durable
// Object wiring lives in index.ts.

// The accepted field set is the wire contract with the client
// (cli/census.go censusPayload json tags) — contract-tested so neither side
// can grow silently.
export const ALLOWED_FIELDS = ["schema", "version", "relay", "os"] as const;
export const REQUIRED_FIELDS = ["schema", "version", "os"] as const;
export const ALLOWED_OS = ["macos", "linux", "windows"] as const;
export const MAX_BODY_BYTES = 512;
export const STATS_WINDOW_WEEKS = 4;
// The /stats weekly series stays bounded: older weeks age out of the public
// response (the counters themselves are kept).
export const WEEKLY_HORIZON_WEEKS = 26;
// A hostile client could otherwise mint a Cartesian product of fabricated
// version/relay values. Reserve the final row for one all-dimensions overflow
// bucket; the whole weekly table is therefore capped, not just each dimension.
export const MAX_ROWS_PER_WEEK = 256;
export const OVERFLOW_BUCKET = "other";

// Exported so the cross-contract test can compare them against the client's
// mirror (cli/census.go censusVersionPattern / censusMaxVersionLength).
export const VERSION_PATTERN = /^\d+\.\d+\.\d+(-rc\d+)?$/;
export const MAX_VERSION_LENGTH = 32;

export interface CensusPing {
  schema: number;
  version: string;
  os: string;
  relay?: string;
}

// One aggregate counter row: how many pings arrived in `iso_week` for this
// (version, os, relay) combination. `relay` is "unknown" when the ping
// omitted it. No identifiers, no timestamps beyond the week label.
export interface CounterRow {
  iso_week: string;
  version: string;
  os: string;
  relay: string;
  count: number;
}

export interface CounterKey {
  iso_week: string;
  version: string;
  os: string;
  relay: string;
}

export type HandlerResult = {
  status: number;
  body?: string;
  headers?: Record<string, string>;
};

function isValidVersion(value: unknown): value is string {
  return (
    typeof value === "string" &&
    value.length <= MAX_VERSION_LENGTH &&
    VERSION_PATTERN.test(value)
  );
}

export type PingValidation =
  | { ok: true; ping: CensusPing }
  | { ok: false; status: number; error: string };

// validatePing enforces the strict allowlist: 413 for oversized bodies,
// 400 for unknown fields, wrong shapes, bad semver, or an os outside the
// three documented buckets. Method/content-type gating (405/415) happens in
// handleCensusRequest.
export function validatePing(bodyText: string): PingValidation {
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
  const record = parsed as Record<string, unknown>;
  for (const key of Object.keys(record)) {
    if (!(ALLOWED_FIELDS as readonly string[]).includes(key)) {
      return { ok: false, status: 400, error: `unknown field: ${key}` };
    }
  }
  for (const key of REQUIRED_FIELDS) {
    if (!(key in record)) {
      return { ok: false, status: 400, error: `missing field: ${key}` };
    }
  }
  if (record["schema"] !== 1) {
    return { ok: false, status: 400, error: "unsupported schema" };
  }
  if (!isValidVersion(record["version"])) {
    return { ok: false, status: 400, error: "invalid version" };
  }
  const os = record["os"];
  if (typeof os !== "string" || !(ALLOWED_OS as readonly string[]).includes(os)) {
    return { ok: false, status: 400, error: "invalid os" };
  }
  const ping: CensusPing = {
    schema: 1,
    version: record["version"],
    os,
  };
  if ("relay" in record) {
    if (!isValidVersion(record["relay"])) {
      return { ok: false, status: 400, error: "invalid relay version" };
    }
    ping.relay = record["relay"];
  }
  return { ok: true, ping };
}

export function counterKeyFor(ping: CensusPing, now: Date): CounterKey {
  return {
    iso_week: isoWeekUTC(now),
    version: ping.version,
    os: ping.os,
    relay: ping.relay ?? "unknown",
  };
}

// isoWeekUTC renders the ISO-8601 week label in UTC, zero-padded — the same
// label the client stamps (cli/census_state.go censusISOWeek).
export function isoWeekUTC(date: Date): string {
  const day = Date.UTC(
    date.getUTCFullYear(),
    date.getUTCMonth(),
    date.getUTCDate(),
  );
  const probe = new Date(day);
  // Shift to the Thursday of this week — its year is the ISO year.
  const isoDay = (probe.getUTCDay() + 6) % 7; // Monday = 0
  probe.setUTCDate(probe.getUTCDate() - isoDay + 3);
  const isoYear = probe.getUTCFullYear();
  const yearStart = Date.UTC(isoYear, 0, 1);
  const week = Math.ceil(((probe.getTime() - yearStart) / 86400000 + 1) / 7);
  return `${String(isoYear).padStart(4, "0")}-W${String(week).padStart(2, "0")}`;
}

// readBodyCapped consumes a request body stream with a hard byte cap: the
// moment more than maxBytes arrive, reading stops (the stream is cancelled)
// and overflow is reported — a chunked/H2 request without Content-Length can
// never make the worker buffer an unbounded body.
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
    if (done) {
      break;
    }
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

// oldestPublishedWeek is the earliest ISO-week label inside the public stats
// horizon. The Durable Object uses it to BOUND the rows it loads for /stats
// (labels are zero-padded and year-major, so string comparison is
// chronological); buildStats applies the same horizon, so nothing published
// is lost by the bound.
export function oldestPublishedWeek(now: Date): string {
  return isoWeekUTC(new Date(now.getTime() - (WEEKLY_HORIZON_WEEKS - 1) * 7 * 86400000));
}

function lastWeeks(now: Date, count: number): string[] {
  const labels: string[] = [];
  for (let i = 0; i < count; i++) {
    labels.push(isoWeekUTC(new Date(now.getTime() - i * 7 * 86400000)));
  }
  return labels;
}

function addCount(target: Record<string, number>, key: string, count: number): void {
  target[key] = (target[key] ?? 0) + count;
}

export interface CensusStats {
  schema: number;
  generated_at: string;
  weekly: { iso_week: string; count: number }[];
  window_weeks: number;
  by_os: Record<string, number>;
  by_version: Record<string, number>;
  by_relay: Record<string, number>;
  peak_weekly_pings: number;
  footnotes: {
    counting: string;
    identifiers: string;
  };
}

// buildStats aggregates accepted pings into the public numbers. by_*
// breakdowns cover the last STATS_WINDOW_WEEKS ISO weeks (including the
// current one); peak_weekly_pings is the busiest week in that window. It is a
// directional activity signal, never a distinct-install estimate: the public,
// ID-free receiver cannot authenticate, de-duplicate, or verify clients.
export function buildStats(rows: CounterRow[], now: Date): CensusStats {
  const weeklyTotals: Record<string, number> = {};
  const window = new Set(lastWeeks(now, STATS_WINDOW_WEEKS));
  const byOS: Record<string, number> = {};
  const byVersion: Record<string, number> = {};
  const byRelay: Record<string, number> = {};
  for (const row of rows) {
    addCount(weeklyTotals, row.iso_week, row.count);
    if (window.has(row.iso_week)) {
      addCount(byOS, row.os, row.count);
      addCount(byVersion, row.version, row.count);
      addCount(byRelay, row.relay, row.count);
    }
  }
  // The published weekly series is bounded to a fixed horizon; counters for
  // older weeks remain stored but age out of the public response.
  const horizon = new Set(lastWeeks(now, WEEKLY_HORIZON_WEEKS));
  const weekly = Object.keys(weeklyTotals)
    .filter((label) => horizon.has(label))
    .sort()
    .map((iso_week) => ({ iso_week, count: weeklyTotals[iso_week] ?? 0 }));
  let peakWeeklyPings = 0;
  for (const label of window) {
    peakWeeklyPings = Math.max(peakWeeklyPings, weeklyTotals[label] ?? 0);
  }
  return {
    schema: 1,
    generated_at: now.toISOString(),
    weekly,
    window_weeks: STATS_WINDOW_WEEKS,
    by_os: byOS,
    by_version: byVersion,
    by_relay: byRelay,
    peak_weekly_pings: peakWeeklyPings,
    footnotes: {
      counting:
        "Counts are accepted, schema-valid census pings. The endpoint has no client identifier or authentication, so counts are directional and may include duplicates or fabricated pings. New dimension combinations beyond the weekly row cap appear in the other overflow bucket — these are not verified unique installs.",
      identifiers:
        "Pings carry no identifier, so unique installs, retention, and de-duplication cannot be computed — by design.",
    },
  };
}

// CounterStore is the only thing the request handler needs from storage; the
// Durable Object implements it with SQLite, tests with a Map.
//
// ATOMICITY CONTRACT for increment: the implementation must apply
// clampWeeklyCardinality and write the resulting row in one atomic step —
// i.e. with NO await point between reading the week's rows and writing.
// Clamping at the handler level would race: two interleaved pings with
// distinct fabricated versions could both read pre-cap rows and both insert,
// defeating the cap. The Durable Object uses the synchronous SQLite API for
// exactly this reason (index.ts); the test store applies it synchronously.
export interface CounterStore {
  increment(key: CounterKey): Promise<void>;
  rows(): Promise<CounterRow[]>;
}

// clampWeeklyCardinality enforces one total per-week row cap. Existing exact
// rows keep their labels. Once only the reserved overflow slot remains, every
// new combination folds into one (other, other, other) row, preventing a
// fabricated version x relay x os Cartesian product. Pure and synchronous by
// design — callers embed it into their atomic write step.
export function clampWeeklyCardinality(weekRows: CounterRow[], key: CounterKey): CounterKey {
  const sameWeek = weekRows.filter((row) => row.iso_week === key.iso_week);
  const exactExists = sameWeek.some(
    (row) => row.version === key.version && row.os === key.os && row.relay === key.relay,
  );
  if (exactExists) {
    return key;
  }
  const overflow: CounterKey = {
    iso_week: key.iso_week,
    version: OVERFLOW_BUCKET,
    os: OVERFLOW_BUCKET,
    relay: OVERFLOW_BUCKET,
  };
  const overflowExists = sameWeek.some(
    (row) =>
      row.version === OVERFLOW_BUCKET &&
      row.os === OVERFLOW_BUCKET &&
      row.relay === OVERFLOW_BUCKET,
  );
  if (overflowExists || sameWeek.length >= MAX_ROWS_PER_WEEK - 1) {
    return overflow;
  }
  return key;
}

export function isJSONMediaType(contentType: string): boolean {
  return contentType.split(";", 1)[0]?.trim().toLowerCase() === "application/json";
}

export interface CensusRequestLike {
  method: string;
  path: string;
  contentType: string;
  bodyText: string;
  // Declared Content-Length, when the transport knows it BEFORE reading the
  // body — lets oversized requests be rejected without buffering them.
  contentLength?: number;
}

export const STATS_HEADERS: Record<string, string> = {
  "Content-Type": "application/json",
  "Cache-Control": "public, max-age=3600",
  "Access-Control-Allow-Origin": "*",
};

// handleCensusRequest is the whole routing surface: POST /ping (validated,
// counted, 204), GET /stats (public aggregates), 404 for everything else.
export async function handleCensusRequest(
  request: CensusRequestLike,
  store: CounterStore,
  now: Date,
): Promise<HandlerResult> {
  if (request.path === "/ping") {
    if (request.method !== "POST") {
      return { status: 405, headers: { Allow: "POST" } };
    }
    if (!isJSONMediaType(request.contentType)) {
      return { status: 415 };
    }
    // Declared-size gate first: an oversized Content-Length is 413 before any
    // body bytes were buffered (validatePing re-checks the actual bytes).
    if (request.contentLength !== undefined && request.contentLength > MAX_BODY_BYTES) {
      return { status: 413 };
    }
    const validation = validatePing(request.bodyText);
    if (!validation.ok) {
      return {
        status: validation.status,
        body: JSON.stringify({ error: validation.error }),
        headers: { "Content-Type": "application/json" },
      };
    }
    // The store clamps and writes atomically (see the CounterStore contract).
    // A storage failure is answered as 5xx, never masked as a 204 — the
    // client stays fire-and-forget, so visibility costs users nothing.
    try {
      await store.increment(counterKeyFor(validation.ping, now));
    } catch {
      return {
        status: 500,
        body: JSON.stringify({ error: "counter write failed" }),
        headers: { "Content-Type": "application/json" },
      };
    }
    return { status: 204 };
  }
  if (request.path === "/stats" && request.method === "GET") {
    const stats = buildStats(await store.rows(), now);
    return {
      status: 200,
      body: JSON.stringify(stats),
      headers: STATS_HEADERS,
    };
  }
  return { status: 404 };
}
