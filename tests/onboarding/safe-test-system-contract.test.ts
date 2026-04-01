import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

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
    expect(pkg.scripts?.["verify:docs"]).toContain("scripts/check-docs.sh");
    expect(pkg.scripts?.["verify:installers"]).toBe("node scripts/install-src/build-installers.mjs --check");
    expect(pkg.scripts?.["verify:onboarding"]).toContain("tests/onboarding/install-skills-per-client.test.ts");
    expect(pkg.scripts?.["verify:onboarding"]).toContain("npm run verify:installers");
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

  it("defines an explicit macOS desktop validation command instead of mixing it into npm test", () => {
    expect(pkg.scripts?.["test:desktop:macos"]).toContain("macos-private-rc-suite.sh");
    expect(pkg.scripts?.["test:desktop:windows:headless"]).toContain("windows-private-rc-install.ps1");
    expect(pkg.scripts?.["test:desktop:windows:rdp"]).toContain("windows-desktop-setup.ps1");
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
