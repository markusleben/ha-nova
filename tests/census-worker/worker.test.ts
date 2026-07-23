import { execFileSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import {
  ALLOWED_FIELDS,
  ALLOWED_OS,
  CounterKey,
  CounterRow,
  CounterStore,
  MAX_BODY_BYTES,
  MAX_ROWS_PER_WEEK,
  MAX_VERSION_LENGTH,
  OVERFLOW_BUCKET,
  VERSION_PATTERN,
  WEEKLY_HORIZON_WEEKS,
  buildStats,
  clampWeeklyCardinality,
  handleCensusRequest,
  isoWeekUTC,
  oldestPublishedWeek,
  readBodyCapped,
  validatePing,
} from "../../census-worker/src/census.js";

function streamOf(...chunks: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  return new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) {
        controller.enqueue(encoder.encode(chunk));
      }
      controller.close();
    },
  });
}

function memoryStore(): CounterStore & { all: () => CounterRow[] } {
  const counters = new Map<string, CounterRow>();
  // Mirrors the Durable Object's atomicity contract: clamp + write happen in
  // one synchronous block with no await point inside.
  const applySync = (key: CounterKey): void => {
    const clamped = clampWeeklyCardinality([...counters.values()], key);
    const id = `${clamped.iso_week}|${clamped.version}|${clamped.os}|${clamped.relay}`;
    const existing = counters.get(id);
    if (existing) {
      existing.count += 1;
    } else {
      counters.set(id, { ...clamped, count: 1 });
    }
  };
  return {
    async increment(key: CounterKey): Promise<void> {
      // Interleaving may happen up to here (like the DO's request parsing),
      // but never inside the clamp+write step below.
      await Promise.resolve();
      applySync(key);
    },
    async rows(): Promise<CounterRow[]> {
      return [...counters.values()];
    },
    all: () => [...counters.values()],
  };
}

const NOW = new Date("2026-07-22T12:00:00Z");

function ping(body: unknown, overrides: Partial<{ method: string; path: string; contentType: string; bodyText: string }> = {}) {
  return {
    method: "POST",
    path: "/ping",
    contentType: "application/json",
    bodyText: typeof body === "string" ? body : JSON.stringify(body),
    ...overrides,
  };
}

