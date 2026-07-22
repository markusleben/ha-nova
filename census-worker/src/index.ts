// Cloudflare wiring for the HA NOVA census: one SQLite Durable Object holding
// aggregate counter rows (iso_week, version, os, relay) -> count. All request
// logic lives in the pure core (census.ts); this file only adapts it to the
// workers runtime. No identifiers, no IPs, no client timestamps are stored —
// see PRIVACY.md and docs/reference/census.md in the repo root.

import {
  CensusRequestLike,
  CounterKey,
  CounterRow,
  CounterStore,
  MAX_BODY_BYTES,
  clampWeeklyCardinality,
  handleCensusRequest,
  oldestPublishedWeek,
  readBodyCapped,
} from "./census";

export interface Env {
  CENSUS: DurableObjectNamespace;
}

export class CensusCounter {
  private readonly sql: SqlStorage;

  constructor(state: DurableObjectState) {
    this.sql = state.storage.sql;
    this.sql.exec(
      `CREATE TABLE IF NOT EXISTS counters (
         iso_week TEXT NOT NULL,
         version  TEXT NOT NULL,
         os       TEXT NOT NULL,
         relay    TEXT NOT NULL,
         count    INTEGER NOT NULL DEFAULT 0,
         PRIMARY KEY (iso_week, version, os, relay)
       )`,
    );
  }

  async fetch(request: Request): Promise<Response> {
    const path = new URL(request.url).pathname;
    if (request.method === "POST" && path === "/increment") {
      const key = (await request.json()) as CounterKey;
      // Clamp + upsert with NO await in between: the DO is single-threaded,
      // and the synchronous SQLite API keeps this read-modify-write atomic —
      // interleaved requests cannot both slip past the cardinality cap.
      const weekRows = this.sql
        .exec(
          `SELECT iso_week, version, os, relay, count FROM counters WHERE iso_week = ?`,
          key.iso_week,
        )
        .toArray() as unknown as CounterRow[];
      const clamped = clampWeeklyCardinality(weekRows, key);
      this.sql.exec(
        `INSERT INTO counters (iso_week, version, os, relay, count)
         VALUES (?, ?, ?, ?, 1)
         ON CONFLICT (iso_week, version, os, relay)
         DO UPDATE SET count = count + 1`,
        clamped.iso_week,
        clamped.version,
        clamped.os,
        clamped.relay,
      );
      return new Response(null, { status: 204 });
    }
    if (request.method === "GET" && path === "/rows") {
      // Bound the scan to the published stats horizon: counters are retained
      // forever, but /stats only publishes WEEKLY_HORIZON_WEEKS — loading the
      // whole table would grow without bound. Within the horizon the row
      // count is already bounded by construction: per week at most
      // (MAX_VERSIONS_PER_WEEK+1) x (MAX_RELAYS_PER_WEEK+1) x 3 os buckets.
      const rows = this.sql
        .exec(
          `SELECT iso_week, version, os, relay, count FROM counters WHERE iso_week >= ?`,
          oldestPublishedWeek(new Date()),
        )
        .toArray() as unknown as CounterRow[];
      return Response.json(rows);
    }
    return new Response(null, { status: 404 });
  }
}

function storeFor(env: Env): CounterStore {
  const stub = env.CENSUS.get(env.CENSUS.idFromName("global"));
  return {
    async increment(key: CounterKey): Promise<void> {
      await stub.fetch("https://census-do/increment", {
        method: "POST",
        body: JSON.stringify(key),
      });
    },
    async rows(): Promise<CounterRow[]> {
      const response = await stub.fetch("https://census-do/rows");
      return (await response.json()) as CounterRow[];
    },
  };
}

const worker: ExportedHandler<Env> = {
  async fetch(request: Request, env: Env): Promise<Response> {
    const path = new URL(request.url).pathname;
    const contentType = request.headers.get("content-type") ?? "";
    // Reject oversized requests via the declared Content-Length BEFORE
    // touching the body; bodies without a declared length (chunked/H2) are
    // read through a hard byte cap so they can never be fully buffered. The
    // body is only consumed at all for a plausible ping (POST /ping with a
    // JSON content type) — every other request is routed on headers alone.
    const declared = Number(request.headers.get("content-length") ?? "");
    const contentLength = Number.isFinite(declared) ? declared : undefined;
    let overflow = contentLength !== undefined && contentLength > MAX_BODY_BYTES;
    let bodyText = "";
    const wantsBody =
      request.method === "POST" &&
      path === "/ping" &&
      contentType.toLowerCase().startsWith("application/json");
    if (wantsBody && !overflow) {
      const read = await readBodyCapped(request.body, MAX_BODY_BYTES);
      bodyText = read.text;
      overflow = read.overflow;
    }
    const requestLike: CensusRequestLike = {
      method: request.method,
      path,
      contentType,
      bodyText,
    };
    if (overflow) {
      requestLike.contentLength = MAX_BODY_BYTES + 1;
    } else if (contentLength !== undefined) {
      requestLike.contentLength = contentLength;
    }
    const result = await handleCensusRequest(requestLike, storeFor(env), new Date());
    return new Response(result.body ?? null, {
      status: result.status,
      headers: result.headers ?? {},
    });
  },
};

export default worker;
