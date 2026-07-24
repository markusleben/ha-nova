import { localStatsAccess, verifyCloudflareAccess } from "./access.js";
import {
  CensusStore,
  InstallationRecord,
  InstallationStats,
  LegacyCounterKey,
  LegacyCounterRow,
  RELEASE_SMOKE_VERSION,
  handleMutationRequest,
  isoWeekUTC,
} from "./census.js";
import { adaptCensusRequest } from "./request-adapter.js";
import { mutationRateIdentity } from "./rate-limit.js";
import {
  MAX_BREAKDOWN_ROWS,
  MAX_INSTALLATIONS,
  admissionRejections,
  admitNewInstallation,
  boundedNumberRecord,
  ensureAdmissionTable,
} from "./storage-policy.js";
import {
  ACTIVE_DAYS,
  RETENTION_DAYS,
  buildPrivateStats,
  fetchRelayAppAnalytics,
  privateStatsHeaders,
  renderDashboard,
} from "./stats.js";

export interface Env {
  CENSUS: DurableObjectNamespace;
  CENSUS_PING_RATE_LIMITER: RateLimit;
  CENSUS_WITHDRAW_RATE_LIMITER: RateLimit;
  ACCESS_TEAM_DOMAIN?: string;
  ACCESS_AUD?: string;
  LOCAL_STATS_BYPASS_TOKEN?: string;
  CF_VERSION_METADATA?: {
    id: string;
    tag: string;
    timestamp: string;
  };
}

const CENSUS_OBJECT_NAME = "public-v0.21";
const MAX_LEGACY_ROWS_PER_WEEK = 256;
const DAY_MS = 86_400_000;
const LEGACY_HORIZON_MS = 26 * 7 * DAY_MS;

export class CensusCounter {
  private readonly state: DurableObjectState;
  private readonly sql: SqlStorage;

  constructor(state: DurableObjectState) {
    this.state = state;
    this.sql = state.storage.sql;
    this.sql.exec(
      `CREATE TABLE IF NOT EXISTS counters (
         iso_week TEXT NOT NULL,
         version TEXT NOT NULL,
         os TEXT NOT NULL,
         relay TEXT NOT NULL,
         count INTEGER NOT NULL DEFAULT 0,
         PRIMARY KEY (iso_week, version, os, relay)
       )`,
    );
    this.sql.exec(
      `CREATE TABLE IF NOT EXISTS installations (
         id_hash TEXT PRIMARY KEY,
         version TEXT NOT NULL,
         os TEXT NOT NULL,
         relay TEXT,
         first_seen_at INTEGER NOT NULL,
         last_seen_at INTEGER NOT NULL
       )`,
    );
    this.sql.exec(
      "CREATE INDEX IF NOT EXISTS installations_last_seen ON installations(last_seen_at)",
    );
    ensureAdmissionTable(this.sql);
  }

  private prune(now: number): void {
    this.sql.exec(
      "DELETE FROM installations WHERE last_seen_at <= ?",
      now - RETENTION_DAYS * DAY_MS,
    );
  }

  private async scheduleAlarm(): Promise<void> {
    const rows = this.sql
      .exec("SELECT MIN(last_seen_at) AS oldest FROM installations")
      .toArray() as unknown as { oldest: number | null }[];
    const oldest = rows[0]?.oldest;
    if (typeof oldest === "number") {
      await this.state.storage.setAlarm(oldest + RETENTION_DAYS * DAY_MS);
    } else {
      await this.state.storage.deleteAlarm();
    }
  }