describe("census worker validation matrix", () => {
  it("accepts the exact client payload with and without relay and returns 204", async () => {
    const store = memoryStore();
    const withRelay = await handleCensusRequest(
      ping({ schema: 1, version: "0.21.0", relay: "0.7.0", os: "macos" }),
      store,
      NOW,
    );
    expect(withRelay.status).toBe(204);
    const withoutRelay = await handleCensusRequest(
      ping({ schema: 1, version: "0.21.0", os: "linux" }),
      store,
      NOW,
    );
    expect(withoutRelay.status).toBe(204);
    // The relay-less ping lands in the "unknown" bucket.
    expect(store.all().map((row) => row.relay).sort()).toEqual(["0.7.0", "unknown"]);
  });

  it("rejects non-POST on /ping with 405", async () => {
    const result = await handleCensusRequest(ping("", { method: "GET" }), memoryStore(), NOW);
    expect(result.status).toBe(405);
  });

  it("rejects wrong content types with 415", async () => {
    for (const contentType of ["text/plain", "application/jsonp", "application/json-patch+json"]) {
      const result = await handleCensusRequest(
        ping({ schema: 1, version: "0.21.0", os: "macos" }, { contentType }),
        memoryStore(),
        NOW,
      );
      expect(result.status, contentType).toBe(415);
    }
  });

  it("accepts application/json media-type parameters", async () => {
    const result = await handleCensusRequest(
      ping(
        { schema: 1, version: "0.21.0", os: "macos" },
        { contentType: "Application/JSON; charset=utf-8" },
      ),
      memoryStore(),
      NOW,
    );
    expect(result.status).toBe(204);
  });

  it("rejects bodies over 512 bytes with 413", async () => {
    const result = await handleCensusRequest(
      ping(`{"schema":1,"version":"0.21.0","os":"macos","x":"${"a".repeat(600)}"}`),
      memoryStore(),
      NOW,
    );
    expect(result.status).toBe(413);
  });

  it("rejects an oversized declared Content-Length with 413 before reading a body", async () => {
    const result = await handleCensusRequest(
      { ...ping(""), bodyText: "", contentLength: 100000 },
      memoryStore(),
      NOW,
    );
    expect(result.status).toBe(413);
    // A truthful small declared length falls through to normal validation.
    const ok = await handleCensusRequest(
      { ...ping({ schema: 1, version: "0.21.0", os: "macos" }), contentLength: 45 },
      memoryStore(),
      NOW,
    );
    expect(ok.status).toBe(204);
  });

  it("rejects unknown fields and bad shapes with 400", async () => {
    const cases: unknown[] = [
      { schema: 1, version: "0.21.0", os: "macos", uuid: "nope" }, // unknown field
      { schema: 2, version: "0.21.0", os: "macos" }, // wrong schema
      { schema: 1, os: "macos" }, // missing version
      { schema: 1, version: "0.21.0" }, // missing os
      { schema: 1, version: "not-semver", os: "macos" },
      { schema: 1, version: "0.21.0.1", os: "macos" },
      { schema: 1, version: `0.21.${"9".repeat(30)}`, os: "macos" }, // over 32 chars
      { schema: 1, version: "0.21.0", os: "freebsd" }, // outside the os clamp
      { schema: 1, version: "0.21.0", os: "macos", relay: "latest" },
      { schema: 1, version: "0.21.0", os: 7 },
      [1, 2, 3],
      "not an object",
    ];
    for (const body of cases) {
      const result = await handleCensusRequest(ping(body), memoryStore(), NOW);
      expect(result.status, JSON.stringify(body)).toBe(400);
    }
    expect((await handleCensusRequest(ping("{broken"), memoryStore(), NOW)).status).toBe(400);
  });

  it("accepts rc versions per the documented pattern", () => {
    expect(validatePing(JSON.stringify({ schema: 1, version: "0.21.0-rc2", os: "windows" })).ok).toBe(true);
    expect(validatePing(JSON.stringify({ schema: 1, version: "0.21.0-rc", os: "windows" })).ok).toBe(false);
  });

  it("answers 404 for everything else", async () => {
    const store = memoryStore();
    expect((await handleCensusRequest(ping("", { path: "/" , method: "GET"}), store, NOW)).status).toBe(404);
    expect((await handleCensusRequest(ping({}, { path: "/stats", method: "POST" }), store, NOW)).status).toBe(404);
    expect((await handleCensusRequest(ping("", { path: "/admin", method: "GET" }), store, NOW)).status).toBe(404);
  });
});

