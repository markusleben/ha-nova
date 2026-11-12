import { chmodSync, copyFileSync, existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { spawnSync } from "node:child_process";

import { describe, expect, it } from "vitest";

import { makeTempDir } from "../helpers/temp-dir";

// up.sh writes .state/owner; down.sh tears a bed down only for its owner
// (same checkout, same TESTBED_OWNER). A fake docker on PATH stands in for
// Docker: FAKE_BED_DIR is the checkout the running bed was started from.
const FAKE_DOCKER = [
  "#!/usr/bin/env bash",
  'echo "$*" >> "$FAKE_LOG"',
  'case "$1" in',
  "  info) exit 0 ;;",
  '  ps) [[ -z "${FAKE_BED_DIR:-}" ]] || echo "$FAKE_BED_DIR"; exit 0 ;;',
  "  run)",
  '    for a in "$@"; do [[ "$a" == *:/target ]] && rm -rf "${a%:/target}"/* "${a%:/target}"/.[!.]*; done',
  "    exit 0 ;;",
  "  compose)",
  '    case " $* " in',
  '      *" version "*) exit 0 ;;',
  '      *" ps -q "*) [[ -z "${FAKE_BED_DIR:-}" ]] || echo abc123; exit 0 ;;',
  '      *" run "*) exit 1 ;;',
  "      *) exit 0 ;;",
  "    esac ;;",
  "esac",
  "exit 0",
  "",
].join("\n");

function fixture() {
  const root = makeTempDir("ha-nova-testbed-owner-");
  const here = join(root, "scripts/e2e/testbed");
  const bin = join(root, "bin");
  const overlay = join(root, "overlay");
  mkdirSync(join(here, "config"), { recursive: true });
  mkdirSync(join(overlay, "config"), { recursive: true });
  mkdirSync(bin);
  for (const f of ["up.sh", "down.sh"]) copyFileSync(join("scripts/e2e/testbed", f), join(here, f));
  writeFileSync(join(bin, "docker"), FAKE_DOCKER);
  chmodSync(join(bin, "docker"), 0o755);
  const log = join(root, "docker.log");
  writeFileSync(log, "");
  // down.sh compares the physical checkout path (macOS /var -> /private/var).
  const physical = spawnSync("bash", ["-c", 'cd -P "$1" && pwd', "_", here], { encoding: "utf8" }).stdout.trim();
  const run = (script: string, env: Record<string, string> = {}, args: string[] = []) => {
    const r = spawnSync("bash", [join(here, script), ...args], {
      encoding: "utf8",
      env: { PATH: bin + ":" + process.env.PATH, HOME: root, FAKE_LOG: log, ...env },
    });
    return { code: r.status, err: r.stderr, log: readFileSync(log, "utf8") };
  };
  return { here: physical, state: join(here, ".state"), overlay, run };
}

// Under TESTBED_OVERLAY up.sh skips the Go build and the port probe; the fake
// docker fails its first container run, so up.sh stops right after the marker.
function upUntilMarker(f: ReturnType<typeof fixture>, owner?: string) {
  const env: Record<string, string> = { TESTBED_OVERLAY: f.overlay };
  if (owner !== undefined) env.TESTBED_OWNER = owner;
  const r = f.run("up.sh", env);
  expect(r.code).not.toBe(0);
  expect(existsSync(join(f.state, "owner"))).toBe(true);
}

describe("test bed owner marker", () => {
  it("up.sh records TESTBED_OWNER and its owner tears the bed down", () => {
    const f = fixture();
    upUntilMarker(f, "ha-nova/HANO-52");
    expect(readFileSync(join(f.state, "owner"), "utf8")).toBe("ha-nova/HANO-52");
    const r = f.run("down.sh", { TESTBED_OWNER: "ha-nova/HANO-52", FAKE_BED_DIR: f.here });
    expect(r.code, r.err).toBe(0);
    expect(r.log).toMatch(/compose .* down -v/);
    expect(existsSync(f.state)).toBe(false);
  });

  it("refuses another owner in the same checkout", () => {
    const f = fixture();
    upUntilMarker(f, "ha-nova/HANO-49");
    for (const env of [{ TESTBED_OWNER: "ha-nova/HANO-48" }, {}]) {
      const r = f.run("down.sh", { ...env, FAKE_BED_DIR: f.here });
      expect(r.code).toBe(1);
      expect(r.err).toContain("belongs to 'ha-nova/HANO-49'");
      expect(r.log).not.toMatch(/down -v|^run /m);
      expect(existsSync(join(f.state, "owner"))).toBe(true);
    }
  });

  it("refuses a bed started from another checkout, even with nothing here", () => {
    const f = fixture();
    const r = f.run("down.sh", { FAKE_BED_DIR: "/elsewhere/scripts/e2e/testbed" });
    expect(r.code).toBe(1);
    expect(r.err).toContain("started from another checkout (/elsewhere/scripts/e2e/testbed)");
    expect(r.log).not.toMatch(/down -v/);
  });

  it("refuses a bed without an owner marker", () => {
    const f = fixture();
    mkdirSync(join(f.state, "config"), { recursive: true });
    const r = f.run("down.sh", { FAKE_BED_DIR: f.here });
    expect(r.code).toBe(1);
    expect(r.err).toContain("no owner marker");
    expect(existsSync(f.state)).toBe(true);
  });

  it("an unset TESTBED_OWNER matches only an unset one", () => {
    const f = fixture();
    upUntilMarker(f);
    expect(f.run("down.sh", { TESTBED_OWNER: "x", FAKE_BED_DIR: f.here }).code).toBe(1);
    expect(f.run("down.sh", { FAKE_BED_DIR: f.here }).code).toBe(0);
  });

  it("is a no-op when no bed runs and nothing is here", () => {
    const r = fixture().run("down.sh");
    expect(r.code, r.err).toBe(0);
  });

  it("--force tears down a bed that is not the caller's", () => {
    const f = fixture();
    upUntilMarker(f, "ha-nova/HANO-49");
    const r = f.run("down.sh", { FAKE_BED_DIR: "/elsewhere" }, ["--force"]);
    expect(r.code, r.err).toBe(0);
    expect(r.log).toMatch(/down -v/);
    expect(existsSync(f.state)).toBe(false);
  });

  it("up.sh on a running bed names its checkout and leaves no marker", () => {
    const f = fixture();
    const r = f.run("up.sh", { TESTBED_OVERLAY: f.overlay, FAKE_BED_DIR: "/elsewhere/scripts/e2e/testbed" });
    expect(r.code).toBe(1);
    expect(r.err).toContain("already running (started from /elsewhere/scripts/e2e/testbed)");
    expect(existsSync(f.state)).toBe(false);
  });
});
