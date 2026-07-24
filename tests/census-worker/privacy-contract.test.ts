import { execFileSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import {
  ALLOWED_FIELDS,
  ALLOWED_OS,
  INSTALLATION_ID_PATTERN,
  MAX_VERSION_LENGTH,
  RELEASE_SMOKE_VERSION,
  VERSION_PATTERN,
  WITHDRAW_FIELDS,
} from "../../census-worker/src/census.js";

describe("Census privacy and cross-contract guards", () => {
  it("limits incoming Request reads to payload and explicit stats-auth headers", () => {
    expect(() =>
      execFileSync(
        process.execPath,
        ["scripts/test/check-census-worker-request-access.mjs"],
        {
          cwd: process.cwd(),
          stdio: "pipe",
        },
      ),
    ).not.toThrow();
  });

  it("stores only the SHA-256 ID hash and never exposes per-ID stats", () => {
    const index = readFileSync(
      join(process.cwd(), "census-worker", "src", "index.ts"),
      "utf8",
    );
    const stats = readFileSync(
      join(process.cwd(), "census-worker", "src", "stats.ts"),
      "utf8",
    );
    expect(index).toContain("id_hash TEXT PRIMARY KEY");
    expect(index).not.toContain("installation_id TEXT");
    expect(index).not.toContain("request.cf");
    expect(index).not.toContain("connecting-ip");
    expect(stats).toContain('"Cache-Control": "private, no-store"');
    expect(stats).toContain('"Content-Security-Policy"');
    expect(stats).toContain('"Referrer-Policy": "no-referrer"');
    expect(stats).toContain('"X-Content-Type-Options": "nosniff"');
    expect(index).toContain('request.path === "/stats/api"');
    expect(index).toContain("verifyCloudflareAccess");
    expect(index).toContain("admitNewInstallation");
    expect(index).toContain("MAX_BREAKDOWN_ROWS");
    expect(index).toContain("version = ? THEN 1 ELSE 0");
    expect(index).toContain("RELEASE_SMOKE_VERSION");
    expect(index).toContain("CENSUS_PING_RATE_LIMITER");
    expect(index).toContain("CENSUS_WITHDRAW_RATE_LIMITER");
    expect(index).toContain("mutationRateIdentity(request)");
    expect(index).toContain("request.localRequest");
    expect(index).toContain("now - RETENTION_DAYS * DAY_MS");
    expect(index).toContain(
      "DELETE FROM installations WHERE last_seen_at <= ?",
    );
    expect(index).toContain("await this.state.storage.setAlarm");
  });

  it("pins disabled Cloudflare logging and isolated mutation limiters", () => {
    const config = readFileSync(
      join(process.cwd(), "census-worker", "wrangler.toml"),
      "utf8",
    );
    expect(config).toMatch(/\[observability\]\s+enabled = false/);
    expect(config).toMatch(
      /\[observability\.logs\]\s+enabled = false\s+invocation_logs = false/,
    );
    expect(config).toContain('name = "CENSUS_PING_RATE_LIMITER"');
    expect(config).toContain('name = "CENSUS_WITHDRAW_RATE_LIMITER"');
    expect(config).not.toContain('name = "CENSUS_RATE_LIMITER"');
  });

  it("matches the Go schema-2 payload tags and validation vocabulary", () => {
    const cliDir = join(process.cwd(), "cli");
    const censusSources = readdirSync(cliDir)
      .filter(
        (name) =>
          name.startsWith("census") &&
          name.endsWith(".go") &&
          !name.endsWith("_test.go"),
      )
      .map((name) => readFileSync(join(cliDir, name), "utf8"))
      .join("\n");
    const structBody =
      censusSources.match(/type censusPayload struct \{([\s\S]*?)\n\}/)?.[1] ??
      "";
    const tags = [
      ...structBody.matchAll(/json:"([a-z0-9_]+)(?:,omitempty)?"/g),
    ].map((match) => match[1]);
    expect(tags.sort()).toEqual([...ALLOWED_FIELDS].sort());
    expect([...ALLOWED_OS].sort()).toEqual(["linux", "macos", "windows"]);
    expect(censusSources).toContain(
      "regexp.MustCompile(`" + VERSION_PATTERN.source + "`)",
    );
    expect(censusSources).toContain(
      `censusMaxVersionLength = ${MAX_VERSION_LENGTH}`,
    );
    const deploymentVerifier = readFileSync(
      join(
        process.cwd(),
        "scripts",
        "release",
        "verify-census-deployment.sh",
      ),
      "utf8",
    );
    expect(deploymentVerifier).toContain(
      `smoke_version="${RELEASE_SMOKE_VERSION}"`,
    );
    expect(censusSources).toContain(
      "regexp.MustCompile(`" + INSTALLATION_ID_PATTERN.source + "`)",
    );

    const withdrawalStruct =
      censusSources.match(
        /type censusWithdrawPayload struct \{([\s\S]*?)\n\}/,
      )?.[1] ?? "";
    const withdrawalTags = [
      ...withdrawalStruct.matchAll(/json:"([a-z0-9_]+)"/g),
    ].map((match) => match[1]);
    expect(withdrawalTags.sort()).toEqual([...WITHDRAW_FIELDS].sort());
  });

  it("documents the exact opt-in and withdrawal privacy boundary", () => {
    const privacy = readFileSync(join(process.cwd(), "PRIVACY.md"), "utf8");
    expect(privacy).toContain(
      "No installation report is sent unless you explicitly choose Yes.",
    );
    expect(privacy).toContain(
      '{"schema":2,"installation_id":"cns-0123456789abcdef0123456789abcdef"}',
    );
    expect(privacy).toContain(
      "A later opt-out or uninstall may send the deletion request",
    );
    expect(privacy).toMatch(
      /Cloudflare\s+processes its JSON and transport metadata/,
    );
  });

  it("keeps Worker source files below the project size limit", () => {
    for (const file of [
      "access.ts",
      "census.ts",
      "census-store.ts",
      "index.ts",
      "rate-limit.ts",
      "request-adapter.ts",
      "stats.ts",
      "storage-policy.ts",
    ]) {
      const source = readFileSync(
        join(process.cwd(), "census-worker", "src", file),
        "utf8",
      );
      expect(source.split("\n").length, file).toBeLessThanOrEqual(400);
    }
  });
});