describe("census worker aggregation", () => {
  it("counts pings into weekly counter rows", async () => {
    const store = memoryStore();
    const body = { schema: 1, version: "0.21.0", relay: "0.7.0", os: "macos" };
    await handleCensusRequest(ping(body), store, NOW);
    await handleCensusRequest(ping(body), store, NOW);
    const rows = store.all();
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ iso_week: isoWeekUTC(NOW), count: 2 });
  });

  it("computes iso weeks like the Go client (UTC, padded, iso-year aware)", () => {
    expect(isoWeekUTC(new Date("2026-01-01T12:00:00Z"))).toBe("2026-W01");
    expect(isoWeekUTC(new Date("2027-01-01T12:00:00Z"))).toBe("2026-W53");
    expect(isoWeekUTC(new Date("2026-02-03T00:00:00Z"))).toBe("2026-W06");
  });

  it("serves stats with weekly series, breakdowns, unknown bucket, and an honest peak", async () => {
    const week = (offsetWeeks: number) =>
      isoWeekUTC(new Date(NOW.getTime() - offsetWeeks * 7 * 86400000));
    const rows: CounterRow[] = [
      { iso_week: week(0), version: "0.21.0", os: "macos", relay: "0.7.0", count: 3 },
      { iso_week: week(1), version: "0.21.0", os: "linux", relay: "unknown", count: 5 },
      { iso_week: week(2), version: "0.20.0", os: "windows", relay: "0.6.0", count: 2 },
      // Outside the 4-week window: appears in the weekly series only.
      { iso_week: week(6), version: "0.19.0", os: "macos", relay: "unknown", count: 9 },
    ];
    const stats = buildStats(rows, NOW);
    expect(stats.weekly.map((entry) => entry.iso_week)).toEqual(
      [week(6), week(2), week(1), week(0)].sort(),
    );
    expect(stats.by_os).toEqual({ macos: 3, linux: 5, windows: 2 });
    expect(stats.by_version).toEqual({ "0.21.0": 8, "0.20.0": 2 });
    expect(stats.by_relay).toEqual({ "0.7.0": 3, unknown: 5, "0.6.0": 2 });
    expect(stats.peak_weekly_pings).toBe(5); // max of the last 4 weeks, not a sum
    expect(stats).not.toHaveProperty("monthly_lower_bound");
    expect(stats.window_weeks).toBe(4);
    expect(stats.footnotes.counting).toContain("not verified unique installs");
    expect(stats.footnotes.counting).toContain("fabricated pings");
    expect(stats.footnotes.counting).toContain("other overflow bucket");
    expect(stats.footnotes.identifiers).toContain(
      "application JSON contains no installation, device, or user identifier",
    );
    expect(stats.footnotes.identifiers).toContain(
      "Cloudflare is the hosting provider",
    );
    expect(stats.footnotes.identifiers).toContain(
      "Worker code does not read the source IP",
    );
  });

  it("starts the stable public census on a clean counter namespace", () => {
    const indexSource = readFileSync(join(process.cwd(), "census-worker", "src", "index.ts"), "utf8");
    expect(indexSource).toContain('const CENSUS_OBJECT_NAME = "public-v0.21"');
    expect(indexSource).toContain("idFromName(CENSUS_OBJECT_NAME)");
    expect(indexSource).not.toContain('idFromName("global")');
  });

  it("positively allowlists every incoming Worker Request access", () => {
    expect(() =>
      execFileSync(process.execPath, ["scripts/test/check-census-worker-request-access.mjs"], {
        cwd: process.cwd(),
        stdio: "pipe",
      }),
    ).not.toThrow();
  });

  it("exposes exact Cloudflare deployment metadata on public stats", () => {
    const indexSource = readFileSync(join(process.cwd(), "census-worker", "src", "index.ts"), "utf8");
    const wrangler = readFileSync(join(process.cwd(), "census-worker", "wrangler.toml"), "utf8");
    expect(wrangler).toContain('account_id = "58e387e1204bdfe78781caca64f2cd15"');
    expect(wrangler).toMatch(/\[version_metadata\]\s+binding = "CF_VERSION_METADATA"/);
    expect(wrangler).toMatch(
      /\[observability\]\s+enabled = false\s+\[observability\.logs\]\s+enabled = false\s+invocation_logs = false/,
    );
    expect(indexSource).toContain("env.CF_VERSION_METADATA.tag");
    expect(indexSource).toContain("env.CF_VERSION_METADATA.id");
    expect(indexSource).toContain('headers.set("X-HA-NOVA-Deployment-SHA"');
    expect(indexSource).toContain('headers.set("X-HA-NOVA-Version-ID"');
  });

  it("caps streamed bodies without a Content-Length instead of buffering them", async () => {
    // Chunked/H2 upload: no declared length — the capped reader must stop the
    // moment the cap is crossed and report overflow (which maps to 413).
    const big = await readBodyCapped(streamOf("x".repeat(400), "y".repeat(400)), MAX_BODY_BYTES);
    expect(big.overflow).toBe(true);
    expect(big.text).toBe("");
    const small = await readBodyCapped(streamOf('{"schema":1,', '"version":"0.21.0","os":"macos"}'), MAX_BODY_BYTES);
    expect(small.overflow).toBe(false);
    expect(JSON.parse(small.text).os).toBe("macos");
    const empty = await readBodyCapped(null, MAX_BODY_BYTES);
    expect(empty).toEqual({ text: "", overflow: false });
    // The overflow signal maps to the same 413 as a declared oversize.
    const result = await handleCensusRequest(
      { ...ping(""), bodyText: "", contentLength: MAX_BODY_BYTES + 1 },
      memoryStore(),
      NOW,
    );
    expect(result.status).toBe(413);
  });

  it("answers 5xx when the counter write fails instead of masking it as 204", async () => {
    // A store whose write fails (the DO answering non-2xx maps to a throw in
    // index.ts's storeFor) must surface as a server error, never a 204.
    const failingStore: CounterStore = {
      async increment(): Promise<void> {
        throw new Error("counter write failed: HTTP 404");
      },
      async rows(): Promise<CounterRow[]> {
        return [];
      },
    };
    const result = await handleCensusRequest(
      ping({ schema: 1, version: "0.21.0", os: "macos" }),
      failingStore,
      NOW,
    );
    expect(result.status).toBe(500);
    // And the worker wiring actually checks the DO response status.
    const indexSource = readFileSync(join(process.cwd(), "census-worker", "src", "index.ts"), "utf8");
    expect(indexSource).toContain("if (!response.ok)");
    expect(indexSource).toContain("counter write failed");
  });

  it("folds new combinations into one overflow row at the total weekly cap", async () => {
    const store = memoryStore();
    for (let i = 0; i < MAX_ROWS_PER_WEEK - 1; i++) {
      const result = await handleCensusRequest(
        ping({ schema: 1, version: `${i}.0.0`, os: "macos" }),
        store,
        NOW,
      );
      expect(result.status).toBe(204);
    }
    // The cap is reached: one more DISTINCT version must not mint a new row.
    expect((await handleCensusRequest(ping({ schema: 1, version: "999.0.0", os: "macos" }), store, NOW)).status).toBe(204);
    const versions = store.all().map((row) => row.version);
    expect(versions).not.toContain("999.0.0");
    expect(versions).toContain(OVERFLOW_BUCKET);
    expect(store.all()).toHaveLength(MAX_ROWS_PER_WEEK);
    // An already-known version still lands in its own row.
    await handleCensusRequest(ping({ schema: 1, version: "1.0.0", os: "macos" }), store, NOW);
    expect(store.all().find((row) => row.version === "1.0.0")?.count).toBe(2);
  });

  it("holds the cardinality cap under interleaved pings at the boundary", async () => {
    const store = memoryStore();
    for (let i = 0; i < MAX_ROWS_PER_WEEK - 2; i++) {
      expect(
        (await handleCensusRequest(ping({ schema: 1, version: `${i}.0.0`, os: "macos" }), store, NOW)).status,
      ).toBe(204);
    }
    // Two concurrent pings with distinct fabricated versions race for the last
    // free slot: the store's atomic clamp+write must admit exactly one and
    // fold the other into the overflow bucket — never two new rows.
    await Promise.all([
      handleCensusRequest(ping({ schema: 1, version: "700.0.0", os: "macos" }), store, NOW),
      handleCensusRequest(ping({ schema: 1, version: "701.0.0", os: "macos" }), store, NOW),
    ]);
    const versions = store.all().map((row) => row.version);
    expect(versions.filter((v) => v === "700.0.0" || v === "701.0.0")).toHaveLength(1);
    expect(versions).toContain(OVERFLOW_BUCKET);
    expect(store.all()).toHaveLength(MAX_ROWS_PER_WEEK);
  });

  it("caps an adversarial version-relay-os Cartesian product", async () => {
    const store = memoryStore();
    for (let version = 0; version < 20; version++) {
      for (let relay = 0; relay < 20; relay++) {
        for (const os of ALLOWED_OS) {
          const result = await handleCensusRequest(
            ping({ schema: 1, version: `${version}.0.0`, relay: `${relay}.0.0`, os }),
            store,
            NOW,
          );
          expect(result.status).toBe(204);
        }
      }
    }
    expect(store.all().length).toBeLessThanOrEqual(MAX_ROWS_PER_WEEK);
    expect(store.all()).toContainEqual(
      expect.objectContaining({
        version: OVERFLOW_BUCKET,
        relay: OVERFLOW_BUCKET,
        os: OVERFLOW_BUCKET,
      }),
    );
  });

  it("trims the public weekly series to the fixed horizon", () => {
    const week = (offsetWeeks: number) =>
      isoWeekUTC(new Date(NOW.getTime() - offsetWeeks * 7 * 86400000));
    const rows: CounterRow[] = [
      { iso_week: week(0), version: "0.21.0", os: "macos", relay: "unknown", count: 1 },
      { iso_week: week(WEEKLY_HORIZON_WEEKS + 4), version: "0.10.0", os: "macos", relay: "unknown", count: 7 },
    ];
    const stats = buildStats(rows, NOW);
    expect(stats.weekly.map((entry) => entry.iso_week)).toEqual([week(0)]);
  });

  it("bounds the rows the stats path needs to the published horizon", () => {
    // The DO loads only rows >= oldestPublishedWeek (string order is
    // chronological for the padded labels); buildStats over the bounded set
    // must equal buildStats over the full table — the bound loses nothing.
    expect(oldestPublishedWeek(NOW)).toBe(
      isoWeekUTC(new Date(NOW.getTime() - (WEEKLY_HORIZON_WEEKS - 1) * 7 * 86400000)),
    );
    const week = (offsetWeeks: number) =>
      isoWeekUTC(new Date(NOW.getTime() - offsetWeeks * 7 * 86400000));
    const allRows: CounterRow[] = [
      { iso_week: week(0), version: "0.21.0", os: "macos", relay: "unknown", count: 2 },
      { iso_week: week(WEEKLY_HORIZON_WEEKS - 1), version: "0.20.0", os: "linux", relay: "unknown", count: 3 },
      // Ancient rows that the bounded query would not even load:
      { iso_week: week(WEEKLY_HORIZON_WEEKS + 10), version: "0.1.0", os: "macos", relay: "unknown", count: 9 },
      { iso_week: week(200), version: "0.0.1", os: "windows", relay: "0.1.0", count: 4 },
    ];
    const bounded = allRows.filter((row) => row.iso_week >= oldestPublishedWeek(NOW));
    expect(bounded).toHaveLength(2);
    expect(JSON.stringify(buildStats(bounded, NOW))).toBe(JSON.stringify(buildStats(allRows, NOW)));
    // And the Durable Object actually queries with that bound.
    const indexSource = readFileSync(join(process.cwd(), "census-worker", "src", "index.ts"), "utf8");
    expect(indexSource).toContain("WHERE iso_week >= ?");
    expect(indexSource).toContain("oldestPublishedWeek(new Date())");
  });

  it("serves /stats with public cache and CORS headers", async () => {
    const store = memoryStore();
    await handleCensusRequest(ping({ schema: 1, version: "0.21.0", os: "macos" }), store, NOW);
    const result = await handleCensusRequest(
      { method: "GET", path: "/stats", contentType: "", bodyText: "" },
      store,
      NOW,
    );
    expect(result.status).toBe(200);
    expect(result.headers?.["Cache-Control"]).toBe("public, max-age=3600");
    expect(result.headers?.["Access-Control-Allow-Origin"]).toBe("*");
    const stats = JSON.parse(result.body ?? "{}");
    expect(stats.weekly).toEqual([{ iso_week: isoWeekUTC(NOW), count: 1 }]);
    expect(stats.peak_weekly_pings).toBe(1);
  });
});

