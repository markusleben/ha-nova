import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("dev-sync contract", () => {
  const content = readFileSync("scripts/dev-sync.sh", "utf8");
  const claudeLib = readFileSync("scripts/onboarding/lib/install-local-skills-claude.sh", "utf8");

  it("delegates file clients back to install-local-skills.sh", () => {
    expect(content).toContain('bash "${REPO_ROOT}/scripts/onboarding/install-local-skills.sh" "$target"');
    expect(content).toContain('refresh_file_client "$name" "$target"');
    expect(content).toContain('refresh_file_client "Gemini" "gemini"');
  });

  it("keeps legacy Gemini marker support during migration", () => {
    expect(content).toContain('.gemini/skills/ha-nova-read/SKILL.md');
    expect(content).toContain('.agents/skills/ha-nova-read/SKILL.md');
  });

  it("requires symlink markers for Codex and OpenCode", () => {
    expect(content).toContain('file_client_install_present()');
    expect(content).toContain('[[ -d "${install_root}" && -f "${install_root}/ha-nova/SKILL.md" ]]');
    expect(content).toContain('sync_file_client "Codex"');
    expect(content).toContain('sync_file_client "OpenCode"');
  });

  it("generates the version-check wrapper directly instead of copying a tracked shell shim", () => {
    expect(content).toContain('write_repo_cli_wrapper "${config_dir}/version-check" "check-update" "--quiet"');
    expect(content).not.toContain('scripts/version-check.sh');
    expect(content).not.toContain('scripts/update.sh');
  });

  it("uses the dedicated Claude plugin state helper instead of inline shell JSON rewrites", () => {
    expect(content).toContain('. "${REPO_ROOT}/scripts/onboarding/lib/install-local-skills-claude.sh"');
    expect(content).toContain('CLAUDE_PLUGIN_STATE_TOOL="$(claude_plugin_state_tool)"');
    expect(claudeLib).toContain('node "$(claude_plugin_state_tool)" inspect-installed-plugin');
    expect(content).toContain('node "${CLAUDE_PLUGIN_STATE_TOOL}" repair-plugin-record');
    expect(content).not.toContain("inplace_sed()");
    expect(content).not.toContain('sed -i \'\' "$@"');
    expect(content).not.toContain('sed -i "$@"');
  });

  it("locks the new fail-loud repo invariant guards", () => {
    expect(content).toContain('missing repo skills directory');
    expect(content).toContain('missing repo version file');
    expect(content).toContain('missing repo helper runtime shim');
    expect(readFileSync("scripts/onboarding/install-local-skills.sh", "utf8")).toContain('Missing repo skills directory');
    expect(readFileSync("scripts/onboarding/install-local-skills.sh", "utf8")).toContain('Missing repo version file');
    expect(readFileSync("scripts/onboarding/install-local-skills.sh", "utf8")).toContain('Missing repo helper runtime shim');
  });
});
