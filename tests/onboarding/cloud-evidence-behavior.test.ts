import { spawnSync } from "node:child_process";
import {
  chmodSync,
  copyFileSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

const SCRIPT_REL = "scripts/release/build-cloud-evidence.sh";
const GATE_REL = "scripts/release/verify-cloud-release-gate.sh";
const REPO_SLUG = "testowner/testrepo";

function git(cwd: string, ...args: string[]): string {
  const result = spawnSync("git", args, { cwd, encoding: "utf8" });
  expect(result.status, `git ${args.join(" ")}\n${result.stderr}`).toBe(0);
  return result.stdout;
}

interface Fixture {
  repo: string;
  originGit: string;
  mainCommit: string;
  mainTree: string;
}

// The script derives ROOT_DIR from its own location, so a copy inside a temp
// git repo (with a local bare `origin`) runs fully offline: git fetches hit
// the bare repo, gh/ssh/scp hit the fakes on PATH.
function initFixture(platforms: string[]): Fixture {
  const root = mkdtempSync(join(tmpdir(), "ha-nova-cloud-evidence-"));
  const repo = join(root, "repo");
  const originGit = join(root, "origin.git");
  mkdirSync(join(repo, "scripts", "release"), { recursive: true });
  mkdirSync(join(repo, "nova"), { recursive: true });
  copyFileSync(SCRIPT_REL, join(repo, SCRIPT_REL));
  chmodSync(join(repo, SCRIPT_REL), 0o755);
  writeFileSync(join(repo, "nova", "config.yaml"), 'name: HA NOVA\nversion: "0.9.0"\n');
  writeFileSync(
    join(repo, "version.json"),
    `${JSON.stringify({ skill_version: "0.24.0", cloud_remote_platforms: platforms }, null, 2)}\n`,
  );
  git(repo, "init", "-q", "-b", "main");
  git(repo, "config", "user.email", "test@example.invalid");
  git(repo, "config", "user.name", "test");
  git(repo, "add", "-A");
  git(repo, "commit", "-q", "-m", "base");
  git(root, "clone", "--bare", "-q", repo, originGit);
  git(repo, "remote", "add", "origin", originGit);
  const mainCommit = git(repo, "rev-parse", "HEAD").trim();
  const mainTree = git(repo, "rev-parse", "HEAD^{tree}").trim();
  return { repo, originGit, mainCommit, mainTree };
}

interface FakeBin {
  dir: string;
  trace: string;
  state: string;
}

function makeFakeBin(prJson?: object): FakeBin {
  const dir = mkdtempSync(join(tmpdir(), "ha-nova-cloud-evidence-bin-"));
  const state = join(dir, "state");
  mkdirSync(state);
  const trace = join(state, "trace");
  writeFileSync(trace, "");
  if (prJson) {
    writeFileSync(join(state, "pr.json"), JSON.stringify(prJson));
  }
  // Advancing per-location stamps model GitHub's updated_at; FAKE_GH_FROZEN
  // simulates a write that GitHub does not reflect, which the script must
  // treat as a failure. Any un-modelled call exits 91 so new gh usage cannot
  // slip past this suite unnoticed.
  writeFileSync(
    join(dir, "gh"),
    `#!/usr/bin/env bash
set -euo pipefail
args="$*"
trace() { printf '%s\\n' "$1" >>"\${FAKE_GH_STATE}/trace"; }
bump() {
  local f="\$1" n
  n="$(cat "\${FAKE_GH_STATE}/\$f.count" 2>/dev/null || echo 0)"
  n=$((n + 1))
  printf '%s\\n' "\$n" >"\${FAKE_GH_STATE}/\$f.count"
  printf 'stamp-%s\\n' "\$n" >"\${FAKE_GH_STATE}/\$f.stamp"
}
case "$args" in
  "api user --jq .login")
    printf '%s\\n' "\${FAKE_GH_LOGIN}" ;;
  "api repos/${REPO_SLUG}/pulls/"*)
    trace "read:pr"
    cat "\${FAKE_GH_STATE}/pr.json" ;;
  "secret set HA_NOVA_CLOUD_GATE_EVIDENCE_JSON --repo ${REPO_SLUG} --env production --body "*)
    trace "set:env"
    [ "\${FAKE_GH_FROZEN:-0}" = 1 ] || bump env ;;
  "secret set HA_NOVA_CLOUD_GATE_EVIDENCE_JSON --repo ${REPO_SLUG} --body "*)
    trace "set:repo"
    [ "\${FAKE_GH_FROZEN:-0}" = 1 ] || bump repo ;;
  "api repos/${REPO_SLUG}/actions/secrets/HA_NOVA_CLOUD_GATE_EVIDENCE_JSON --jq .updated_at")
    trace "read:repo-stamp"
    cat "\${FAKE_GH_STATE}/repo.stamp" 2>/dev/null ;;
  "api repos/${REPO_SLUG}/environments/production/secrets/HA_NOVA_CLOUD_GATE_EVIDENCE_JSON --jq .updated_at")
    trace "read:env-stamp"
    cat "\${FAKE_GH_STATE}/env.stamp" 2>/dev/null ;;
  *)
    printf 'unexpected gh call: %s\\n' "$args" >&2
    exit 91 ;;
esac
`,
    "utf8",
  );
  chmodSync(join(dir, "gh"), 0o755);
  // ssh/scp must never reach a real host from this suite. Exit 97 marks the
  // spot: a test that legitimately ends at reachability asserts the script's
  // own "unreachable" message, everything else must die earlier.
  for (const tool of ["ssh", "scp"]) {
    writeFileSync(join(dir, tool), `#!/usr/bin/env bash\necho "fake-${tool}: blocked" >&2\nexit 97\n`, "utf8");
    chmodSync(join(dir, tool), 0o755);
  }
  return { dir, trace, state };
}

function runScript(
  fixture: Fixture,
  fake: FakeBin,
  args: string[],
  extraEnv: Record<string, string> = {},
) {
  return spawnSync("bash", [join(fixture.repo, SCRIPT_REL), ...args], {
    cwd: fixture.repo,
    encoding: "utf8",
    env: {
      ...process.env,
      PATH: `${fake.dir}:${process.env.PATH ?? ""}`,
      FAKE_GH_STATE: fake.state,
      FAKE_GH_LOGIN: "testowner",
      HA_NOVA_REPO: REPO_SLUG,
      ...extraEnv,
    },
  });
}

function validEnvelope(commit: string, tree: string, platforms: string[]) {
  const keyrings: Record<string, boolean> = {};
  for (const p of platforms) keyrings[p] = true;
  return {
    schema: 2,
    commit_sha: commit,
    tree_sha: tree,
    relay_app: { version: "0.9.0", source_commit: commit, source_tree_sha: tree },
    checks: {
      parity: true,
      stress_10000: true,
      keyrings,
      roles: true,
      domains_mfa: true,
      lifecycle: true,
      redirects_non_disclosure: true,
      installed_relay_app: true,
      routing: true,
      signing_and_update_matrix: true,
    },
  };
}

function writeEnvelope(fixture: Fixture, envelope: object): string {
  const file = join(fixture.repo, "envelope.json");
  writeFileSync(file, `${JSON.stringify(envelope, null, 2)}\n`);
  return file;
}

const SYNTHETIC_COMMIT = "a".repeat(40);

describe("cloud evidence argument contract", () => {
  const fixture = initFixture(["darwin", "linux", "windows"]);
  const fake = makeFakeBin();

  it("refuses --set without --envelope", () => {
    const result = runScript(fixture, fake, ["7", "--set"]);
    expect(result.status, result.stderr).not.toBe(0);
    expect(result.stderr).toContain("refusing --set without --envelope");
  });

  it("refuses --dry-run combined with --set", () => {
    const result = runScript(fixture, fake, ["7", "--dry-run", "--set"]);
    expect(result.status, result.stderr).not.toBe(0);
    expect(result.stderr).toContain("pick one of --dry-run or --set");
  });

  it("refuses a non-numeric PR", () => {
    const result = runScript(fixture, fake, ["abc"]);
    expect(result.status, result.stderr).not.toBe(0);
    expect(result.stderr).toContain("PR must be a number");
  });

  it("refuses an unknown option", () => {
    const result = runScript(fixture, fake, ["7", "--frobnicate"]);
    expect(result.status, result.stderr).not.toBe(0);
    expect(result.stderr).toContain("unknown option");
  });

  it("refuses a wrong gh login before doing anything", () => {
    const envelopeFile = writeEnvelope(
      fixture,
      validEnvelope(SYNTHETIC_COMMIT, fixture.mainTree, ["darwin", "linux", "windows"]),
    );
    const result = runScript(fixture, fake, ["--repoint", envelopeFile], {
      FAKE_GH_LOGIN: "someone-else",
    });
    expect(result.status, result.stderr).not.toBe(0);
    expect(result.stderr).toContain("gh is authenticated as 'someone-else'");
    expect(readFileSync(fake.trace, "utf8")).not.toContain("set:");
  });
});

describe("cloud evidence --repoint", () => {
  const platforms = ["darwin", "linux", "windows"];

  it("repoints to the main commit and writes BOTH secret locations", () => {
    const fixture = initFixture(platforms);
    const fake = makeFakeBin();
    const envelope = validEnvelope(SYNTHETIC_COMMIT, fixture.mainTree, platforms);
    const envelopeFile = writeEnvelope(fixture, envelope);
    const before = readFileSync(envelopeFile, "utf8");

    const result = runScript(fixture, fake, ["--repoint", envelopeFile]);
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);

    // Both locations, repository first — setting only `production` is the
    // exact failure observed on #559.
    const trace = readFileSync(fake.trace, "utf8");
    expect(trace).toContain("set:repo");
    expect(trace).toContain("set:env");
    expect(trace.indexOf("set:repo")).toBeLessThan(trace.indexOf("set:env"));

    expect(result.stdout).toContain(`"commit_sha": "${fixture.mainCommit}"`);
    expect(result.stdout).not.toContain(`"commit_sha": "${SYNTHETIC_COMMIT}"`);
    // The input file is source material, not state — it must survive untouched.
    expect(readFileSync(envelopeFile, "utf8")).toBe(before);
  });

  it("refuses when origin/main moved past the envelope tree", () => {
    const fixture = initFixture(platforms);
    const fake = makeFakeBin();
    const envelope = validEnvelope(SYNTHETIC_COMMIT, "b".repeat(40), platforms);
    const envelopeFile = writeEnvelope(fixture, envelope);

    const result = runScript(fixture, fake, ["--repoint", envelopeFile]);
    expect(result.status, result.stdout).not.toBe(0);
    expect(result.stderr).toContain("Mint fresh evidence instead of repointing");
    expect(readFileSync(fake.trace, "utf8")).not.toContain("set:");
  });

  it.each([
    [
      "a check that is not literally true",
      (envelope: any) => {
        envelope.checks.installed_relay_app = false;
      },
      "literally true",
    ],
    [
      "a missing required check",
      (envelope: any) => {
        delete envelope.checks.parity;
      },
      "exactly the gate's required checks",
    ],
    [
      "an extra top-level field",
      (envelope: any) => {
        envelope.carry_reason = "docs-only";
      },
      "exactly schema, commit_sha, tree_sha, checks, relay_app",
    ],
    [
      "keyrings not matching the enabled platforms",
      (envelope: any) => {
        envelope.checks.keyrings = { linux: true };
      },
      "keyrings keys must exactly match",
    ],
    [
      "a string schema",
      (envelope: any) => {
        envelope.schema = "2";
      },
      "the number, not a string",
    ],
    [
      "relay_app not repeating the envelope identity",
      (envelope: any) => {
        envelope.relay_app.source_commit = "c".repeat(40);
      },
      "relay_app.source_commit must equal commit_sha",
    ],
  ])("refuses %s and never writes", (_label, mutate, message) => {
    const fixture = initFixture(platforms);
    const fake = makeFakeBin();
    const envelope = validEnvelope(SYNTHETIC_COMMIT, fixture.mainTree, platforms) as any;
    mutate(envelope);
    const envelopeFile = writeEnvelope(fixture, envelope);

    const result = runScript(fixture, fake, ["--repoint", envelopeFile]);
    expect(result.status, result.stdout).not.toBe(0);
    expect(result.stderr).toContain(message);
    expect(readFileSync(fake.trace, "utf8")).not.toContain("set:");
  });

  it("fails when a secret write is not reflected in updated_at", () => {
    const fixture = initFixture(platforms);
    const fake = makeFakeBin();
    // Pre-seed both stamps so the failure is "did not advance", not "absent".
    writeFileSync(join(fake.state, "repo.stamp"), "stamp-0\n");
    writeFileSync(join(fake.state, "env.stamp"), "stamp-0\n");
    const envelopeFile = writeEnvelope(
      fixture,
      validEnvelope(SYNTHETIC_COMMIT, fixture.mainTree, platforms),
    );

    const result = runScript(fixture, fake, ["--repoint", envelopeFile], {
      FAKE_GH_FROZEN: "1",
    });
    expect(result.status, result.stdout).not.toBe(0);
    expect(result.stderr).toContain("did not advance");
  });
});

