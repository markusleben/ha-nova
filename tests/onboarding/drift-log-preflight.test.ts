/**
 * #973: the release preflight fails on a stale upstream drift log.
 */
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

const SCRIPT = "scripts/release/verify-drift-log-fresh.sh";
const dirs: string[] = [];

function run(log: string | null, today: string) {
  const dir = mkdtempSync(join(tmpdir(), "drift-log-"));
  dirs.push(dir);
  const path = join(dir, "upstream-drift-log.md");
  if (log !== null) writeFileSync(path, log);
  const r = spawnSync("bash", [SCRIPT], {
    encoding: "utf8",
    env: { ...process.env, HA_NOVA_DRIFT_LOG: path, HA_NOVA_DRIFT_TODAY: today },
  });
  return { code: r.status, out: r.stdout + r.stderr };
}

const log = (...dates: string[]) =>
  "# Upstream Drift Log\n\n" + dates.map((d) => `## ${d} — pre-release screen: HA\n\n- CLEAN\n`).join("\n");

afterEach(() => {
  for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true });
});

describe("release preflight: upstream drift log freshness", () => {
  it("passes when the newest entry is at most 35 days old", () => {
    expect(run(log("2026-08-05", "2026-11-05"), "2026-11-05").code).toBe(0);
    expect(run(log("2026-10-01"), "2026-11-05").code).toBe(0);
  });

  it("fails on an entry older than 35 days and names the log and the rule", () => {
    const r = run(log("2026-08-05", "2026-09-30"), "2026-11-05");
    expect(r.code).toBe(1);
    expect(r.out).toContain("newest screen in");
    expect(r.out).toContain("2026-09-30, 36 days ago (limit 35)");
    expect(r.out).toContain("upstream-drift-log.md");
    expect(r.out).toContain("AGENTS.md (Releases)");
  });

  it("counts a deep-audit screen and a screen after a closed fence", () => {
    expect(run("# Log\n\n## 2026-10-20 — deep-audit screen: HA\n", "2026-11-05").code).toBe(0);
    expect(run("# Log\n\n```\nx\n```\n\n## 2026-10-20 — pre-release screen (v1): HA\n", "2026-11-05").code).toBe(0);
  });

  it("uses the newest entry, not the last one in the file", () => {
    expect(run(log("2026-11-01", "2026-08-05"), "2026-11-05").code).toBe(0);
  });

  it("counts only screens, not ad hoc checks, corrections, fenced or commented headings", () => {
    const r = run(
      log("2026-09-01") +
        "\n## 2026-11-01 — ad hoc check (Marketplace)\n\n## 2026-11-02 — correction to the previous screen\n" +
        "\n```md\n## 2026-11-03 — pre-release screen: template\n```\n" +
        "\n   ~~~~\n## 2026-11-03 — pre-release screen: indented, longer fence\n~~~\n## 2026-11-03 — pre-release screen: still fenced\n   ~~~~\n" +
        "\n<!--\n## 2026-11-04 — pre-release screen: draft\n-->\n\n## 2026-11-04-draft screen\n",
      "2026-11-05",
    );
    expect(r.code).toBe(1);
    expect(r.out).toContain("2026-09-01, 65 days ago");
  });

  it("allows one day of clock slack and refuses a later date", () => {
    expect(run(log("2026-11-06"), "2026-11-05").code).toBe(0);
    const r = run(log("2026-11-07"), "2026-11-05");
    expect(r.code).toBe(1);
    expect(r.out).toContain("after today");
  });

  it("fails closed on a missing log, no screen, or an invalid date", () => {
    expect(run(null, "2026-11-05").out).toContain("cannot read");
    expect(run("# Upstream Drift Log\n\nno entries\n", "2026-11-05").out).toContain("has no screen");
    expect(run(log("2026-02-30"), "2026-11-05").out).toContain("is not a date");
    expect(run(log("2026-11-01"), "2026-13-01").out).toContain("today is not a date");
    for (const r of [run(null, "2026-11-05"), run(log("2026-02-30"), "2026-11-05")]) expect(r.code).toBe(1);
  });

  it("is wired into npm and the release runbook", () => {
    expect(JSON.parse(readFileSync("package.json", "utf8")).scripts["verify:drift-log"]).toBe(`bash ${SCRIPT}`);
    expect(readFileSync("docs/releasing.md", "utf8")).toContain("npm run verify:drift-log");
  });
});
