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
  handleCensusRequest,
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
      this.sql.exec(
        `INSERT INTO counters (iso_week, version, os, relay, count)
         VALUES (?, ?, ?, ?, 1)
         ON CONFLICT (iso_week, version, os, relay)
         DO UPDATE SET count = count + 1`,
        key.iso_week,
        key.version,
        key.os,
        key.relay,
      );
      return new Response(null, { status: 204 });
    }
    if (request.method === "GET" && path === "/rows") {
      const rows = this.sql
        .exec(`SELECT iso_week, version, os, relay, count FROM counters`)
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
    // Reject oversized requests via the declared Content-Length BEFORE
    // buffering the body; the core re-checks the actual bytes.
    const declared = Number(request.headers.get("content-length") ?? "");
    const contentLength = Number.isFinite(declared) ? declared : undefined;
    const oversized = contentLength !== undefined && contentLength > MAX_BODY_BYTES;
    const requestLike: CensusRequestLike = {
      method: request.method,
      path: new URL(request.url).pathname,
      contentType: request.headers.get("content-type") ?? "",
      bodyText: request.method === "POST" && !oversized ? await request.text() : "",
    };
    if (contentLength !== undefined) {
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
