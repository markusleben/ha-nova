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
});
