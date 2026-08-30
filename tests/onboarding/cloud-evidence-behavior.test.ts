import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
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
const SOURCED_LIBS_REL = [
  "scripts/release/cloud-evidence-envelope.sh",
  "scripts/release/cloud-evidence-provenance.sh",
];
const ENVELOPE_LIB_REL = SOURCED_LIBS_REL[0] ?? "";
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
  for (const lib of SOURCED_LIBS_REL) {
    copyFileSync(lib, join(repo, lib));
  }
  writeFileSync(join(repo, "nova", "config.yaml"), 'name: HA NOVA\nversion: "0.9.0"\n');
  // The dispatch preflight reads the same policy file as the server resolver.
  mkdirSync(join(repo, ".github", "policy"), { recursive: true });
  writeFileSync(
    join(repo, ".github", "policy", "repo-policy.json"),
    `${JSON.stringify({
      main_branch_protection: {
        required_status_checks: ["ci-gate", "cloud-source-gate"],
        advisory_checks: [],
      },
    })}\n`,
  );
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
last_arg() { local a v=""; for a in "$@"; do v="$a"; done; printf '%s' "$v"; }
case "$args" in
  "api user --jq .login")
    printf '%s\\n' "\${FAKE_GH_LOGIN}" ;;
  "api repos/${REPO_SLUG}/pulls/"*)
    trace "read:pr"
    cat "\${FAKE_GH_STATE}/pr.json" ;;
  "api --paginate --slurp repos/${REPO_SLUG}/actions/runs?head_sha="*"&event=pull_request&per_page=100")
    trace "read:ci-workflow"
    if [ -f "\${FAKE_GH_STATE}/ci-run-pages.json" ]; then cat "\${FAKE_GH_STATE}/ci-run-pages.json"; else
      msha="$(cat "\${FAKE_GH_STATE}/main.sha" 2>/dev/null || echo unknown)"
      printf '[{"workflow_runs":[{"id":9,"path":".github/workflows/ci.yml","check_suite_id":777,"status":"completed","conclusion":"success","pull_requests":[{"number":7,"base":{"sha":"%s"}}]}]}]\\n' "\$msha"
    fi ;;
  "api --paginate --slurp repos/${REPO_SLUG}/commits/"*"/check-runs?per_page=100")
    trace "read:head-checks"
    cat "\${FAKE_GH_STATE}/head-checks.json" 2>/dev/null \\
      || printf '[{"check_runs":[{"name":"ci-gate","id":1,"check_suite":{"id":777},"app":{"slug":"github-actions"},"pull_requests":[{"number":7}],"status":"completed","conclusion":"success"}]}]\\n' ;;
  "api --paginate --slurp repos/${REPO_SLUG}/commits/"*"/status?per_page=100")
    trace "read:commit-status"
    cat "\${FAKE_GH_STATE}/status-shadow.state" 2>/dev/null || printf '[]\\n' ;;
  "api graphql --paginate --slurp -f owner="*" -f name="*" -F number="*" -f query="*)
    trace "read:review-threads"
    cat "\${FAKE_GH_STATE}/review.json" 2>/dev/null \\
      || printf '[{"data":{"repository":{"pullRequest":{"state":"OPEN","isDraft":false,"reviewDecision":null,"reviewThreads":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}]\\n' ;;
  "run list --repo ${REPO_SLUG} --workflow cloud-candidate-bundle.yml --json databaseId,displayTitle,headSha,status,conclusion,event,attempt --limit 30")
    trace "run-list"
    n="$(cat "\${FAKE_GH_STATE}/list.count" 2>/dev/null || echo 0)"
    n=$((n + 1))
    printf '%s\\n' "\$n" >"\${FAKE_GH_STATE}/list.count"
    f="\${FAKE_GH_STATE}/runs-\$n.json"
    while [ ! -f "\$f" ] && [ "\$n" -gt 1 ]; do n=$((n - 1)); f="\${FAKE_GH_STATE}/runs-\$n.json"; done
    cat "\$f" 2>/dev/null || printf '[]\\n' ;;
  "workflow run cloud-candidate-bundle.yml --repo ${REPO_SLUG} -f pull_request=7 -f version_tag="*" -f request_id="*)
    trace "dispatch"
    exit 0 ;;
  "run watch "*" --repo ${REPO_SLUG} --exit-status")
    trace "run-watch:$3"
    exit 0 ;;
  "run download "*" --repo ${REPO_SLUG} --name cloud-candidate-install-bundles --dir "*)
    trace "run-download:$3"
    dest="$(last_arg "$@")"
    mkdir -p "$dest"
    cp "\${FAKE_GH_STATE}/bundles/"* "$dest"/ ;;
  "secret set HA_NOVA_CLOUD_GATE_EVIDENCE_JSON --repo ${REPO_SLUG} --env production --body "*)
    trace "set:env"
    last_arg "$@" >"\${FAKE_GH_STATE}/env.body"
    [ "\${FAKE_GH_FROZEN:-0}" = 1 ] || bump env ;;
  "secret set HA_NOVA_CLOUD_GATE_EVIDENCE_JSON --repo ${REPO_SLUG} --body "*)
    trace "set:repo"
    last_arg "$@" >"\${FAKE_GH_STATE}/repo.body"
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
  // own "unreachable" message, everything else must die earlier. FAKE_SSH_OK
  // lets reachability pass (exit 0, no output) so later stages are testable;
  // the empty output then stops the run at the linux architecture probe.
  writeFileSync(
    join(dir, "ssh"),
    `#!/usr/bin/env bash\nif [ "\${FAKE_SSH_OK:-0}" = 1 ]; then exit 0; fi\necho "fake-ssh: blocked" >&2\nexit 97\n`,
    "utf8",
  );
  chmodSync(join(dir, "ssh"), 0o755);
  writeFileSync(join(dir, "scp"), `#!/usr/bin/env bash\necho "fake-scp: blocked" >&2\nexit 97\n`, "utf8");
  chmodSync(join(dir, "scp"), 0o755);
  return { dir, trace, state };
}

