import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("safe test system contract", () => {
  const pkg = JSON.parse(readFileSync("package.json", "utf8")) as {
    scripts?: Record<string, string>;
  };
  const platform = readFileSync("cli/platform.go", "utf8");
  const keyringService = readFileSync("cli/keyring_service.go", "utf8");
  const helpers = readFileSync("tests/onboarding/_helpers.ts", "utf8");
  const contributing = readFileSync("CONTRIBUTING.md", "utf8");
  const releasing = readFileSync("docs/releasing.md", "utf8");

  it("keeps npm test and verify host-safe", () => {
    expect(pkg.scripts?.["test:safe"]).toBe("vitest run");
    expect(pkg.scripts?.test).toBe("npm run test:safe");
    expect(pkg.scripts?.["test:watch"]).toBe("vitest");
    expect(pkg.scripts?.["verify:security"]).toBe("bash scripts/release/verify-npm-audit.sh");
    expect(pkg.scripts?.verify).toBe(
      "npm run verify:release-metadata && npm run verify:security && bash scripts/release/verify-blocked-files.sh && npm run typecheck && npm run test:safe && npm run build && bash scripts/check-docs.sh && npm run test:cli"
    );
    expect(pkg.scripts?.verify).not.toContain("test:desktop");
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
