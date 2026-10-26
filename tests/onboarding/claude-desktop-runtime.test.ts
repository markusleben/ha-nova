import { spawnSync } from "node:child_process";

import { expect, it } from "vitest";

import { REPO_ROOT } from "./_helpers.js";

it("uses Desktop's bundled Claude Code without changing PATH users' profile", { timeout: 180000 }, () => {
  const tests = [
    "TestDesktopClaudeNewestWorkingRuntime",
    "TestDesktopClaudeRejectsIncompleteAndUnsupportedRuntimes",
    "TestDesktopClaudePATHAlwaysWinsIncludingCommandFailure",
    "TestDesktopClaudeOtherPlatformsDoNotSearchMacBundles",
    "TestDesktopClaudeUserLocalRuntimeStillWins",
    "TestDesktopClaudeRuntimeProbeHasDeadline",
    "TestDesktopClaudeTerminalUsesQuotedBundledBinary",
    "TestDesktopClaudeLinuxTerminalUsesResolvedRuntime",
    "TestDesktopClaudeInstallsAndVerifiesSelectedProfile",
    "TestDesktopClaudePluginListUsesBundleAndInheritedProfile",
    "TestDesktopClaudePrivateConfigWarningDoesNotDecryptOrRedirect",
    "TestClaudeStatusReasonNamesClaudeDesktopWithoutClaudeCode",
    "TestDoctorNamesAnOutdatedClaudePlugin",
    "TestInstallClaudePluginFailsWhenClaudeCLIMissing",
    "TestInstallClaudePluginMigratesFloatingGitHubMarketplaceToVersionedLocalSnapshot",
    "TestInstallClaudePluginMigratesStructuredGitHubMarketplaceSourceToVersionedLocalSnapshot",
    "TestInstallClaudePluginReplacesPinnedGitHubMarketplaceRefWithVersionedLocalSnapshot",
    "TestInstallClaudePluginClearsMarketplaceRegistrationWhenFreshInstallFails",
    "TestClaudeConfigRootDefaultsToDotClaude",
    "TestClaudeConfigRootHonorsClaudeConfigDir",
    "TestClaudePluginInstallStateFollowsClaudeConfigDir",
    "TestClaudePluginPresenceCLIAnswerCoversMissingFileEntry",
    "TestClaudePluginPresenceCLIEntryWithoutInstallPathIsUsable",
    "TestClaudePluginPresenceHealthyFileShortCircuitsWithoutExec",
    "TestClaudePluginPresenceExplicitDisableOutranksHealthyFileWithoutExec",
    "TestClaudePluginPresenceUnreadableFileNeverExecs",
    "TestClaudePluginPresenceFallsBackToFileVerdictOnNonJSONOutput",
    "TestClaudePluginPresenceCLIDisabledPluginIsNotAttached",
    "TestClaudePluginPresenceCLINegativeIsAuthoritative",
    "TestVerifyClaudePluginReinstallsAPluginClaudeCodeKeptOld",
    "TestVerifyClaudePluginFailsWhenClaudeCodeKeepsItOld",
    "TestSetupCompleteBannerSaysWhichClaudeGetsHANova",
  ];
  const result = spawnSync("go", ["test", "-p", "2", "./...", "-run", `^(${tests.join("|")})$`, "-count=1", "-v"], {
    cwd: `${REPO_ROOT}/cli`,
    encoding: "utf8",
    timeout: 170000,
  });
  expect(result.status, result.stdout + result.stderr).toBe(0);
  for (const test of tests) expect(result.stdout).toContain(`=== RUN   ${test}\n`);
});
