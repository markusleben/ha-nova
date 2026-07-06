import { spawnSync } from "node:child_process";

import { describe, expect, it } from "vitest";
import { REPO_ROOT } from "./_helpers.js";

describe("go runtime onboarding contract", () => {
  function expectGoTestsToPass(pattern: string): void {
    const result = spawnSync("go", ["test", "./...", "-run", pattern, "-count=1"], {
      cwd: `${REPO_ROOT}/cli`,
      encoding: "utf8",
      timeout: 180000,
    });

    expect(result.status, result.stdout + result.stderr).toBe(0);
  }

  it("executes the current Go wizard coverage", { timeout: 120000 }, () => {
    expectGoTestsToPass(
      "TestInteractiveSetupFreshInstallShowsWizardAndInstallsAntigravitySkills|TestInteractiveSetupFreshInstallCanPasteExistingRelayToken|TestInteractiveSetupFreshInstallGuidesLLATSetup|TestInteractiveSetupWithHostAndRelayTokenFlagsSkipsLLATWalkthrough|TestInteractiveSetupPartialResumeSkipsTokenChoiceAndReentersLLATGuideWhenWSIsPending|TestInteractiveSetupBackFromVerifyDoesNotPersistConfig",
    );
  });

  it("executes readiness coverage on the Go runtime path", { timeout: 120000 }, () => {
    expectGoTestsToPass(
      "TestDetectSetupStateUsesWSPingFallbackForResume|TestCheckRelayReadinessAcceptsWSPingSuccess|TestRunDoctorTreatsWSPingSuccessAsReady",
    );
  });

  it("executes uninstall truth coverage on the Go runtime path", { timeout: 120000 }, () => {
    expectGoTestsToPass(
      "TestRunUninstallShowsPreflightAndRelayStillRunningNote|TestRunUninstallPreservesUnknownConfigAndCacheFiles|TestRunUninstallNoopDoesNotClaimRemoval",
    );
  });
});
