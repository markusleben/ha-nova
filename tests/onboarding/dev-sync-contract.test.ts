import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("dev-sync contract", () => {
  const content = readFileSync("scripts/dev-sync.sh", "utf8");
  const claudeLib = readFileSync("scripts/onboarding/lib/install-local-skills-claude.sh", "utf8");

  it("delegates file clients back to install-local-skills.sh", () => {
    expect(content).toContain('bash "${REPO_ROOT}/scripts/onboarding/install-local-skills.sh" "$target"');
    expect(content).toContain('refresh_file_client "$name" "$target"');
    expect(content).toContain('refresh_file_client "Gemini" "gemini"');
    expect(content).toContain('CURRENT_PLATFORM_ID="$(detect_platform_id)"');
    expect(content).toContain("sync_hermes()");
    expect(content).toContain('sync_file_client "Hermes Agent" "${HOME}/.hermes/skills/ha-nova" "hermes"');
    expect(content).toContain("native Windows sync not supported");
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

  it("syncs Hermes only on POSIX-style installs", () => {
    expect(content).toContain('CURRENT_PLATFORM_ID="$(detect_platform_id)"');
    expect(content).toContain('sync_hermes()');
    expect(content).toContain('sync_file_client "Hermes Agent" "${HOME}/.hermes/skills/ha-nova" "hermes"');
    expect(content).toContain("native Windows sync not supported");
    expect(content).toContain("WSL2/Linux HA NOVA install");
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

  it("rebuilds the local Go CLI onto the runtime ha-nova for lockstep dev testing", () => {
    expect(content).toContain("sync_cli_runtime()");
    expect(content).toContain("stamp_dev_sync_state()");
    expect(content).toContain("dev_runtime_target()");
    expect(content).toContain('go build -ldflags "$(dev_build_ldflags)" -o "${target}"');
    // Guarded to a runtime under the current HOME so test sandboxes never build.
    expect(content).toContain('"${HOME}"/*) ;;');
    // Never rebuild onto a tracked in-repo helper shim that happens to be on PATH:
    // dev_runtime_target rejects any candidate inside the repo working tree.
    expect(content).toContain("path_within_repo()");
    expect(content).toContain('if ! path_within_repo "${resolved}"; then');
    // The dev guard refuses a plain `ha-nova update`, so the hint must name --force.
    expect(content).toContain("restore the release with 'ha-nova update --force'");
    expect(content).not.toContain("restore the release with 'ha-nova update'\"");
    // A failed CLI build must fail the whole sync, not silently leave a stale
    // runtime behind refreshed skills that call new ha-nova subcommands. The
    // missing-Go / missing-cli branch is the same stale-runtime gap, so it must set
    // the failure too whenever clients were already synced.
    expect(content).toContain("cli_build_failed=1");
    expect(content).toContain('if [[ "${cli_build_failed}" -eq 1 ]]; then');
    expect(content).toContain('if [[ "${#synced[@]}" -gt 0 ]]; then');
    // Wired into the main flow right after the Claude sync.
    expect(content).toContain("sync_claude\nsync_cli_runtime\n");
  });

  it("stamps the dev build channel into the CLI so `ha-nova version` self-reports", () => {
    // The build identity lives in the shared CLI (via ldflags), not in skill
    // files — so it survives in symlink clients (Codex) and never pollutes the
    // committed skill source. Released builds omit these flags -> bare version.
    expect(content).toContain("dev_build_ldflags()");
    expect(content).toContain("-X main.Version=");
    expect(content).toContain("-X main.BuildChannel=dev");
    expect(content).toContain("-X main.BuildStamp=");
    expect(content).toContain('cp "${REPO_ROOT}/version.json" "$(dirname "${target}")/version.json"');
    expect(content).toContain('[[ -f "${state_file}" ]] || return 0');
    expect(content).toContain("node -e 'process.exit(0)' >/dev/null 2>&1 || return 0");
    expect(content).toContain("state.clients_verified_version = version");
    // The stamp has a SINGLE owner: sync_cli_runtime, which builds the runtime
    // binary `ha-nova` actually resolves to. The shared-tools relay build stays
    // plain (in repo-dev installs relay_dst is a wrapper this would clobber).
    expect(content).toContain('go build -ldflags "$(dev_build_ldflags)" -o "${target}"');
    expect(content).not.toContain('go build -ldflags "$(dev_build_ldflags)" -o "${relay_dst}"');
    expect(content).toContain('go build -o "${relay_dst}"');
    // The fragile in-file skill stamp is retired (it was invisible in symlink
    // clients and risked writing back into the repo).
    expect(content).not.toContain("stamp_dev_build_marker");
    expect(content).not.toContain("HA-NOVA-DEV-BUILD");
    // Marketplace source sync stays: keeps dev skills alive across a Claude restart.
    expect(content).toContain("claude_marketplace_source_dir()");
    // The parser must read string-form sources too (the Go reader + fixtures use
    // them); object-only parsing skipped the rsync and a restart clobbered dev skills.
    expect(content).toContain('typeof src === "string"');
    expect(content).toContain('"${mkt_src}" == "${HOME}"/*');
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