describe("cloud evidence PR target binding", () => {
  // linux-only platforms keep this deterministic on any test host: the darwin
  // branch would depend on the host OS, and the fake ssh (exit 97) guarantees
  // nothing past reachability ever runs.
  const platforms = ["linux", "windows"];

  function prFixture() {
    const fixture = initFixture(platforms);
    const fake = makeFakeBin({
      state: "open",
      mergeable_state: "clean",
      merge_commit_sha: fixture.mainCommit,
    });
    // GitHub's synthetic merge ref, served by the local bare origin.
    git(fixture.originGit, "update-ref", "refs/pull/7/merge", fixture.mainCommit);
    return { fixture, fake };
  }

  it("refuses an envelope whose commit_sha is not the resolved target commit", () => {
    const { fixture, fake } = prFixture();
    const envelope = validEnvelope("c".repeat(40), fixture.mainTree, platforms);
    const envelopeFile = writeEnvelope(fixture, envelope);

    const result = runScript(fixture, fake, ["7", "--set", "--envelope", envelopeFile]);
    expect(result.status, result.stdout).not.toBe(0);
    expect(result.stderr).toContain("does not match the resolved target commit");
    expect(readFileSync(fake.trace, "utf8")).not.toContain("set:");
  });

  it("validates the envelope before spending anything on reachability or dispatch", () => {
    const { fixture, fake } = prFixture();
    const envelope = validEnvelope(fixture.mainCommit, fixture.mainTree, platforms);
    const envelopeFile = writeEnvelope(fixture, envelope);

    const result = runScript(fixture, fake, ["7", "--set", "--envelope", envelopeFile]);
    // Validation passed (the message printed), then the fake ssh stopped the
    // run at reachability — no dispatch, no secret writes.
    expect(result.status).not.toBe(0);
    expect(result.stdout).toContain("envelope matches the target and the gate contract");
    expect(result.stderr).toContain("unreachable");
    const trace = readFileSync(fake.trace, "utf8");
    expect(trace).toContain("read:pr");
    expect(trace).not.toContain("set:");
  });

  it("refuses a closed PR and points at --repoint", () => {
    const fixture = initFixture(platforms);
    const fake = makeFakeBin({
      state: "closed",
      mergeable_state: "unknown",
      merge_commit_sha: fixture.mainCommit,
    });
    const result = runScript(fixture, fake, ["7"]);
    expect(result.status, result.stdout).not.toBe(0);
    expect(result.stderr).toContain("use --repoint instead");
  });
});

describe("cloud evidence contract parity with the gate", () => {
  it("pins the script's required-check list to verify-cloud-release-gate.sh", () => {
    const gate = readFileSync(GATE_REL, "utf8");
    const gateBody = gate.match(/const requiredChecks = \[([\s\S]*?)\];/)?.[1] ?? "";
    expect(gateBody, "requiredChecks array not found in the gate").not.toBe("");
    const gateChecks = [...gateBody.matchAll(/"([a-z0-9_]+)"/g)].map((m) => m[1] ?? "");
    expect(gateChecks.length).toBeGreaterThan(0);

    const script = readFileSync(SCRIPT_REL, "utf8");
    const scriptList = script.match(/^REQUIRED_CHECKS_SORTED="([a-z0-9_,]+)"$/m)?.[1] ?? "";
    expect(scriptList, "REQUIRED_CHECKS_SORTED not found in the script").not.toBe("");
    const scriptChecks = scriptList.split(",");

    expect(scriptChecks).toEqual([...gateChecks, "keyrings"].sort());
  });
});