  private stats(now: number): InstallationStats {
    this.prune(now);
    const activeCutoff = now - ACTIVE_DAYS * DAY_MS;
    const knownCutoff = now - RETENTION_DAYS * DAY_MS;
    const totalRows = this.sql
      .exec(
        `SELECT
           SUM(CASE WHEN last_seen_at > ? THEN 1 ELSE 0 END) AS active,
           SUM(CASE WHEN last_seen_at > ? THEN 1 ELSE 0 END) AS known,
           SUM(CASE WHEN last_seen_at > ? AND version = ? THEN 1 ELSE 0 END) AS release_smoke
         FROM installations`,
        activeCutoff,
        knownCutoff,
        activeCutoff,
        RELEASE_SMOKE_VERSION,
      )
      .toArray() as unknown as {
      active: number | null;
      known: number | null;
      release_smoke: number | null;
    }[];
    const byVersion = this.sql
      .exec(
        "SELECT version, COUNT(*) AS count FROM installations WHERE last_seen_at > ? GROUP BY version ORDER BY count DESC, version ASC LIMIT ?",
        activeCutoff,
        MAX_BREAKDOWN_ROWS,
      )
      .toArray() as unknown as Record<string, unknown>[];
    const byOS = this.sql
      .exec(
        "SELECT os, COUNT(*) AS count FROM installations WHERE last_seen_at > ? GROUP BY os",
        activeCutoff,
      )
      .toArray() as unknown as Record<string, unknown>[];
    const byRelay = this.sql
      .exec(
        "SELECT relay, COUNT(*) AS count FROM installations WHERE last_seen_at > ? AND relay IS NOT NULL GROUP BY relay ORDER BY count DESC, relay ASC LIMIT ?",
        activeCutoff,
        MAX_BREAKDOWN_ROWS,
      )
      .toArray() as unknown as Record<string, unknown>[];
    const missingRows = this.sql
      .exec(
        "SELECT COUNT(*) AS count FROM installations WHERE last_seen_at > ? AND relay IS NULL",
        activeCutoff,
      )
      .toArray() as unknown as { count: number }[];
    const active = totalRows[0]?.active ?? 0;
    const relayMissing = missingRows[0]?.count ?? 0;
    return {
      active_21_days: active,
      known_60_days: totalRows[0]?.known ?? 0,
      release_smoke_installations: totalRows[0]?.release_smoke ?? 0,
      by_version: boundedNumberRecord(byVersion, "version", active),
      by_os: boundedNumberRecord(byOS, "os", active),
      relay_versions: boundedNumberRecord(
        byRelay,
        "relay",
        active - relayMissing,
      ),
      relay_not_recently_observed: relayMissing,
      new_installation_rejections_today: admissionRejections(this.sql, now),
    };
  }

  async fetch(request: Request): Promise<Response> {
    const url = new URL(request.url);
    if (request.method === "POST" && url.pathname === "/installation") {
      const record = (await request.json()) as InstallationRecord;
      this.prune(record.observed_at);
      const exists =
        this.sql
          .exec(
            "SELECT 1 AS found FROM installations WHERE id_hash = ?",
            record.id_hash,
          )
          .toArray().length > 0;
      if (!exists) {
        const countRows = this.sql
          .exec("SELECT COUNT(*) AS count FROM installations")
          .toArray() as unknown as { count: number }[];
        if ((countRows[0]?.count ?? 0) >= MAX_INSTALLATIONS) {
          return Response.json(
            { error: "installation capacity reached" },
            { status: 507 },
          );
        }
        if (!admitNewInstallation(this.sql, record.observed_at)) {
          return Response.json(
            { error: "new installation admission limit reached" },
            { status: 429 },
          );
        }
      }
      this.sql.exec(
        `INSERT INTO installations
           (id_hash, version, os, relay, first_seen_at, last_seen_at)
         VALUES (?, ?, ?, ?, ?, ?)
         ON CONFLICT (id_hash) DO UPDATE SET
           version = excluded.version,
           os = excluded.os,
           relay = excluded.relay,
           last_seen_at = excluded.last_seen_at`,
        record.id_hash,
        record.version,
        record.os,
        record.relay ?? null,
        record.observed_at,
        record.observed_at,
      );
      await this.scheduleAlarm();
      return new Response(null, { status: 204 });
    }
    if (request.method === "DELETE" && url.pathname === "/installation") {
      const body = (await request.json()) as { id_hash: string };
      this.sql.exec(
        "DELETE FROM installations WHERE id_hash = ?",
        body.id_hash,
      );
      await this.scheduleAlarm();
      return new Response(null, { status: 204 });
    }
    if (request.method === "POST" && url.pathname === "/legacy") {
      const key = (await request.json()) as LegacyCounterKey;
      const weekRows = this.sql
        .exec(
          "SELECT version, os, relay FROM counters WHERE iso_week = ?",
          key.iso_week,
        )
        .toArray() as unknown as {
        version: string;
        os: string;
        relay: string;
      }[];
      const exact = weekRows.some(
        (row) =>
          row.version === key.version &&
          row.os === key.os &&
          row.relay === key.relay,
      );
      const overflow = weekRows.some(
        (row) =>
          row.version === "other" &&
          row.os === "other" &&
          row.relay === "other",
      );
      const stored =
        !exact && (overflow || weekRows.length >= MAX_LEGACY_ROWS_PER_WEEK - 1)
          ? { ...key, version: "other", os: "other", relay: "other" }
          : key;
      this.sql.exec(
        `INSERT INTO counters (iso_week, version, os, relay, count)
         VALUES (?, ?, ?, ?, 1)
         ON CONFLICT (iso_week, version, os, relay)
         DO UPDATE SET count = count + 1`,
        stored.iso_week,
        stored.version,
        stored.os,
        stored.relay,
      );
      return new Response(null, { status: 204 });
    }
    if (request.method === "GET" && url.pathname === "/stats") {
      const now = Number(url.searchParams.get("now"));
      if (!Number.isFinite(now)) {
        return new Response(null, { status: 400 });
      }
      return Response.json(this.stats(now));
    }
    if (request.method === "GET" && url.pathname === "/legacy") {
      const now = Number(url.searchParams.get("now"));
      const cutoffWeek = isoWeekUTC(new Date(now - LEGACY_HORIZON_MS));
      const rows = this.sql
        .exec(
          "SELECT iso_week, version, os, relay, count FROM counters WHERE iso_week >= ?",
          cutoffWeek,
        )
        .toArray() as unknown as LegacyCounterRow[];
      return Response.json(rows);
    }
    return new Response(null, { status: 404 });
  }