// A real tar.gz with the real layout (ha-nova/bundle.json), so the script's
// pre-execution identity extraction works against honest bytes.
function seedBundle(
  fake: FakeBin,
  name: string,
  opts: { tree: string; version?: string; checksum?: boolean },
): void {
  const dir = join(fake.state, "bundles");
  mkdirSync(dir, { recursive: true });
  const buildRoot = mkdtempSync(join(tmpdir(), "ha-nova-bundle-build-"));
  mkdirSync(join(buildRoot, "ha-nova"), { recursive: true });
  writeFileSync(
    join(buildRoot, "ha-nova", "bundle.json"),
    JSON.stringify({
      version: opts.version ?? "0.24.0-rc1",
      cloud_release: { source_tree_sha: opts.tree },
    }),
  );
  const tarResult = spawnSync("tar", ["-czf", join(dir, name), "-C", buildRoot, "ha-nova"], {
    encoding: "utf8",
  });
  expect(tarResult.status, tarResult.stderr).toBe(0);
  if (opts.checksum !== false) {
    const content = readFileSync(join(dir, name));
    const hash = createHash("sha256").update(content).digest("hex");
    writeFileSync(join(dir, `${name}.sha256`), `${hash}  ${name}\n`);
  }
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
      // Per-test TMPDIR: the secret-write lock lives under TMPDIR, and a
      // deliberately-failing write keeps it — a shared /tmp would leak that
      // stale lock into the next suite run.
      TMPDIR: fake.state,
      HA_NOVA_POLL_SECONDS: "0",
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

    // What was WRITTEN, not just that something was written: both locations
    // get the identical, repointed, all-true envelope.
    const writtenRepo = readFileSync(join(fake.state, "repo.body"), "utf8");
    const writtenEnv = readFileSync(join(fake.state, "env.body"), "utf8");
    expect(writtenEnv).toBe(writtenRepo);
    const written = JSON.parse(writtenEnv);
    expect(written.commit_sha).toBe(fixture.mainCommit);
    expect(written.relay_app.source_commit).toBe(fixture.mainCommit);
    expect(written.tree_sha).toBe(fixture.mainTree);
    expect(written.schema).toBe(2);
    expect(written.checks.installed_relay_app).toBe(true);

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
  // branch would depend on the host OS, the windows branch would need a zip
  // toolchain, and the fake ssh guarantees nothing real is ever reached.
  const platforms = ["linux"];

  function prJson(fixture: Fixture, overrides: Record<string, unknown> = {}) {
    return {
      state: "open",
      draft: false,
      mergeable_state: "clean",
      merge_commit_sha: fixture.mainCommit,
      base: { ref: "main", sha: fixture.mainCommit },
      head: { sha: fixture.mainCommit, repo: { full_name: REPO_SLUG } },
      ...overrides,
    };
  }

  function prFixture() {
    const fixture = initFixture(platforms);
    const fake = makeFakeBin(prJson(fixture));
    writeFileSync(join(fake.state, "main.sha"), `${fixture.mainCommit}\n`);
    // GitHub's synthetic merge ref, served by the local bare origin.
    git(fixture.originGit, "update-ref", "refs/pull/7/merge", fixture.mainCommit);
    return { fixture, fake };
  }

  const RUN = (
    fixture: Fixture,
    databaseId: number,
    status: string,
    conclusion: string | null,
    identity: {
      pr?: number;
      base?: string;
      head?: string;
      merge?: string;
      attempt?: number;
    } = {},
  ) => {
    const pr = identity.pr ?? 7;
    const base = identity.base ?? fixture.mainCommit;
    const head = identity.head ?? fixture.mainCommit;
    const merge = identity.merge ?? fixture.mainCommit;
    const requestId = `pr${pr}-${base}-${head}-${merge}`;
    return {
      databaseId,
      displayTitle: `Cloud candidate PR #${pr} v0.24.0-rc1 (${requestId})`,
      headSha: base,
      status,
      conclusion,
      event: "workflow_dispatch",
      attempt: identity.attempt ?? 1,
    };
  };

  it("refuses a PR that does not target main", () => {
    const fixture = initFixture(platforms);
    const fake = makeFakeBin(prJson(fixture, { base: { ref: "release-train" } }));
    const result = runScript(fixture, fake, ["7"]);
    expect(result.status, result.stdout).not.toBe(0);
    expect(result.stderr).toContain("not main");
  });

  it("refuses a fork-headed PR", () => {
    const fixture = initFixture(platforms);
    const fake = makeFakeBin(prJson(fixture, { head: { sha: fixture.mainCommit, repo: { full_name: "someone/fork" } } }));
    const result = runScript(fixture, fake, ["7"]);
    expect(result.status, result.stdout).not.toBe(0);
    expect(result.stderr).toContain("same-repo branches");
  });

  it("refuses unresolved review threads before spending a dispatch", () => {
    // #609/rc22: a stale bot thread from a pre-session review burned the
    // dispatch — server-side the resolver rejects only AFTER the run started.
    const { fixture, fake } = prFixture();
    // Slurp shape: the unresolved thread sits on the SECOND page, so a
    // first-page-only preflight would pass and burn the dispatch.
    writeFileSync(
      join(fake.state, "review.json"),
      JSON.stringify([
        {
          data: {
            repository: {
              pullRequest: {
                state: "OPEN",
                isDraft: false,
                reviewDecision: null,
                reviewThreads: {
                  nodes: [{ isResolved: true }],
                  pageInfo: { hasNextPage: true, endCursor: "c1" },
                },
              },
            },
          },
        },
        {
          data: {
            repository: {
              pullRequest: {
                state: "OPEN",
                isDraft: false,
                reviewDecision: null,
                reviewThreads: {
                  nodes: [{ isResolved: false }],
                  pageInfo: { hasNextPage: false, endCursor: null },
                },
              },
            },
          },
        },
      ]),
    );
    const result = runScript(fixture, fake, ["7"]);
    expect(result.status, result.stdout).not.toBe(0);
    expect(result.stderr).toContain("unresolved threads");
    expect(readFileSync(fake.trace, "utf8")).not.toContain("dispatch");
  });

  it("refuses a requested-changes review before spending a dispatch", () => {
    const { fixture, fake } = prFixture();
    writeFileSync(
      join(fake.state, "review.json"),
      JSON.stringify([
        {
          data: {
            repository: {
              pullRequest: {
                state: "OPEN",
                isDraft: false,
                reviewDecision: "CHANGES_REQUESTED",
                reviewThreads: {
                  nodes: [],
                  pageInfo: { hasNextPage: false, endCursor: null },
                },
              },
            },
          },
        },
      ]),
    );
    const result = runScript(fixture, fake, ["7"]);
    expect(result.status, result.stdout).not.toBe(0);
    expect(result.stderr).toContain("requested-changes");
    expect(readFileSync(fake.trace, "utf8")).not.toContain("dispatch");
  });

  it("refuses to dispatch while ci-gate has not succeeded on the head", () => {
    // #611/rc23: dispatched seconds after a push, CI unfinished — the server
    // resolver rejected AFTER the run started.
    const { fixture, fake } = prFixture();
    writeFileSync(
      join(fake.state, "ci-run-pages.json"),
      JSON.stringify([
        {
          workflow_runs: [
            {
              id: 9,
              path: ".github/workflows/ci.yml",
              check_suite_id: 777,
              status: "in_progress",
              conclusion: null,
              pull_requests: [{ number: 7, base: { sha: fixture.mainCommit } }],
            },
          ],
        },
      ]),
    );
    const result = runScript(fixture, fake, ["7"]);
    expect(result.status, result.stdout).not.toBe(0);
    expect(result.stderr).toContain("CI workflow run");
    expect(readFileSync(fake.trace, "utf8")).not.toContain("dispatch");
  });

  it("refuses when a policy-required check is missing from the head's suite", () => {
    const { fixture, fake } = prFixture();
    writeFileSync(join(fake.state, "head-checks.json"), "[]\n");
    const result = runScript(fixture, fake, ["7"]);
    expect(result.status, result.stdout).not.toBe(0);
    expect(result.stderr).toContain("required pre-evidence check 'ci-gate' is 'absent'");
    expect(readFileSync(fake.trace, "utf8")).not.toContain("dispatch");
  });

  it("refuses an envelope whose commit_sha is not the resolved target commit", () => {
    const { fixture, fake } = prFixture();
    const envelope = validEnvelope("c".repeat(40), fixture.mainTree, platforms);
    const envelopeFile = writeEnvelope(fixture, envelope);

    const result = runScript(fixture, fake, ["7", "--set", "--envelope", envelopeFile]);
    expect(result.status, result.stdout).not.toBe(0);
    expect(result.stderr).toContain("does not match the resolved target commit");
    expect(readFileSync(fake.trace, "utf8")).not.toContain("set:");
  });

  it("an unreachable lab host stays fail-closed without the explicit fallback opt-in", () => {
    const { fixture, fake } = prFixture();
    const envelope = validEnvelope(fixture.mainCommit, fixture.mainTree, platforms);
    const envelopeFile = writeEnvelope(fixture, envelope);

    const result = runScript(fixture, fake, ["7", "--set", "--envelope", envelopeFile]);
    expect(result.status).not.toBe(0);
    expect(result.stdout).toContain("envelope matches the target and the gate contract");
    expect(result.stderr).toContain("HA_NOVA_EVIDENCE_RUNNER_FALLBACK=1");
    const trace = readFileSync(fake.trace, "utf8");
    expect(trace).not.toContain("dispatch");
    expect(trace).not.toContain("set:");
  });

  it("--set reuses only an exact request and still requires valid signed evidence", () => {
    const { fixture, fake } = prFixture();
    const envelope = validEnvelope(fixture.mainCommit, fixture.mainTree, platforms);
    const envelopeFile = writeEnvelope(fixture, envelope);
    writeFileSync(
      join(fake.state, "runs-1.json"),
      JSON.stringify([RUN(fixture, 42, "completed", "success")]),
    );
    seedBundle(fake, "ha-nova-installer-bundle-linux-amd64.tar.gz", { tree: fixture.mainTree });

    const result = runScript(fixture, fake, ["7", "--set", "--envelope", envelopeFile], {
      HA_NOVA_EVIDENCE_RUNNER_FALLBACK: "1",
    });
    // Reachability records the fallback, the bundle reuses the seeded run,
    // and the POSITIVE static verification then rejects the unsigned bundle
    // before any secret is written.
    expect(result.status).not.toBe(0);
    expect(result.stdout).toContain(
      "static signed-evidence verification + the workflow's native runner smoke stand in",
    );
    expect(result.stderr).toContain("static signed-evidence verification failed");
    const trace = readFileSync(fake.trace, "utf8");
    expect(trace).toContain("run-download:42");
    expect(trace).not.toContain("dispatch");
    expect(trace).not.toContain("set:");
  });

  it("ignores a successful foreign PR with the same tree and dispatches the exact request", () => {
    const { fixture, fake } = prFixture();
    const foreign = RUN(fixture, 41, "completed", "success", {
      pr: 8,
      merge: "b".repeat(40),
    });
    writeFileSync(join(fake.state, "runs-1.json"), JSON.stringify([foreign]));
    writeFileSync(
      join(fake.state, "runs-2.json"),
      JSON.stringify([RUN(fixture, 99, "completed", "success"), foreign]),
    );
    // The foreign artifact has the exact same tree. Tree-only selection would
    // reuse it even though its PR and merge identity are different.
    seedBundle(fake, "ha-nova-installer-bundle-linux-amd64.tar.gz", {
      tree: fixture.mainTree,
    });

    const result = runScript(fixture, fake, ["7"], { FAKE_SSH_OK: "1" });
    const trace = readFileSync(fake.trace, "utf8");
    expect(trace).toContain("dispatch");
    expect(trace).toContain("run-download:99");
    expect(trace).not.toContain("run-download:41");
    expect(result.stdout).toContain("every bundle matches tree");
  });

  it("does not watch a foreign in-flight run", () => {
    const { fixture, fake } = prFixture();
    const foreign = RUN(fixture, 43, "in_progress", null, {
      pr: 8,
      merge: "c".repeat(40),
    });
    writeFileSync(join(fake.state, "runs-1.json"), JSON.stringify([foreign]));
    writeFileSync(
      join(fake.state, "runs-2.json"),
      JSON.stringify([RUN(fixture, 99, "completed", "success"), foreign]),
    );
    seedBundle(fake, "ha-nova-installer-bundle-linux-amd64.tar.gz", {
      tree: fixture.mainTree,
    });

    runScript(fixture, fake, ["7"], { FAKE_SSH_OK: "1" });
    const trace = readFileSync(fake.trace, "utf8");
    expect(trace).toContain("dispatch");
    expect(trace).not.toContain("run-watch:43");
    expect(trace).not.toContain("run-download:43");
  });

  it("does not reuse an exact title from a rerun or foreign workflow head", () => {
    const { fixture, fake } = prFixture();
    const rerun = RUN(fixture, 44, "completed", "success", { attempt: 2 });
    const foreignHead = {
      ...RUN(fixture, 45, "completed", "success"),
      headSha: "d".repeat(40),
    };
    writeFileSync(
      join(fake.state, "runs-1.json"),
      JSON.stringify([foreignHead, rerun]),
    );
    writeFileSync(
      join(fake.state, "runs-2.json"),
      JSON.stringify([
        RUN(fixture, 99, "completed", "success"),
        foreignHead,
        rerun,
      ]),
    );
    seedBundle(fake, "ha-nova-installer-bundle-linux-amd64.tar.gz", {
      tree: fixture.mainTree,
    });

    runScript(fixture, fake, ["7"], { FAKE_SSH_OK: "1" });
    const trace = readFileSync(fake.trace, "utf8");
    expect(trace).toContain("dispatch");
    expect(trace).not.toContain("run-download:44");
    expect(trace).not.toContain("run-download:45");
  });

  it("watches an in-flight run first, then reuses it once its artifact proves the target", () => {
    const { fixture, fake } = prFixture();
    writeFileSync(join(fake.state, "runs-1.json"), JSON.stringify([RUN(fixture, 42, "in_progress", null)]));
    writeFileSync(join(fake.state, "runs-2.json"), JSON.stringify([RUN(fixture, 42, "completed", "success")]));
    seedBundle(fake, "ha-nova-installer-bundle-linux-amd64.tar.gz", { tree: fixture.mainTree });

    const result = runScript(fixture, fake, ["7"], { FAKE_SSH_OK: "1" });
    expect(result.stdout).toContain("matching run 42 is in flight — watching it first");
    const trace = readFileSync(fake.trace, "utf8");
    expect(trace).toContain("run-watch");
    expect(trace).toContain("run-download");
    expect(trace).not.toContain("dispatch");
    expect(result.stderr).not.toContain("unexpected gh call");
    // Checksums and the pre-execution identity check pass; the run then stops
    // deterministically at the linux architecture probe (the FAKE_SSH_OK ssh
    // answers with empty output).
    expect(result.stdout).toContain("reusing run 42");
    expect(result.stdout).toContain("every bundle matches tree");
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("unsupported architecture");
  });

  it("treats an identity mismatch as 'not ours', dispatches fresh, and hard-stops on a persistent mismatch", () => {
    const { fixture, fake } = prFixture();
    writeFileSync(join(fake.state, "runs-1.json"), JSON.stringify([RUN(fixture, 44, "completed", "success")]));
    // After the dispatch, a new run appears above the previous max id.
    writeFileSync(
      join(fake.state, "runs-2.json"),
      JSON.stringify([RUN(fixture, 99, "completed", "success"), RUN(fixture, 44, "completed", "success")]),
    );
    // Valid checksums, wrong tree — the fake serves the same wrong artifact
    // for the dispatched run too, so the hard stop must fire before any
    // copy/execute step (which would surface as the blocked fake scp).
    seedBundle(fake, "ha-nova-installer-bundle-linux-amd64.tar.gz", { tree: "d".repeat(40) });

    const result = runScript(fixture, fake, ["7"], { FAKE_SSH_OK: "1" });
    expect(result.status, result.stdout).not.toBe(0);
    expect(result.stdout).toContain("identity does not match this target");
    expect(result.stdout).toContain("dispatching fresh");
    const trace = readFileSync(fake.trace, "utf8");
    expect(trace).toContain("dispatch");
    expect(result.stderr).toContain("refusing to execute it");
    expect(result.stderr).not.toContain("cannot copy");
  });

  it("falls back to a fresh dispatch when the reuse artifact is gone, and fails loudly if that one is gone too", () => {
    const { fixture, fake } = prFixture();
    writeFileSync(join(fake.state, "runs-1.json"), JSON.stringify([RUN(fixture, 44, "completed", "success")]));
    writeFileSync(
      join(fake.state, "runs-2.json"),
      JSON.stringify([RUN(fixture, 99, "completed", "success"), RUN(fixture, 44, "completed", "success")]),
    );
    // No bundles seeded at all: every download fails like an expired artifact.
    const result = runScript(fixture, fake, ["7"], { FAKE_SSH_OK: "1" });
    expect(result.status, result.stdout).not.toBe(0);
    expect(result.stdout).toContain("dispatching fresh");
    expect(result.stderr).toContain("cannot download the install bundles from run 99");
  });

  it("refuses a bundle without its .sha256 instead of running it unverified", () => {
    const { fixture, fake } = prFixture();
    writeFileSync(join(fake.state, "runs-1.json"), JSON.stringify([RUN(fixture, 44, "completed", "success")]));
    seedBundle(fake, "ha-nova-installer-bundle-linux-amd64.tar.gz", {
      tree: fixture.mainTree,
      checksum: false,
    });

    const result = runScript(fixture, fake, ["7"], { FAKE_SSH_OK: "1" });
    expect(result.status, result.stdout).not.toBe(0);
    expect(result.stderr).toContain("has no .sha256");
  });

  it("refuses a draft PR before any dispatch", () => {
    const fixture = initFixture(platforms);
    const fake = makeFakeBin(prJson(fixture, { draft: true, mergeable_state: "blocked" }));
    const result = runScript(fixture, fake, ["7"]);
    expect(result.status, result.stdout).not.toBe(0);
    expect(result.stderr).toContain("is a draft");
  });

  it("refuses a closed PR and points at --repoint", () => {
    const fixture = initFixture(platforms);
    const fake = makeFakeBin(prJson(fixture, { state: "closed", mergeable_state: "unknown" }));
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
    const gateChecks = [...gateBody.matchAll(/"([^"]+)"/g)].map((m) => m[1] ?? "");
    expect(gateChecks.length).toBeGreaterThan(0);

    const script = readFileSync(ENVELOPE_LIB_REL, "utf8");
    const scriptList = script.match(/^REQUIRED_CHECKS_SORTED="([a-z0-9_,]+)"$/m)?.[1] ?? "";
    expect(scriptList, "REQUIRED_CHECKS_SORTED not found in the envelope lib").not.toBe("");
    const scriptChecks = scriptList.split(",");

    expect(scriptChecks).toEqual([...gateChecks, "keyrings"].sort());
  });
});
