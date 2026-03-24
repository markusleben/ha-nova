import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("dev-sync contract", () => {
  const content = readFileSync("scripts/dev-sync.sh", "utf8");

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
    expect(content).toContain('[[ -L "$link_path" && -e "$link_path" ]]');
    expect(content).toContain('sync_symlink_client "Codex"');
    expect(content).toContain('sync_symlink_client "OpenCode"');
  });

  it("generates the version-check wrapper directly instead of copying a tracked shell shim", () => {
    expect(content).toContain('write_repo_cli_wrapper "${config_dir}/version-check" "check-update" "--quiet"');
    expect(content).not.toContain('scripts/version-check.sh');
    expect(content).not.toContain('scripts/update.sh');
  });

  it("keeps Claude plugin record rewrites portable across BSD and GNU sed", () => {
    expect(content).toContain("inplace_sed()");
    expect(content).toContain('if [[ "$(uname -s)" == "Darwin" ]]');
    expect(content).toContain('sed -i \'\' "$@"');
    expect(content).toContain('sed -i "$@"');
    expect(content).not.toContain('sed -i \'\' "/"ha-nova@ha-nova"');
  });
});
