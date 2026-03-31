import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("dependabot automation contract", () => {
  const agents = readFileSync("AGENTS.md", "utf8");
  const codeowners = readFileSync(".github/CODEOWNERS", "utf8");
  const dependabot = readFileSync(".github/dependabot.yml", "utf8");
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
    };
    main_branch_protection: {
      required_status_checks: string[];
      required_approving_review_count: number;
      require_code_owner_reviews: boolean;
      required_conversation_resolution: boolean;
      strict_required_status_checks: boolean;
      advisory_checks: string[];
    };
  };
  const protectionScript = readFileSync("scripts/release/verify-github-main-protection.sh", "utf8");
  const releasing = readFileSync("docs/releasing.md", "utf8");
  const workflow = readFileSync(".github/workflows/dependabot-safe-auto-merge.yml", "utf8");

  it("keeps the safe dev-only npm lane in a dedicated Dependabot group", () => {
    expect(policy.manifest_review.label).toBe("manifest-review:approved");
    expect(policy.manifest_review.approvers).toEqual(["markusleben"]);
    expect(policy.manifest_review.manifest_sensitive_files).toEqual([
      "package.json",
      "package-lock.json",
      "nova/package.json",
      "nova/package-lock.json",
    ]);
    expect(policy.dependabot_safe_lane.label).toBe("dependabot-safe:auto-merge");
    expect(policy.dependabot_safe_lane.allowed_package_ecosystems).toEqual(["npm", "npm_and_yarn"]);
    expect(policy.dependabot_safe_lane.dependency_group).toBe("npm-dev-minor-patch");
    expect(policy.dependabot_safe_lane.allowed_update_types).toEqual([
      "version-update:semver-minor",
      "version-update:semver-patch",
    ]);
    expect(policy.dependabot_safe_lane.manual_review_dependencies).toEqual([
      "vitest",
      "vite",
      "typescript",
      "tsx",
      "rollup",
      "rolldown",
      "esbuild",
    ]);
    expect(dependabot).toContain("package-ecosystem: npm");
    expect(dependabot).toContain('directory: "/"');
    expect(dependabot).toContain('directory: "/nova"');
    expect(dependabot).toContain("npm-dev-minor-patch:");
    expect(dependabot).toContain("dependency-type: development");
    expect(dependabot).toContain("update-types:");
    expect(dependabot).toContain("- minor");
    expect(dependabot).toContain("- patch");
    expect(dependabot).not.toContain("npm-minor-patch:");
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
  });

  it("only auto-approves and auto-merges the safe Dependabot manifest lane without checking out PR code", () => {
    expect(workflow).toContain("pull_request_target:");
    expect(workflow).toContain("workflow_run:");
    expect(workflow).toContain("github.event.workflow_run.conclusion == 'success'");
    expect(workflow).toContain('POLICY_REF: ${{ github.event.pull_request.base.sha }}');
    expect(workflow).toContain('POLICY_REF: ${{ github.event.repository.default_branch }}');
    expect(workflow).not.toContain("github.event.workflow_run.event == 'pull_request'");
    expect(workflow).toContain("dependabot[bot]");
    expect(workflow).toContain("dependabot/fetch-metadata@21025c705c08248db411dc16f3619e6b5f9ea21a");
    expect(workflow).toContain('DEPENDENCY_NAMES: ${{ steps.metadata.outputs.dependency-names }}');
    expect(workflow).toContain('PACKAGE_ECOSYSTEM: ${{ steps.metadata.outputs.package-ecosystem }}');
    expect(workflow).toContain('DEPENDENCY_GROUP: ${{ steps.metadata.outputs.dependency-group }}');
    expect(workflow).toContain('UPDATE_TYPE: ${{ steps.metadata.outputs.update-type }}');
    expect(workflow).toContain("safe-label=${safe_label}");
    expect(workflow).toContain("policy-sha=${policy_sha}");
    expect(workflow).toContain("SAFE_POLICY_MARKER");
    expect(workflow).toContain("policy_sha=${POLICY_SHA}");
    expect(workflow).toContain("recorded_policy_sha");
    expect(workflow).toContain('GH_REPO: ${{ github.repository }}');
    expect(workflow).toContain('dependency requires manual review due to toolchain risk');
    expect(workflow).toContain("gh pr review --approve");
    expect(workflow).toContain("--json autoMergeRequest,labels");
    expect(workflow).toContain('jq -e \'.autoMergeRequest != null\'');
    expect(workflow).toContain('gh pr merge "${PR_NUMBER}" --disable-auto');
    expect(workflow).toContain('jq -e --arg label "${SAFE_LABEL}" \'.labels[]? | select(.name == $label)\'');
    expect(workflow).toContain('gh pr edit "${PR_NUMBER}" --remove-label "${SAFE_LABEL}"');
    expect(workflow).toContain('issues/${pr_number}/timeline');
    expect(workflow).toContain('timeline_json="$(');
    expect(workflow).toContain('if [[ "${label_actor}" != "github-actions[bot]" ]]; then');
    expect(workflow).toContain('for required_check in "${required_checks[@]}"; do');
    expect(workflow).toContain('Policy fingerprint drifted for PR #${pr_number}; removing safe label.');
    expect(workflow).toContain('gh pr merge "${pr_number}" --auto --squash');
    expect(workflow).not.toContain("actions/checkout");
  });

  it("blocks non-safe manifest changes unless a maintainer labels them approved", () => {
    expect(manifestGate).toContain("pull_request_target:");
    expect(manifestGate).toContain('POLICY_REF: ${{ github.event.pull_request.base.sha }}');
    expect(manifestGate).toContain('GH_REPO: ${{ github.repository }}');
    expect(manifestGate).toContain("dependabot/fetch-metadata@21025c705c08248db411dc16f3619e6b5f9ea21a");
    expect(manifestGate).toContain('DEPENDENCY_NAMES: ${{ steps.metadata.outputs.dependency-names }}');
    expect(manifestGate).toContain('changed_files_json="$(gh api "repos/${GITHUB_REPOSITORY}/pulls/${PR_NUMBER}/files" --paginate)"');
    expect(manifestGate).toContain("printf '%s' \"${changed_files_json}\" | jq -r '.[].filename'");
    expect(manifestGate).toContain("No package manifest changes detected.");
    expect(manifestGate).toContain("Safe Dependabot manifest lane detected.");
    expect(manifestGate).toContain("issues/${PR_NUMBER}/timeline");
    expect(manifestGate).toContain('timeline_json="$(');
    expect(manifestGate).toContain('approval_labeled_at="$(');
    expect(manifestGate).toContain('latest_invalidating_at="$(');
    expect(manifestGate).toContain('select(.event == "labeled" and .label.name == $label)');
    expect(manifestGate).toContain('.event == "committed" or');
    expect(manifestGate).toContain('.event == "head_ref_force_pushed" or');
    expect(manifestGate).toContain('.event == "reopened" or');
    expect(manifestGate).toContain('.event == "ready_for_review" or');
    expect(manifestGate).toContain('(.event == "unlabeled" and .label.name == $label)');
    expect(manifestGate).toContain('[[ -z "${latest_invalidating_at}" || "${approval_labeled_at}" > "${latest_invalidating_at}" ]]');
    expect(manifestGate).not.toContain('gh pr edit "${PR_NUMBER}" --remove-label "${manifest_label}"');
    expect(manifestGate).not.toContain('.event == "synchronize" or');
    expect(manifestGate).toContain("Package manifest changes require maintainer review. Add the label");
    expect(manifestGate).toContain('DEPENDENCY_GROUP: ${{ steps.metadata.outputs.dependency-group }}');
    expect(manifestGate).toContain('UPDATE_TYPE: ${{ steps.metadata.outputs.update-type }}');
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
    expect(policy.main_branch_protection.required_status_checks).toEqual([
      "analyze",
      "ci-gate",
      "dependency-review",
      "manifest-review-gate",
    ]);
    expect(policy.main_branch_protection.required_approving_review_count).toBe(1);
    expect(policy.main_branch_protection.require_code_owner_reviews).toBe(true);
    expect(policy.main_branch_protection.required_conversation_resolution).toBe(true);
    expect(policy.main_branch_protection.strict_required_status_checks).toBe(false);
    expect(policy.main_branch_protection.advisory_checks).toEqual(["codex-review-gate"]);
    expect(protectionScript).toContain("repo-policy.json");
    expect(protectionScript).toContain(".main_branch_protection.required_status_checks | sort");
    expect(protectionScript).toContain(".main_branch_protection.advisory_checks[]?");
    expect(protectionScript).toContain("required_approving_review_count");
    expect(protectionScript).toContain("require_code_owner_reviews");
    expect(protectionScript).toContain("required_conversation_resolution");
    expect(protectionScript).toContain("strict_required_status_checks");
  });
});
