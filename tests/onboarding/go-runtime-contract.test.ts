import { spawnSync } from "node:child_process";

import { describe, expect, it } from "vitest";
import { REPO_ROOT } from "./_helpers.js";

const GO_TEST_TIMEOUT_MS = 180_000;
const WRAPPER_TIMEOUT_MS = GO_TEST_TIMEOUT_MS + 30_000;

describe("go runtime onboarding contract", () => {
  function expectGoTestsToPass(pattern: string): void {
    const result = spawnSync("go", ["test", "./...", "-run", pattern, "-count=1"], {
      cwd: `${REPO_ROOT}/cli`,
      encoding: "utf8",
      timeout: GO_TEST_TIMEOUT_MS,
    });

    expect(result.status, result.stdout + result.stderr).toBe(0);
  }

  it("executes the current Go wizard coverage", { timeout: WRAPPER_TIMEOUT_MS }, () => {
    expectGoTestsToPass(
      "TestInteractiveSetupFreshInstallShowsWizardAndInstallsGeminiSkills|TestInteractiveSetupFreshInstallCanPasteExistingRelayToken|TestInteractiveSetupFreshInstallPastedTokenSkipsLLATWalkthroughWhenVerifySucceeds|TestInteractiveSetupWithHostAndRelayTokenFlagsSkipsLLATWalkthrough|TestInteractiveSetupPartialResumeSkipsTokenChoiceAndVerifiesFirstWhenWSIsPending|TestInteractiveSetupBackFromVerifyDoesNotPersistConfig",
    );
  });

  it("executes readiness coverage on the Go runtime path", { timeout: WRAPPER_TIMEOUT_MS }, () => {
    expectGoTestsToPass(
      "TestDetectSetupStateUsesWSPingFallbackForResume|TestCheckRelayReadinessAcceptsWSPingSuccess|TestRunDoctorTreatsWSPingSuccessAsReady",
    );
  });

  it("executes uninstall truth coverage on the Go runtime path", { timeout: WRAPPER_TIMEOUT_MS }, () => {
    expectGoTestsToPass(
      "TestRunUninstallShowsPreflightAndRelayStillRunningNote|TestRunUninstallPreservesUnknownConfigAndCacheFiles|TestRunUninstallNoopDoesNotClaimRemoval",
    );
  });
});
