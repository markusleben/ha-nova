import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { spawnSync } from "node:child_process";

import { describe, expect, it } from "vitest";
import { parse } from "yaml";

// Walk the tree instead of fs.globSync to stay independent of the Node minor.
function collectTestFiles(dir: string): string[] {
  const found: string[] = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) {
      found.push(...collectTestFiles(path));
    } else if (/\.(test|spec)\.[cm]?[jt]sx?$/.test(entry)) {
      // vitest's default include covers every js/ts extension family, so the
      // walk must too: .test.tsx, .spec.jsx, .test.cts and .spec.cjs would
      // otherwise slip past the orphan check and preserve the silent-coverage
      // gap this guard exists to close (#515).
      found.push(path.split("\\").join("/"));
    }
  }
  return found;
}

// Files that import vitest but do not carry a test suffix: this repo's
// `*-behavior.ts` convention. They only run when a wrapper imports them.
function collectModules(dir: string): string[] {
  const found: string[] = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) {
      found.push(...collectModules(path));
    } else if (/\.[cm]?[jt]sx?$/.test(entry)) {
      found.push(path.split("\\").join("/"));
    }
  }
  return found;
}

const importsVitest = (file: string): boolean =>
  /from ["']vitest["']/.test(readFileSync(file, "utf8"));

// Traversal must pass THROUGH plain helpers: an aggregator that imports no
// vitest itself still carries the edge from a running entrypoint to a
// behavior module, and stopping there reports live suites as unreachable.
function collectVitestModules(dir: string): string[] {
  return collectModules(dir).filter(importsVitest);
}

function stripComments(source: string): string {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .split("\n")
    .map((line) => {
      let quote: string | null = null;
      for (let i = 0; i < line.length; i += 1) {
        const ch = line[i];
        if (quote) {
          if (ch === "\\") i += 1;
          else if (ch === quote) quote = null;
        } else if (ch === '"' || ch === "'" || ch === "`") {
          quote = ch;
        } else if (ch === "/" && line[i + 1] === "/") {
          return line.slice(0, i);
        }
      }
      return line;
    })
    .join("\n");
}

function expectFragmentsInOrder(haystack: string, fragments: string[]) {
  let cursor = 0;
  for (const fragment of fragments) {
    const index = haystack.indexOf(fragment, cursor);
    expect(index).toBeGreaterThanOrEqual(0);
    cursor = index + fragment.length;
  }
}

describe("safe test system contract", () => {
  // npm test and verify:node run one plain `vitest run`, so a test runs exactly
  // when Vitest's include matches it. A suite can still silently never run in
  // two ways: a test file whose suffix the include misses, and a
  // `*-behavior.ts` module (no test suffix) that no running file imports, or
  // whose register*Tests() nothing calls (#515).
  it("runs every test file and behavior module in the Vitest sweep", () => {
    expect(readFileSync("vitest.config.ts", "utf8")).toContain('include: ["tests/**/*.test.ts"]');
    const isEntrypoint = (file: string) => /^tests\/.+\.test\.ts$/.test(file);
    const testFiles = collectTestFiles("tests");
    expect(testFiles.length).toBeGreaterThan(50);
    expect(collectTestFiles("nova/src"), "test files belong under tests/, which is all Vitest runs").toEqual([]);
    const missed = testFiles.filter((file) => !isEntrypoint(file));
    expect(
      missed,
      `vitest only includes tests/**/*.test.ts, so these never run — rename them:\n  ${missed.join("\n  ")}`
    ).toEqual([]);

    const vitestModules = collectVitestModules("tests");
    const allModules = collectModules("tests");
    const behaviorModules = vitestModules.filter((file) => !isEntrypoint(file));

    // Resolve real module specifiers, not basename occurrences: a name left
    // in a comment must not count as an import, and a cycle of behavior
    // modules importing each other must not count as reachable.
    const importsOf = (file: string): string[] => {
      const dir = file.split("/").slice(0, -1).join("/");
      const body = readFileSync(file, "utf8");
      return [...body.matchAll(/(?:from|import)\s+["'](\.[^"']+)["']/g)]
        // Normalize the SAME suffix set on both sides: an import written as
        // "./suite.mjs" must match a candidate stripped of .mjs.
        .map((m) => (m[1] as string).replace(/\.[cm]?[jt]sx?$/, ""))
        .map((spec) => {
          const base = join(dir, spec).split("\\").join("/");
          return allModules.find(
            (candidate) => candidate.replace(/\.[cm]?[jt]sx?$/, "") === base,
          );
        })
        .filter((hit): hit is string => Boolean(hit));
    };

    const reachableModules = new Set<string>();
    const walk = (file: string): void => {
      for (const dep of importsOf(file)) {
        if (reachableModules.has(dep)) continue;
        reachableModules.add(dep);
        walk(dep);
      }
    };
    // Only entrypoints Vitest runs seed the traversal, so a cycle among
    // behavior modules cannot make itself reachable. A wrapper whose body is
    // nothing but side-effect imports never imports vitest itself, so seed
    // from every entrypoint.
    for (const entry of allModules.filter(isEntrypoint)) walk(entry);
    // Reaching a module is not running it: these export a register*Tests()
    // function the wrapper must CALL. Search only the code that RUNS, with
    // comments removed: a commented-out `// registerFooTests()` is exactly the
    // deletion this guard exists to catch.
    const registrarsOf = (file: string): string[] =>
      [...stripComments(readFileSync(file, "utf8")).matchAll(/export\s+(?:async\s+)?function\s+(register\w+)/g)]
        .map((m) => m[1] as string);
    const runningSources = allModules
      .filter((file) => reachableModules.has(file) || isEntrypoint(file))
      .map((file) => stripComments(readFileSync(file, "utf8")))
      .join("\n");
    const uncalled = behaviorModules.filter((file) =>
      registrarsOf(file).some(
        // A registrar invocation is a STATEMENT, so it begins its line; the
        // same name inside an assertion message never does.
        (name) => !new RegExp(`^\\s*(await\\s+)?${name}\\s*\\(`, "m").test(
          runningSources.replace(new RegExp(`export\\s+(?:async\\s+)?function\\s+${name}`, "g"), ""),
        ),
      ),
    );
    expect(
      uncalled,
      `these behavior modules export a registrar that nothing calls, so their suites never register:\n  ${uncalled.join("\n  ")}`
    ).toEqual([]);

    const unreachable = behaviorModules.filter((file) => !reachableModules.has(file));
    expect(
      unreachable,
      `these vitest modules are imported by nothing and therefore never run:\n  ${unreachable.join("\n  ")}`
    ).toEqual([]);
  });

  // The Go suite runs in the go-test shards; a Vitest file that spawns
  // the Go suite repeats it and compiles the CLI on every local npm test.
  it("never re-runs Go tests from Vitest", () => {
    const spawnsGoTest = collectModules("tests").filter((file) => {
      const source = readFileSync(file, "utf8");
      return /["']go["'],\s*\[[^\]]*["']test["']/.test(source) || /\bgo\s+(?:-C\s+\S+\s+)?test\b/.test(source);
    });
    expect(spawnsGoTest).toEqual([]);
  });

  const pkg = JSON.parse(readFileSync("package.json", "utf8")) as {
    scripts?: Record<string, string>;
  };
  const platform = readFileSync("cli/platform.go", "utf8");
  const keyringService = readFileSync("cli/keyring_service.go", "utf8");
  const helpers = readFileSync("tests/onboarding/_helpers.ts", "utf8");
  const contributing = readFileSync("CONTRIBUTING.md", "utf8");
  const releasing = `${readFileSync("docs/releasing.md", "utf8")}\n${readFileSync("docs/release-validation.md", "utf8")}`;
  const ci = parse(readFileSync(".github/workflows/ci.yml", "utf8")) as {
    env?: Record<string, number>;
    jobs: Record<string, {
      name?: string;
      if?: string;
      env?: Record<string, number>;
      strategy?: unknown;
      needs?: string | string[];
      "continue-on-error"?: boolean;
      steps?: Array<{
        if?: string;
        name?: string;
        run?: string;
        env?: Record<string, string>;
        "continue-on-error"?: boolean;
      }>;
    }>;
  };

  it("gates every CI job through ci-gate", () => {
    // npm run verify runs in two jobs: verify (everything but the Go suite)
    // and go-test (the Go suite).
    expect(pkg.scripts?.verify).toBe("npm run verify:node && npm run test:cli");
    const verifySteps = ci.jobs["verify"]?.steps ?? [];
    expectFragmentsInOrder(
      verifySteps.map((step) => step.name ?? "").join("\n"),
      ["Setup Go", "Warm the Go cache", "Install", "Verify repository"],
    );
    expect(verifySteps.find((step) => step.name === "Install")?.run).toBe("npm ci");
    expect(verifySteps.find((step) => step.name === "Verify repository")?.run).toBe("npm run verify:node");
    // go-test-N and go-race-N are the shards of one run each: separate jobs
    // (ci-gate sees every result), the same steps, and one SHARD_INDEX each
    // from 0 up to the shard count the script deals over.
    const shards = (kind: string, count: string) => {
      const total = ci.env?.[count] ?? 0;
      const jobs = Array.from({ length: total }, (_, i) => `${kind}-${i + 1}`);
      expect(total).toBeGreaterThan(1);
      expect(Object.keys(ci.jobs).filter((job) => job.startsWith(`${kind}-`)).sort()).toEqual(jobs);
      for (const [i, job] of jobs.entries()) {
        expect(ci.jobs[job]).toMatchObject({ name: job, needs: "changes" });
        expect(ci.jobs[job]?.env).toEqual({ SHARD_INDEX: i });
        expect(ci.jobs[job]?.strategy).toBeUndefined();
        expect(ci.jobs[job]?.steps).toEqual(ci.jobs[`${kind}-1`]?.steps);
      }
      return jobs;
    };
    const goTests = shards("go-test", "GO_TEST_SHARDS");
    const goRaces = shards("go-race", "GO_RACE_SHARDS");
    const goTestSteps = ci.jobs["go-test-1"]?.steps ?? [];
    expect(goTestSteps.find((step) => step.name === "Install")?.run).toBe("npm ci");
    expect(goTestSteps.find((step) => step.name === "Go tests")?.run).toBe(
      'bash .github/scripts/go-test-shard.sh "$SHARD_INDEX" "$GO_TEST_SHARDS" . -count=1',
    );
    expect(ci.jobs["go-race-1"]?.steps?.find((step) => step.name === "Race detector (setup, confirm, and gate)")?.run).toMatch(
      /^bash \.github\/scripts\/go-test-shard\.sh "\$SHARD_INDEX" "\$GO_RACE_SHARDS" '[^']+' -race -count=1$/,
    );
    expect(ci.jobs["verify"]?.needs).toBeUndefined();

    // ci-gate is the required check and waits for every job, the secret scan
    // and the native macOS and Windows jobs included.
    const gated = [
      "changes",
      "repo-hygiene",
      "verify",
      ...goTests,
      "go-build",
      ...goRaces,
      "ha-integration",
      "govulncheck",
      "macos-native-secret-boundary",
      "windows-native-secret-boundary",
      "windows-powershell-unicode",
    ];
    expect(Object.keys(ci.jobs).filter((job) => job !== "ci-gate").sort()).toEqual([...gated].sort());
    expect(ci.jobs["ci-gate"]?.needs).toEqual(gated);
    // A skipped required check counts as passed: ci-gate always runs and fails
    // unless the changes job succeeded and every needed job succeeded.
    expect(ci.jobs["ci-gate"]?.if).toBe("${{ always() }}");
    const ciGateSteps = ci.jobs["ci-gate"]?.steps ?? [];
    expect(ciGateSteps).toHaveLength(1);
    expect(ciGateSteps[0]).toMatchObject({
      name: "Require the needed jobs",
      env: { NEEDS: "${{ toJSON(needs) }}" },
    });
    expect(ciGateSteps[0]?.if).toBeUndefined();
    // Run the gate on job results: a job may be skipped only when the run map
    // marks it false; anything else that is not success fails.
    const gate = (needs: unknown) =>
      spawnSync("bash", ["-c", ciGateSteps[0]?.run ?? "exit 0"], {
        encoding: "utf8",
        env: { ...process.env, NEEDS: JSON.stringify(needs) },
      }).status;
    const results = (run: string, changes: string, scoped: string, rest = "success") => ({
      changes: { result: changes, outputs: { run } },
      "repo-hygiene": { result: rest, outputs: {} },
      verify: { result: rest, outputs: {} },
      "go-test-1": { result: scoped, outputs: {} },
    });
    const unneeded = JSON.stringify({ "go-test-1": false });
    expect(gate(results(unneeded, "success", "skipped"))).toBe(0);
    expect(gate(results(JSON.stringify({ "go-test-1": true }), "success", "success"))).toBe(0);
    expect(gate(results(JSON.stringify({ "go-test-1": true }), "success", "skipped"))).not.toBe(0);
    expect(gate(results("{}", "success", "skipped"))).not.toBe(0);
    expect(gate(results(JSON.stringify({ "go-test-1": "false" }), "success", "skipped"))).not.toBe(0);
    expect(gate(results('"x"', "success", "skipped"))).not.toBe(0);
    expect(gate(results("", "success", "skipped"))).not.toBe(0);
    expect(gate(results(unneeded, "failure", "skipped"))).not.toBe(0);
    expect(gate(results(unneeded, "success", "failure"))).not.toBe(0);
    expect(gate(results(unneeded, "success", "skipped", "cancelled"))).not.toBe(0);
    expect(gate(results(unneeded, "success", "skipped", "skipped"))).not.toBe(0);
    expect(gate({ verify: { result: "success", outputs: {} } })).not.toBe(0);
    for (const job of gated) {
      expect(ci.jobs[job]?.["continue-on-error"]).toBeUndefined();
      expect((ci.jobs[job]?.steps ?? []).filter((step) => step.if || step["continue-on-error"])).toEqual([]);
    }

    // Path scoping: a job skips only through the run map of the changes job,
    // which lists its areas. The changes job itself always runs.
    const changesSteps = ci.jobs["changes"]?.steps ?? [];
    expect(ci.jobs["changes"]?.if).toBeUndefined();
    const select = changesSteps.find((step) => step.name === "Select jobs");
    // Only a pull request into main or a release train is scoped, never a
    // change under .github/ (the classifier lives there).
    expect(select?.run).toContain('[[ "$EVENT" == pull_request ]]');
    expect(select?.run).toContain('[[ "$BASE_REF" == "$DEFAULT_BRANCH" || "$BASE_REF" == release/* ]]');
    expect(select?.run).toContain("git rev-parse --verify --quiet HEAD^2");
    expect(select?.run).toContain("git diff --quiet HEAD^1 HEAD -- .github; then");
    expect(select?.run).toContain("bash .github/scripts/changed-areas.sh --all");
    const jobAreas = JSON.parse(select?.env?.JOB_AREAS ?? "{}") as Record<string, string[]>;
    expect(Object.keys(jobAreas).sort()).toEqual([
      "go-build",
      ...goRaces,
      ...goTests,
      "govulncheck",
      "ha-integration",
      "macos-native-secret-boundary",
      "windows-native-secret-boundary",
      "windows-powershell-unicode",
    ]);
    // The Go tests read skills/ and docs/work/: they run for those areas too.
    for (const job of goTests) expect(jobAreas[job]).toEqual(["go", "relay", "skills", "docs"]);
    for (const job of goRaces) expect(jobAreas[job]).toEqual(jobAreas["go-build"]);
    for (const [job, config] of Object.entries(ci.jobs)) {
      if (job === "ci-gate") continue;
      if (jobAreas[job]) {
        expect(config.if).toBe(`\${{ fromJSON(needs.changes.outputs.run)['${job}'] }}`);
        expect([config.needs].flat()).toContain("changes");
      } else {
        expect(config.if).toBeUndefined();
      }
    }
    // The scan fails the job on any finding or load error, never swallows one.
    const scan = ci.jobs["govulncheck"]?.steps?.find((step) => step.run?.includes("govulncheck@v"))?.run ?? "";
    expect(scan).toContain('exit "$status"');
    expect(scan).not.toMatch(/\|\|\s*(?:true|:)|exit 0/);
    for (const job of [
      "go-build",
      "macos-native-secret-boundary",
      "windows-native-secret-boundary",
      "windows-powershell-unicode",
    ]) {
      expect(ci.jobs[job]?.needs).toBe("changes");
    }
  });

  it("keeps npm test and verify host-safe", () => {
    expect(pkg.scripts?.["test:safe"]).toBe("vitest run");
    expect(pkg.scripts?.test).toBe("npm run test:safe");
    expect(pkg.scripts?.["test:watch"]).toBe("vitest");
    expect(pkg.scripts?.["verify:security"]).toBe("bash scripts/release/verify-npm-audit.sh");
    expect(pkg.scripts?.verify).toBe("npm run verify:node && npm run test:cli");
    // verify:node is the half that is safe on any host: one Vitest sweep and
    // no Go suite.
    const verifyNode = pkg.scripts?.["verify:node"] ?? "";
    expectFragmentsInOrder(verifyNode, [
      "npm run verify:security",
      "bash scripts/release/verify-blocked-files.sh",
      "npm run verify:release-metadata",
      "npm run typecheck",
      "bash scripts/check-docs.sh",
      "npm run build",
      "npm run test:safe",
    ]);
    expect(verifyNode.match(/vitest|test:safe/g)).toEqual(["test:safe"]);
    expect(verifyNode).not.toMatch(/test:cli|go\stest|test:desktop/);
  });

  it("defines an explicit macOS desktop validation command instead of mixing it into npm test", () => {
    expect(pkg.scripts?.["test:desktop:macos"]).toContain("macos-private-rc-suite.sh");
    expect(pkg.scripts?.["test:desktop:linux:antigravity"]).toContain("linux-headless-setup-check.sh");
    expect(pkg.scripts?.["test:desktop:linux:antigravity"]).toContain("ha-nova setup antigravity");
    expect(pkg.scripts?.["test:desktop:windows:headless"]).toContain("windows-private-rc-install.ps1");
    expect(pkg.scripts?.["test:desktop:windows:rdp"]).toContain("windows-desktop-setup.ps1");
    expect(pkg.scripts?.["test:desktop:windows:antigravity"]).toContain("windows-desktop-setup.ps1");
    expect(pkg.scripts?.["test:desktop:windows:antigravity"]).toContain("-Client antigravity");
  });

  it("keeps the Go runtime no-browser guard for setup flows", () => {
    expect(platform).toContain('os.Getenv("HA_NOVA_NO_BROWSER") == "1"');
    expect(platform).toContain("clipboard disabled");
  });

  it("supports a file-based test keyring override", () => {
    expect(keyringService).toContain('os.Getenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING") != "1"');
    expect(keyringService).toContain('os.Getenv("HA_NOVA_TEST_KEYRING_FILE")');
    expect(keyringService).toContain("writeRelayAuthTokenOverride");
    expect(keyringService).toContain("readRelayAuthTokenOverride");
    expect(keyringService).toContain("deleteRelayAuthTokenOverride");
    expect(helpers).toContain('HA_NOVA_ALLOW_INSECURE_TEST_KEYRING');
    expect(helpers).toContain('HA_NOVA_TEST_KEYRING_FILE');
  });

  it("documents host-safe defaults and explicit desktop validation", () => {
    expect(contributing).toContain("host-safe");
    expect(contributing).toContain("npm run verify:node");
    // The Go suite pairs and purges: on a Mac only in isolation (AGENTS.md).
    expect(contributing).toContain("on macOS, run the Go suite only in a Linux container or a separate macOS user");
    expect(contributing).toContain("test:desktop:macos");
    expect(contributing).toContain("test:desktop:windows:headless");
    expect(contributing).toContain("test:desktop:windows:rdp");
    expect(contributing).toContain("start-local-validation-harness");
    expect(contributing).toContain("dev:validation:harness");
    expect(contributing).toContain("pkill -f");
    expect(releasing).toContain("host-safe");
    expect(releasing).toContain("test:desktop:macos");
    expect(releasing).toContain("test:desktop:windows:headless");
    expect(releasing).toContain("test:desktop:windows:rdp");
    expect(releasing).toContain("start-local-validation-harness");
    expect(releasing).toContain("dev:validation:harness");
    expect(releasing).toContain("pkill -f");
    expect(releasing).toContain("verify-npm-audit.sh");
  });
});