describe("census cross-contract (client payload == worker allowlist)", () => {
  it("matches the json tags of the Go payload struct exactly", () => {
    const cliDir = join(process.cwd(), "cli");
    const censusSources = readdirSync(cliDir)
      .filter((name) => name.startsWith("census") && name.endsWith(".go") && !name.endsWith("_test.go"))
      .map((name) => readFileSync(join(cliDir, name), "utf8"))
      .join("\n");
    const structMatch = censusSources.match(
      /type censusPayload struct \{([\s\S]*?)\n\}/,
    );
    expect(structMatch, "cli/census*.go must define censusPayload").toBeTruthy();
    const structBody = structMatch?.[1] ?? "";
    // Every struct field must carry a tag the extractor understands — an
    // unmatched (e.g. renamed or oddly-formed) tag fails the count equality
    // instead of silently dropping out of the comparison.
    const fieldLines = structBody
      .split("\n")
      .filter((line) => line.includes("`json:"));
    const tags = [...structBody.matchAll(/json:"([a-z0-9_]+)(?:,omitempty)?"/g)].map(
      (match) => match[1],
    );
    expect(tags).toHaveLength(fieldLines.length);
    expect(tags.sort()).toEqual([...ALLOWED_FIELDS].sort());
    // And the os vocabulary stays pinned on both sides.
    expect([...ALLOWED_OS].sort()).toEqual(["linux", "macos", "windows"]);
    expect(censusSources).toContain('case "macos", "windows", "linux":');
    // The client-side relay-version filter must accept exactly what the
    // worker accepts: same regex source, same length cap.
    expect(censusSources).toContain("regexp.MustCompile(`" + VERSION_PATTERN.source + "`)");
    expect(censusSources).toContain(`censusMaxVersionLength = ${MAX_VERSION_LENGTH}`);
  });
});
