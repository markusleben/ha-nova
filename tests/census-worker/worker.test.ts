import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import {
  ALLOWED_FIELDS,
  ALLOWED_OS,
  CounterKey,
  CounterRow,
  CounterStore,
  MAX_VERSIONS_PER_WEEK,
  OVERFLOW_BUCKET,
  WEEKLY_HORIZON_WEEKS,
  buildStats,
  clampWeeklyCardinality,
  handleCensusRequest,
  isoWeekUTC,
  validatePing,
} from "../../census-worker/src/census.js";

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
    const result = await handleCensusRequest(
      ping({ schema: 1, version: "0.21.0", os: "macos" }, { contentType: "text/plain" }),
      memoryStore(),
      NOW,
    );
    expect(result.status).toBe(415);
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

  it("serves stats with weekly series, breakdowns, unknown bucket, and the monthly lower bound", async () => {
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
    expect(stats.monthly_lower_bound).toBe(5); // max of the last 4 weeks, not a sum
    expect(stats.window_weeks).toBe(4);
    expect(stats.footnotes.counting).toContain("lower bound");
    expect(stats.footnotes.identifiers).toContain("no identifier");
  });

  it("folds distinct versions beyond the weekly cap into the other bucket", async () => {
    const store = memoryStore();
    for (let i = 0; i < MAX_VERSIONS_PER_WEEK; i++) {
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
    // An already-known version still lands in its own row.
    await handleCensusRequest(ping({ schema: 1, version: "1.0.0", os: "macos" }), store, NOW);
    expect(store.all().find((row) => row.version === "1.0.0")?.count).toBe(2);
  });

  it("holds the cardinality cap under interleaved pings at the boundary", async () => {
    const store = memoryStore();
    for (let i = 0; i < MAX_VERSIONS_PER_WEEK - 1; i++) {
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
    expect(new Set(versions).size).toBe(MAX_VERSIONS_PER_WEEK + 1); // cap + the overflow bucket
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
  });
});
