// Cloudflare wiring for the HA NOVA census: one SQLite Durable Object holding
// aggregate counter rows (iso_week, version, os, relay) -> count. All request
// logic lives in the pure core (census.ts); this file only adapts it to the
// workers runtime. HA NOVA does not read source-IP metadata and stores no
// identifiers, IPs, or client timestamps — see PRIVACY.md and
// docs/reference/census.md in the repo root.

import {
  CounterKey,
  CounterRow,
  CounterStore,
  clampWeeklyCardinality,
  handleCensusRequest,
  oldestPublishedWeek,
} from "./census";
import { adaptCensusRequest } from "./request-adapter";

export interface Env {
  CENSUS: DurableObjectNamespace;
  CF_VERSION_METADATA?: {
    id: string;
    tag: string;
    timestamp: string;
  };
}

// One-time clean namespace for the first stable census launch. The earlier
// "global" object contains deployment-smoke pings from before the client
// feature shipped. Do not change this name after v0.21 without an explicit
// data-migration decision.
const CENSUS_OBJECT_NAME = "public-v0.21";

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

  async fetch(internalRequest: Request): Promise<Response> {
    const path = new URL(internalRequest.url).pathname;
    if (internalRequest.method === "POST" && path === "/increment") {
      const key = (await internalRequest.json()) as CounterKey;
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
    if (internalRequest.method === "GET" && path === "/rows") {
      // Bound the scan to the published stats horizon: counters are retained
      // forever, but /stats only publishes WEEKLY_HORIZON_WEEKS — loading the
      // whole table would grow without bound. Within the horizon the row
      // count is bounded by construction to MAX_ROWS_PER_WEEK per week.
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
  const stub = env.CENSUS.get(env.CENSUS.idFromName(CENSUS_OBJECT_NAME));
  return {
    async increment(key: CounterKey): Promise<void> {
      const response = await stub.fetch("https://census-do/increment", {
        method: "POST",
        body: JSON.stringify(key),
      });
      // A failed storage write must never be masked as a 204: stub.fetch
      // resolves on non-2xx too (bad migration/route, storage failure) —
      // throw so the ping answers 5xx and the loss is visible.
      if (!response.ok) {
        throw new Error(`counter write failed: HTTP ${response.status}`);
      }
    },
    async rows(): Promise<CounterRow[]> {
      const response = await stub.fetch("https://census-do/rows");
      if (!response.ok) {
        throw new Error(`counter read failed: HTTP ${response.status}`);
      }
      return (await response.json()) as CounterRow[];
    },
  };
}

const worker: ExportedHandler<Env> = {
  async fetch(incomingRequest: Request, env: Env): Promise<Response> {
    // Reject oversized requests before unbounded buffering. The isolated
    // adapter is source-contract checked so it cannot start reading transport
    // metadata without an explicit privacy-contract change.
    const requestLike = await adaptCensusRequest(incomingRequest);
    const path = requestLike.path;
    const result = await handleCensusRequest(requestLike, storeFor(env), new Date());
    const headers = new Headers(result.headers ?? {});
    if (path === "/stats" && result.status === 200 && env.CF_VERSION_METADATA) {
      // The release deploy wrapper stamps its reviewed SHA as the Cloudflare
      // version tag. Returning both version fields lets the external gate
      // prove it reached that exact deployment instead of stale production.
      headers.set("X-HA-NOVA-Deployment-SHA", env.CF_VERSION_METADATA.tag);
      headers.set("X-HA-NOVA-Version-ID", env.CF_VERSION_METADATA.id);
    }
    return new Response(result.body ?? null, {
      status: result.status,
      headers,
    });
  },
};

export default worker;