  async alarm(): Promise<void> {
    this.prune(Date.now());
    await this.scheduleAlarm();
  }
}

function storeFor(env: Env): CensusStore {
  const stub = env.CENSUS.get(env.CENSUS.idFromName(CENSUS_OBJECT_NAME));
  const checked = async (
    request: RequestInfo | URL,
    init?: RequestInit,
  ): Promise<Response> => {
    const response = await stub.fetch(request, init);
    if (!response.ok) {
      throw new Error(`Census storage HTTP ${response.status}`);
    }
    return response;
  };
  return {
    async upsertInstallation(record): Promise<void> {
      await checked("https://census-do/installation", {
        method: "POST",
        body: JSON.stringify(record),
      });
    },
    async deleteInstallation(idHash): Promise<void> {
      await checked("https://census-do/installation", {
        method: "DELETE",
        body: JSON.stringify({ id_hash: idHash }),
      });
    },
    async incrementLegacy(key): Promise<void> {
      await checked("https://census-do/legacy", {
        method: "POST",
        body: JSON.stringify(key),
      });
    },
    async installationStats(now): Promise<InstallationStats> {
      const response = await checked(
        `https://census-do/stats?now=${now.getTime()}`,
      );
      return (await response.json()) as InstallationStats;
    },
    async legacyRows(now): Promise<LegacyCounterRow[]> {
      const response = await checked(
        `https://census-do/legacy?now=${now.getTime()}`,
      );
      return (await response.json()) as LegacyCounterRow[];
    },
  };
}

async function statsAuthorized(
  request: Awaited<ReturnType<typeof adaptCensusRequest>>,
  env: Env,
): Promise<boolean> {
  if (
    request.localRequest &&
    localStatsAccess(request.localStatsToken, env.LOCAL_STATS_BYPASS_TOKEN)
  ) {
    return true;
  }
  return verifyCloudflareAccess(
    request.accessToken,
    env.ACCESS_TEAM_DOMAIN ?? "",
    env.ACCESS_AUD ?? "",
  );
}

const worker: ExportedHandler<Env> = {
  async fetch(incomingRequest: Request, env: Env): Promise<Response> {
    const request = await adaptCensusRequest(incomingRequest);
    const store = storeFor(env);
    if (request.path === "/stats" || request.path === "/stats/api") {
      if (!(await statsAuthorized(request, env))) {
        return new Response(null, { status: 403 });
      }
      const now = new Date();
      const [client, legacy, relay] = await Promise.all([
        store.installationStats(now),
        store.legacyRows(now),
        fetchRelayAppAnalytics(),
      ]);
      const stats = buildPrivateStats(client, legacy, relay, now);
      const api = request.path === "/stats/api";
      return new Response(
        api ? JSON.stringify(stats) : renderDashboard(stats),
        {
          status: 200,
          headers: privateStatsHeaders(api, env.CF_VERSION_METADATA),
        },
      );
    }
    const rateIdentity = await mutationRateIdentity(request);
    if (rateIdentity !== undefined) {
      const limiter =
        rateIdentity.route === "ping"
          ? env.CENSUS_PING_RATE_LIMITER
          : env.CENSUS_WITHDRAW_RATE_LIMITER;
      const limit = await limiter.limit({ key: rateIdentity.key });
      if (!limit.success) {
        return new Response(null, { status: 429 });
      }
    }
    const result = await handleMutationRequest(request, store, new Date());
    return new Response(result.body ?? null, {
      status: result.status,
      headers: new Headers(result.headers ?? {}),
    });
  },
};

export default worker;
