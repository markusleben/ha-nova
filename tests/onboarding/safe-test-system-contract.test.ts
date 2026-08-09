import { mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";

import { describe, expect, it } from "vitest";

// CI runs Node 20, where fs.globSync does not exist yet — walk the tree instead.
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
function collectVitestModules(dir: string): string[] {
  const found: string[] = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) {
      found.push(...collectVitestModules(path));
    } else if (/\.[cm]?[jt]sx?$/.test(entry)) {
      const body = readFileSync(path, "utf8");
      if (/from ["']vitest["']/.test(body)) found.push(path.split("\\").join("/"));
    }
  }
  return found;
}

function expectFragmentsInOrder(haystack: string, fragments: string[]) {
  let cursor = 0;
  for (const fragment of fragments) {
    const index = haystack.indexOf(fragment, cursor);
    expect(index).toBeGreaterThanOrEqual(0);
    cursor = index + fragment.length;
  }
}

function expectNoFullVitestSweep(verifyScript: string) {
  expect(verifyScript).not.toMatch(/(^|&& )npm run test:safe($| &&)/);
}

describe("safe test system contract", () => {
  // Every test file must actually RUN in `npm run verify`. A test file that no
  // script references is worse than no test: it looks like coverage and proves
  // nothing. This guard exists because 50 /files security tests and the skill
  // linter silently never ran in CI — they were simply missing from the
  // manifest.
  it("runs every test file through verify", () => {
    const manifest = new Set(
      JSON.parse(readFileSync("scripts/test/safe-core-files.json", "utf8")) as string[]
    );
    // Only scripts REACHABLE FROM `verify` count. Matching against every
    // script would let a file referenced solely by, say, `test:bulk:fast`
    // satisfy a check whose stated invariant is "runs in npm run verify"
    // (#515).
    const allScripts = (
      JSON.parse(readFileSync("package.json", "utf8")) as {
        scripts: Record<string, string>;
      }
    ).scripts;
    const reachable = new Set<string>();
    const visit = (name: string): void => {
      if (reachable.has(name) || !allScripts[name]) return;
      reachable.add(name);
      for (const ref of allScripts[name].matchAll(/npm run ([\w:-]+)/g)) {
        visit(ref[1] as string);
      }
      // npm runs pre<script>/post<script> hooks implicitly.
      for (const hook of [`pre${name}`, `post${name}`]) visit(hook);
    };
    visit("verify");
    const scripts = [...reachable].map((n) => allScripts[n]).join(" ");

    const testFiles = collectTestFiles("tests");
    expect(testFiles.length).toBeGreaterThan(50);
    expect(reachable.size).toBeGreaterThan(5);

    const orphans = testFiles.filter(
      (file) => !manifest.has(file) && !scripts.includes(file)
    );

    // Unsuffixed behavior modules: this repo splits some suites into
    // `*-behavior.ts` files that import vitest and are pulled in by a wrapper.
    // They carry real assertions, so an unimported one is silently dead — the
    // same failure the suffix check exists to prevent, one indirection over.
    const vitestModules = collectVitestModules("tests");
    const isEntrypoint = (file: string) => /\.(test|spec)\.[cm]?[jt]sx?$/.test(file);
    const behaviorModules = vitestModules.filter((file) => !isEntrypoint(file));

    // Resolve real module specifiers, not basename occurrences: a name left
    // in a comment must not count as an import, and a cycle of behavior
    // modules importing each other must not count as reachable.
    const importsOf = (file: string): string[] => {
      const dir = file.split("/").slice(0, -1).join("/");
      const body = readFileSync(file, "utf8");
      return [...body.matchAll(/(?:from|import)\s+["'](\.[^"']+)["']/g)]
        .map((m) => (m[1] as string).replace(/\.js$/, ""))
        .map((spec) => {
          const base = spec.startsWith("./") || spec.startsWith("../")
            ? join(dir, spec).split("\\").join("/")
            : spec;
          return vitestModules.find(
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
    // Only entrypoints that actually run seed the traversal, so a cycle among
    // behavior modules cannot make itself reachable.
    for (const entry of vitestModules.filter(isEntrypoint)) {
      if (manifest.has(entry) || scripts.includes(entry)) walk(entry);
    }
    const unreachable = behaviorModules.filter((file) => !reachableModules.has(file));
    expect(
      unreachable,
      `these vitest modules are imported by nothing and therefore never run:\n  ${unreachable.join("\n  ")}`
    ).toEqual([]);
    expect(
      orphans,
      `these test files are never executed by npm run verify — add them to scripts/test/safe-core-files.json or to an explicit verify step:\n  ${orphans.join("\n  ")}`
    ).toEqual([]);
  });

  const pkg = JSON.parse(readFileSync("package.json", "utf8")) as {
    scripts?: Record<string, string>;
  };
  const platform = readFileSync("cli/platform.go", "utf8");
  const keyringService = readFileSync("cli/keyring_service.go", "utf8");
  const safeCoreFiles = JSON.parse(readFileSync("scripts/test/safe-core-files.json", "utf8")) as string[];
  const helpers = readFileSync("tests/onboarding/_helpers.ts", "utf8");
  const contributing = readFileSync("CONTRIBUTING.md", "utf8");
  const releasing = readFileSync("docs/releasing.md", "utf8");

  it("keeps npm test and verify host-safe", () => {
    expect(pkg.scripts?.["test:safe"]).toBe("vitest run");
    expect(pkg.scripts?.["test:safe:core"]).toBe("node scripts/test/run-safe-core.mjs");
    expect(safeCoreFiles.length).toBeGreaterThan(0);
    expect(safeCoreFiles).toContain("tests/http/health.test.ts");
    expect(pkg.scripts?.test).toBe("npm run test:safe");
    expect(pkg.scripts?.["test:watch"]).toBe("vitest");
    expect(pkg.scripts?.["verify:security"]).toBe("bash scripts/release/verify-npm-audit.sh");
    expect(pkg.scripts?.["preverify:docs"]).toBe("node scripts/test/assert-vitest-files-exist.mjs verify:docs");
    expect(pkg.scripts?.["verify:docs"]).toContain("scripts/check-docs.sh");
    expect(pkg.scripts?.["preverify:installers"]).toBe("node scripts/test/assert-vitest-files-exist.mjs verify:installers");
    expect(pkg.scripts?.["verify:installers"]).toBe(
      "npx vitest run tests/onboarding/installer-contract.test.ts tests/onboarding/windows-installer-contract.test.ts tests/onboarding/windows-installer-preflight.test.ts",
    );
    expect(pkg.scripts?.["preverify:onboarding"]).toBe("node scripts/test/assert-vitest-files-exist.mjs verify:onboarding");
    expect(pkg.scripts?.["verify:onboarding"]).toContain("tests/onboarding/install-skills-per-client.test.ts");
    expect(pkg.scripts?.["verify:onboarding"]).toContain("npm run verify:installers");
    expect(pkg.scripts?.["preverify:release-contracts"]).toBe("node scripts/test/assert-vitest-files-exist.mjs verify:release-contracts");
    expect(pkg.scripts?.["verify:release-contracts"]).toContain("tests/onboarding/release-contract.test.ts");
    expect(pkg.scripts?.["verify:release-contracts"]).toContain("tests/onboarding/desktop-validation-behavior.test.ts");
    const verify = pkg.scripts?.verify ?? "";
    expectFragmentsInOrder(verify, [
      "npm run verify:security",
      "bash scripts/release/verify-blocked-files.sh",
      "npm run typecheck",
      "npm run verify:docs",
      "npm run test:safe:core",
      "npm run verify:onboarding",
      "npm run build",
      "npm run test:cli",
      "npm run verify:release-contracts",
    ]);
    expectNoFullVitestSweep(verify);
    expect(verify).not.toContain("test:desktop");
  });

  it("fails scripted Vitest runs before missing files can be skipped", () => {
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-test-guard-"));
    try {
      mkdirSync(join(tempDir, "tests"), { recursive: true });
      writeFileSync(
        join(tempDir, "package.json"),
        JSON.stringify({
          scripts: {
            broken: "npx vitest run tests/missing.test.ts",
            valid: "npx vitest run tests/present.test.ts",
          },
        }),
      );
      writeFileSync(join(tempDir, "tests/present.test.ts"), "import { it } from 'vitest'; it('passes', () => {});\n");

      const guardPath = join(process.cwd(), "scripts/test/assert-vitest-files-exist.mjs");
      const broken = spawnSync(process.execPath, [guardPath, "broken"], { cwd: tempDir, encoding: "utf8" });
      expect(broken.status).toBe(1);
      expect(broken.stderr).toContain("references missing test files: tests/missing.test.ts");

      const valid = spawnSync(process.execPath, [guardPath, "valid"], { cwd: tempDir, encoding: "utf8" });
      expect(valid.status).toBe(0);
    } finally {
      rmSync(tempDir, { recursive: true, force: true });
    }
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
    expect(contributing).toContain("verify:docs");
    expect(contributing).toContain("verify:installers");
    expect(contributing).toContain("verify:onboarding");
    expect(contributing).toContain("verify:release-contracts");
    expect(contributing).toContain("test:safe:core");
    expect(contributing).toContain("test:desktop:macos");
    expect(contributing).toContain("test:desktop:windows:headless");
    expect(contributing).toContain("test:desktop:windows:rdp");
    expect(contributing).toContain("start-local-validation-harness");
    expect(contributing).toContain("dev:validation:harness");
    expect(contributing).toContain("pkill -f");
    expect(releasing).toContain("host-safe");
    expect(releasing).toContain("verify:installers");
    expect(releasing).toContain("test:desktop:macos");
    expect(releasing).toContain("test:desktop:windows:headless");
    expect(releasing).toContain("test:desktop:windows:rdp");
    expect(releasing).toContain("test:safe:core");
    expect(releasing).toContain("start-local-validation-harness");
    expect(releasing).toContain("dev:validation:harness");
    expect(releasing).toContain("pkill -f");
    expect(releasing).toContain("verify-npm-audit.sh");
  });
});
