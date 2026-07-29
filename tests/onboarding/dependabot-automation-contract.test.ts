import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const botOwnedAutoMergeQuery =
  '.autoMergeRequest.enabledBy.login == "github-actions[bot]"';

function botOwnsNativeAutoMerge(login: string): boolean {
  const result = spawnSync("jq", ["-r", botOwnedAutoMergeQuery], {
    encoding: "utf8",
    input: JSON.stringify({ autoMergeRequest: { enabledBy: { login } } }),
  });
  if (result.error !== undefined || result.status !== 0) {
    throw new Error(`jq ownership check failed: ${result.stderr}`);
  }
  return result.stdout.trim() === "true";
}

describe("dependabot automation contract", () => {
  const agents = readFileSync("AGENTS.md", "utf8");
  const codeowners = readFileSync(".github/CODEOWNERS", "utf8");
  const dependabot = readFileSync(".github/dependabot.yml", "utf8");
  const prepareWorkflow = readFileSync(".github/workflows/dependabot-safe-lane-prepare.yml", "utf8");
  const manifestGate = readFileSync(".github/workflows/manifest-review-gate.yml", "utf8");
  const policy = JSON.parse(readFileSync(".github/policy/repo-policy.json", "utf8")) as {
    manifest_review: {
      label: string;
      approvers: string[];
      manifest_sensitive_files: string[];
    };
    dependabot_safe_lane: {
      label: string;
      allowed_package_ecosystems: string[];
      dependency_group: string;
      allowed_update_types: string[];
      manual_review_dependencies: string[];
      github_actions: {
        dependency_group: string;
        allowed_update_types: string[];
        workflow_file_prefix: string;
      };
    };
    main_branch_protection: {
      required_status_checks: string[];
      required_approving_review_count: number;
      require_code_owner_reviews: boolean;
      dismiss_stale_reviews: boolean;
      required_conversation_resolution: boolean;
      strict_required_status_checks: boolean;
      required_status_check_apps: Record<string, number>;
      advisory_checks: string[];
    };
    production_environment: {
      name: string;
      deployment_branch_policy: Record<string, boolean>;
      deployment_branch_policies: Array<{ name: string; type: string }>;
      protection_rule_types: string[];
    };
    cloud_source_gate: {
      check_name: string;
      reporter_app_id: number;
      reporter_app_slug: string;
      sensitive_workflows: string[];
    };
  };
  const protectionScript = readFileSync("scripts/release/verify-github-main-protection.sh", "utf8");
  const productionEnvironmentScript = readFileSync("scripts/release/verify-github-production-environment.sh", "utf8");
  const cloudUsesOnlyScript = readFileSync("scripts/release/verify-cloud-workflow-uses-only.mjs", "utf8");
  const releasing = readFileSync("docs/releasing.md", "utf8");
  const mergeWorkflow = readFileSync(".github/workflows/dependabot-safe-auto-merge.yml", "utf8");
  const directMergeScript = readFileSync("scripts/release/merge-safe-dependabot-pr.mjs", "utf8");
  const directMergePolicy = readFileSync("scripts/release/dependabot-direct-merge-policy.mjs", "utf8");

  it("keeps the safe dev-only npm lane in a dedicated Dependabot group", () => {
    expect(policy.manifest_review.label).toBe("manifest-review:approved");
    expect(policy.manifest_review.approvers).toEqual(["markusleben"]);
    expect(policy.manifest_review.manifest_sensitive_files).toEqual(["package.json", "package-lock.json", "nova/package.json", "nova/package-lock.json"]);
    expect(policy.dependabot_safe_lane.label).toBe("dependabot-safe:auto-merge");
    expect(policy.dependabot_safe_lane.allowed_package_ecosystems).toEqual(["npm", "npm_and_yarn"]);
    expect(policy.dependabot_safe_lane.dependency_group).toBe("npm-dev-minor-patch");
    expect(policy.dependabot_safe_lane.allowed_update_types).toEqual(["version-update:semver-minor", "version-update:semver-patch"]);
    expect(policy.dependabot_safe_lane.manual_review_dependencies).toEqual(["vitest", "vite", "typescript", "tsx", "rollup", "rolldown", "esbuild"]);
    expect(dependabot).toContain("package-ecosystem: npm");
    expect(dependabot).toContain('directory: "/"');
    expect(dependabot).toContain('directory: "/nova"');
    expect(dependabot).toContain("npm-dev-minor-patch:");
    expect(dependabot).toContain("dependency-type: development");
    expect(dependabot).toContain("exclude-patterns:");
    for (const dependency of policy.dependabot_safe_lane.manual_review_dependencies) {
      expect(dependabot).toContain(`- ${dependency}`);
    }
    expect(dependabot).toContain("update-types:");
    expect(dependabot).toContain("- minor");
    expect(dependabot).toContain("- patch");
    expect(dependabot).not.toContain("npm-minor-patch:");
  });

  it("keeps the safe actions lane on uses-only minor/patch bumps in a dedicated group", () => {
    expect(policy.dependabot_safe_lane.github_actions.dependency_group).toBe("github-actions-minor-patch");
    expect(policy.dependabot_safe_lane.github_actions.allowed_update_types).toEqual(["version-update:semver-minor", "version-update:semver-patch"]);
    expect(policy.dependabot_safe_lane.github_actions.workflow_file_prefix).toBe(".github/workflows/");
    expect(dependabot).toContain("github-actions-minor-patch:");
    expect(dependabot).toContain("github-actions-major:");
    expect(dependabot).not.toContain("github-actions-all:");
    expect(dependabot).toContain("- major");
  });

  it("groups security updates and actions bumps to prevent single-dep PR deadlocks", () => {
    expect(dependabot).toContain("npm-security:");
    expect(dependabot).toContain("applies-to: security-updates");
    const securityGroups = dependabot.match(/npm-security:/g) ?? [];
    expect(securityGroups.length).toBe(2);
  });

  it("limits code owner review to sensitive paths and leaves package manifests out of CODEOWNERS", () => {
    expect(codeowners).not.toContain("* @markusleben");
    expect(codeowners).toContain("Package manifests intentionally stay outside CODEOWNERS");
    expect(codeowners).toContain("/.github/ @markusleben");
    expect(codeowners).toContain("/.claude-plugin/ @markusleben");
    expect(codeowners).toContain("/.goreleaser.yml @markusleben");
    expect(codeowners).toContain("/install.sh @markusleben");
    expect(codeowners).toContain("/install.ps1 @markusleben");
    expect(codeowners).toContain("/version.json @markusleben");
    expect(codeowners).toContain("/cli/ @markusleben");
    expect(codeowners).toContain("/clients/ @markusleben");
    expect(codeowners).toContain("/scripts/ @markusleben");
    expect(codeowners).toContain("/skills/ @markusleben");
    expect(codeowners).toContain("/tests/ @markusleben");
    expect(codeowners).toContain("/docs/reference/ @markusleben");
    expect(codeowners).not.toContain("/package.json @markusleben");
    expect(codeowners).not.toContain("/package-lock.json @markusleben");
    // Un-own entries (no owner, later match wins) keep the safe lanes automatic:
    // /nova manifests for the npm lane, workflow files for the actions lane.
    expect(codeowners).toMatch(/^\/nova\/package\.json$/m);
    expect(codeowners).toMatch(/^\/nova\/package-lock\.json$/m);
    expect(codeowners).toMatch(/^\/\.github\/workflows\/$/m);
  });

  it("only auto-approves and auto-merges the safe Dependabot manifest lane without checking out PR code", () => {
    expect(prepareWorkflow).toContain("pull_request_target:");
    expect(prepareWorkflow).toContain("- unlabeled");
    expect(prepareWorkflow).not.toContain("workflow_run:");
    expect(prepareWorkflow).toContain("Capture PR context");
    expect(prepareWorkflow).toContain('event_path="${GITHUB_EVENT_PATH:-}"');
    expect(prepareWorkflow).toContain('if [[ -z "${event_path}" || ! -f "${event_path}" ]]');
    expect(prepareWorkflow).toContain('echo "should-process=false" >> "${GITHUB_OUTPUT}"');
    expect(prepareWorkflow).toContain("pull_request.number //");
    expect(prepareWorkflow).toContain("pull_request.user.login //");
    expect(prepareWorkflow).toContain("pull_request.draft // false");
    expect(prepareWorkflow).toContain("if: ${{ github.event_name == 'pull_request_target' }}");
    expect(prepareWorkflow).toContain("if: ${{ steps.pr.outputs.should-process == 'true' }}");
    expect(prepareWorkflow).toContain("PR_NUMBER: ${{ steps.pr.outputs.pr-number }}");
    expect(prepareWorkflow).toContain("POLICY_REF: ${{ steps.pr.outputs.pr-base-sha }}");
    expect(prepareWorkflow).toContain("dependabot[bot]");
    expect(prepareWorkflow).toContain("dependabot/fetch-metadata@25dd0e34f4fe68f24cc83900b1fe3fe149efef98");
    expect(prepareWorkflow).toContain("DEPENDENCY_NAMES: ${{ steps.metadata.outputs.dependency-names }}");
    expect(prepareWorkflow).toContain("PACKAGE_ECOSYSTEM: ${{ steps.metadata.outputs.package-ecosystem }}");
    expect(prepareWorkflow).toContain("DEPENDENCY_GROUP: ${{ steps.metadata.outputs.dependency-group }}");
    expect(prepareWorkflow).toContain("UPDATE_TYPE: ${{ steps.metadata.outputs.update-type }}");
    expect(prepareWorkflow).toContain("safe-label=${safe_label}");
    expect(prepareWorkflow).toContain("policy-sha=${policy_sha}");
    expect(prepareWorkflow).toContain("SAFE_POLICY_MARKER");
    expect(prepareWorkflow).toContain("policy_sha=${POLICY_SHA}");
    expect(prepareWorkflow).toContain('reason="automation-owned safe label was removed"');
    expect(prepareWorkflow).toContain('issues/${PR_NUMBER}/comments" --paginate --slurp');
    expect(prepareWorkflow).toContain("GH_REPO: ${{ github.repository }}");
    expect(prepareWorkflow).toContain("dependency requires manual review due to toolchain risk");
    // Actions lane: dedicated group, minor/patch only, and a diff guard that
    // rejects any workflow change beyond uses: version bumps.
    expect(prepareWorkflow).toContain(".dependabot_safe_lane.github_actions.dependency_group");
    expect(prepareWorkflow).toContain('"${PACKAGE_ECOSYSTEM}" == "github_actions"');
    expect(prepareWorkflow).toContain("dependency group is not the safe actions minor/patch lane");
    expect(prepareWorkflow).toContain("changed file outside workflow lane");
    expect(prepareWorkflow).toContain("workflow change beyond uses: version bumps");
    expect(prepareWorkflow).toContain("no reviewable patch lines returned by GitHub API");
    expect(prepareWorkflow).toContain("(.previous_filename // empty)");
    expect(prepareWorkflow).toContain("uses:[[:space:]]+[^[:space:]]+([[:space:]]+#.*)?$");
    expect(prepareWorkflow).toContain("gh pr review --approve");
    expect(prepareWorkflow).toContain("Reevaluate the fully marked safe PR");
    expect(prepareWorkflow).toContain(
      'event_type: $event_type',
    );
    expect(prepareWorkflow).toContain(
      '"dependabot-safe-reevaluate"',
    );
    expect(prepareWorkflow).toContain(
      'gh api "repos/${GITHUB_REPOSITORY}/dispatches"',
    );
    expect(prepareWorkflow).toContain("--json autoMergeRequest,labels");
    expect(prepareWorkflow).toContain(
      `jq -r '${botOwnedAutoMergeQuery}'`,
    );
    expect(prepareWorkflow).toContain(
      'if [[ "${bot_auto_merge}" == "true" ]]; then',
    );
    expect(prepareWorkflow).not.toContain(
      "jq -e '.autoMergeRequest != null'",
    );
    expect(prepareWorkflow).toContain('gh pr merge "${PR_NUMBER}" --disable-auto');
    expect(prepareWorkflow).toContain('latest_label_actor="$(');
    expect(prepareWorkflow).toContain("] | last) as $event");
    expect(prepareWorkflow).toContain(
      '[[ "${latest_label_actor}" == "github-actions[bot]" ]]',
    );
    expect(prepareWorkflow).toContain(
      'current_labels_json="$(gh pr view "${PR_NUMBER}" --json labels)"',
    );
    expect(prepareWorkflow).toContain("jq -e --arg label \"${SAFE_LABEL}\" '.labels[]? | select(.name == $label)'");
    expect(prepareWorkflow).toContain('gh pr edit "${PR_NUMBER}" --remove-label "${SAFE_LABEL}"');
    expect(prepareWorkflow).not.toContain("actions/checkout");
    expect(prepareWorkflow).not.toContain("workflow_run");
    expect(prepareWorkflow).not.toContain("github.event.pull_request.number");
    expect(prepareWorkflow).not.toContain("github.event.pull_request.base.sha");
    expect(prepareWorkflow).not.toContain("github.event.pull_request.html_url");

    expect(mergeWorkflow).toContain("workflow_run:");
    expect(mergeWorkflow).toContain("check_run:");
    expect(mergeWorkflow).toContain("repository_dispatch:");
    expect(mergeWorkflow).toContain("- dependabot-safe-reevaluate");
    expect(mergeWorkflow).not.toContain("pull_request_target:");
    expect(mergeWorkflow).toContain("resolve-trigger:");
    expect(mergeWorkflow).toContain("permissions: {}");
    expect(mergeWorkflow).toContain("Authenticate trusted completion trigger");
    expect(mergeWorkflow).toContain(
      "startsWith(github.event.workflow_run.head_branch, 'dependabot/')",
    );
    expect(mergeWorkflow).toContain(
      "github.event.check_run.name == 'cloud-source-gate'",
    );
    expect(mergeWorkflow).toContain('TRIGGER_DIR="$(mktemp -d)"');
    expect(mergeWorkflow).toContain(
      'TRIGGER_SCRIPT="${TRIGGER_DIR}/resolve-dependabot-auto-merge-trigger.mjs"',
    );
    expect(mergeWorkflow).not.toContain('TRIGGER_SCRIPT="$(mktemp)"');
    expect(mergeWorkflow).toContain("resolve-dependabot-auto-merge-trigger.mjs");
    expect(mergeWorkflow).toContain("needs.resolve-trigger.outputs.should-process == 'true'");
    expect(mergeWorkflow).toContain("needs.resolve-trigger.outputs.run-kind");
    expect(mergeWorkflow).toContain("needs.resolve-trigger.outputs.policy-ref");
    expect(mergeWorkflow).toContain("needs.resolve-trigger.outputs.policy-sha");
    expect(mergeWorkflow).toContain("AUTHENTICATED_POLICY_REF: ${{ needs.resolve-trigger.outputs.policy-ref }}");
    expect(mergeWorkflow).toContain('[[ "${AUTHENTICATED_POLICY_REF}" =~ ^[0-9a-f]{40}$ ]]');
    expect(mergeWorkflow).toContain("ref=${AUTHENTICATED_POLICY_REF}");
    expect(mergeWorkflow).toContain("merge-safe-dependabot-pr.mjs");
    expect(mergeWorkflow).toContain("DEFAULT_BRANCH: ${{ github.event.repository.default_branch }}");
    expect(mergeWorkflow).not.toContain("github.event.workflow_run.event == 'pull_request'");
    expect(mergeWorkflow).toContain("GH_REPO: ${{ github.repository }}");
    expect(mergeWorkflow).not.toContain("--auto");
    expect(mergeWorkflow).not.toContain("dependabot/fetch-metadata");
    expect(mergeWorkflow).not.toContain("actions/checkout");
    expect(directMergeScript).toContain("disablePullRequestAutoMerge");
    expect(directMergeScript).toContain("disableBotOwnedAutoMerge");
    expect(directMergeScript).toContain(
      'runKind === "repository_dispatch"',
    );
    expect(directMergePolicy).toContain("latest dedicated-App source check targets another merge commit");
    expect(directMergeScript).toContain("a CI run is queued or in progress for the candidate head");
    expect(directMergeScript).toContain("pull request API and merge ref identify different merge commits");
    expect(directMergeScript).toContain('merge_method: "squash"');
    expect(directMergeScript).toContain("sha: final.headSHA");
  });

  it("preserves human-owned native auto-merge during ineligible cleanup", () => {
    expect(botOwnsNativeAutoMerge("markusleben")).toBe(false);
  });

  it("disables bot-owned native auto-merge during ineligible cleanup", () => {
    expect(botOwnsNativeAutoMerge("github-actions[bot]")).toBe(true);
  });

  it("blocks non-safe manifest changes unless a maintainer labels them approved", () => {
    expect(manifestGate).toContain("pull_request_target:");
    expect(manifestGate).toContain("POLICY_REF: ${{ github.event.pull_request.base.sha }}");
    expect(manifestGate).toContain("GH_REPO: ${{ github.repository }}");
    expect(manifestGate).toContain("dependabot/fetch-metadata@25dd0e34f4fe68f24cc83900b1fe3fe149efef98");
    expect(manifestGate).toContain("DEPENDENCY_NAMES: ${{ steps.metadata.outputs.dependency-names }}");
    expect(manifestGate).toContain('changed_files_json="$(gh api "repos/${GITHUB_REPOSITORY}/pulls/${PR_NUMBER}/files" --paginate)"');
    expect(manifestGate).toContain("printf '%s' \"${changed_files_json}\" | jq -r '.[] | .filename, (.previous_filename // empty)'");
    expect(manifestGate).toContain("No package manifest changes detected.");
    expect(manifestGate).toContain("Safe Dependabot manifest lane detected.");
    expect(manifestGate).toContain("issues/${PR_NUMBER}/timeline");
    expect(manifestGate).toContain('timeline_json="$(');
    // Label approval is validated by TIMELINE ORDER (server-side, trusted),
    // not by timestamps: commit author/committer dates are user-controlled.
    expect(manifestGate).toContain('label_state="$(');
    expect(manifestGate).toContain('.event == "labeled" and (.label.name // "") == $approval_label');
    expect(manifestGate).toContain('.event == "committed" or');
    expect(manifestGate).toContain('.event == "head_ref_force_pushed" or');
    expect(manifestGate).toContain('.event == "reopened" or');
    expect(manifestGate).toContain('.event == "ready_for_review" or');
    expect(manifestGate).toContain('(.event == "unlabeled" and (.label.name // "") == $approval_label)');
    expect(manifestGate).toContain("rindex(true)) as $label_idx");
    expect(manifestGate).toContain("rindex(true)) as $invalidating_idx");
    expect(manifestGate).toContain("$invalidating_idx == null or $label_idx > $invalidating_idx");
    expect(manifestGate).toContain('[[ "${approver_allowed}" == "true" ]] && [[ "${label_current}" == "true" ]]');
    expect(manifestGate).not.toContain(".created_at // .committer.date");
    expect(manifestGate).not.toContain('gh pr edit "${PR_NUMBER}" --remove-label "${manifest_label}"');
    expect(manifestGate).not.toContain('.event == "synchronize" or');
    expect(manifestGate).toContain("Package manifest changes require maintainer review. Add the label");
    expect(manifestGate).toContain("DEPENDENCY_GROUP: ${{ steps.metadata.outputs.dependency-group }}");
    expect(manifestGate).toContain("UPDATE_TYPE: ${{ steps.metadata.outputs.update-type }}");
    expect(manifestGate).not.toContain("actions/checkout");
  });

  it("documents the release-worthiness and Dependabot safe-lane policy in repo docs", () => {
    expect(releasing).toContain("## Release Worthiness");
    expect(releasing).toContain("Do not cut a new version just because `main` moved.");
    expect(releasing).toContain("## Dependabot Fast Lane");
    expect(releasing).toContain("Manifest-label rule:");
    expect(releasing).toContain("manifest-review:approved");
    expect(releasing).toContain("before `@codex`");
    expect(releasing).toContain("dev-only npm minor/patch updates that touch only `package.json` / `package-lock.json` (root or `nova/`)");
    expect(releasing).toContain("github-actions minor/patch bumps that change only `uses:` lines under `.github/workflows/`");
    expect(releasing).toContain("action majors and any workflow change beyond `uses:` version bumps stay manual");
    expect(releasing).toContain("safe lane excludes toolchain-risk dependencies such as `vitest`, `vite`, `typescript`, `tsx`, `rollup`, `rolldown`, and `esbuild`");
    expect(releasing).toContain("require `dependency-review` on `main`");
    expect(releasing).toContain("require `manifest-review-gate` on `main`");
    expect(releasing).toContain("`codex-review-gate` is advisory on `main`");
    expect(agents).toContain("Release-worthiness rule");
    expect(agents).toContain("Dependabot fast-lane rule");
    expect(agents).toContain("Toolchain-risk dev dependency rule");
    expect(agents).toContain("Codex advisory rule");
    expect(agents).toContain("Manifest-label rule");
    expect(agents).toContain("gh pr edit <nr> --add-label manifest-review:approved");
  });

  it("pins the expected main branch protection policy for maintainer verification", () => {
    expect(policy.main_branch_protection.required_status_checks).toEqual(["analyze", "ci-gate", "cloud-source-gate", "dependency-review", "manifest-review-gate", "readme-release-gate"]);
    expect(policy.main_branch_protection.required_status_check_apps).toEqual({
      "cloud-source-gate": 4400145,
    });
    expect(policy.main_branch_protection.required_approving_review_count).toBe(1);
    expect(policy.main_branch_protection.require_code_owner_reviews).toBe(true);
    expect(policy.main_branch_protection.dismiss_stale_reviews).toBe(true);
    expect(policy.main_branch_protection.required_conversation_resolution).toBe(true);
    expect(policy.main_branch_protection.strict_required_status_checks).toBe(true);
    expect(policy.main_branch_protection.advisory_checks).toEqual(["codex-review-gate"]);
    expect(policy.cloud_source_gate.check_name).toBe("cloud-source-gate");
    expect(policy.cloud_source_gate.reporter_app_slug).toBe("ha-nova-cloud-source-gate");
    expect(policy.cloud_source_gate.reporter_app_slug.length).toBeLessThanOrEqual(34);
    expect(policy.cloud_source_gate.reporter_app_id).toBe(4400145);
    expect(protectionScript).toContain("repo-policy.json");
    expect(protectionScript).toContain(".main_branch_protection.required_status_checks | sort");
    expect(protectionScript).toContain(".main_branch_protection.required_status_check_apps");
    expect(protectionScript).toContain(".required_status_checks.checks[]");
    expect(protectionScript).toContain(".main_branch_protection.advisory_checks[]?");
    expect(protectionScript).toContain("required_approving_review_count");
    expect(protectionScript).toContain("require_code_owner_reviews");
    expect(protectionScript).toContain("dismiss_stale_reviews");
    expect(protectionScript).toContain("required_conversation_resolution");
    expect(protectionScript).toContain("strict_required_status_checks");
  });

  it("pins production deployment refs and keeps only the existing safe actions lane", () => {
    expect(policy.production_environment).toEqual({
      name: "production",
      deployment_branch_policy: {
        protected_branches: false,
        custom_branch_policies: true,
      },
      deployment_branch_policies: [
        { name: "main", type: "branch" },
        { name: "v*", type: "tag" },
      ],
      protection_rule_types: ["branch_policy"],
    });
    expect(policy.cloud_source_gate.sensitive_workflows).toEqual([
      ".github/workflows/cloud-candidate-bundle.yml",
      ".github/workflows/cloud-source-gate.yml",
      ".github/workflows/ci.yml",
      ".github/workflows/release.yml",
      ".github/workflows/release-candidate.yml",
    ]);
    expect(productionEnvironmentScript).toContain('API_VERSION="2026-03-10"');
    expect(productionEnvironmentScript).toContain("deployment-branch-policies?per_page=100");
    expect(productionEnvironmentScript).toContain("{name, type}");
    expect(cloudUsesOnlyScript).toContain("may not add, delete, or rename workflows");
    expect(cloudUsesOnlyScript).toContain("is Cloud-release-sensitive");
    expect(cloudUsesOnlyScript).toContain("must be a forward minor/patch release update");
    expect(cloudUsesOnlyScript).toContain("action SHAs must match their canonical release tags");
  });
});
