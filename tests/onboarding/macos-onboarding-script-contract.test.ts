import { constants, mkdtempSync, mkdirSync, readFileSync, readlinkSync, statSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { createMockBinaries, mockEnv } from "./_helpers.js";

describe("macOS onboarding script contract", () => {
  it("provides executable onboarding script", () => {
    const file = "scripts/onboarding/macos-onboarding.sh";
    const stats = statSync(file);
    const content = readFileSync(file, "utf8");

    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
  });

  it("provides executable local skill installer script", () => {
    const multiInstaller = "scripts/onboarding/install-local-skills.sh";
    const multiInstallerStats = statSync(multiInstaller);
    const multiInstallerContent = readFileSync(multiInstaller, "utf8");

    expect((multiInstallerStats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(multiInstallerContent).toContain('SOURCE_SKILLS_DIR="${REPO_ROOT}/skills"');
    expect(multiInstallerContent).toContain("install_symlink");
    expect(multiInstallerContent).toContain("install_gemini_flat");
    expect(multiInstallerContent).toContain("LEGACY_FLAT_SKILLS");
    expect(multiInstallerContent).toContain("cleanup_legacy");
    expect(multiInstallerContent).toContain("GEMINI_SUB_SKILLS");
    expect(multiInstallerContent).toContain("copy_flat_skill_markdown");
    expect(multiInstallerContent).toContain("rewrite_flat_markdown");
    expect(multiInstallerContent).toContain("perl -0pe");
    expect(multiInstallerContent).toContain("find \"${dest_dir}\" -maxdepth 1 -type f -name '*.md'");
  });

  it("provides executable split onboarding command scripts", () => {
    const scripts = [
      "scripts/onboarding/macos-lib.sh",
      "scripts/onboarding/macos-setup.sh",
      "scripts/onboarding/macos-doctor.sh",
      "scripts/onboarding/macos-ready.sh",
      "scripts/onboarding/macos-quick.sh",
      "scripts/onboarding/macos-env.sh",
      "scripts/onboarding/uninstall.sh"
    ];

    for (const file of scripts) {
      const stats = statSync(file);
      const content = readFileSync(file, "utf8");
      expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
      expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
    }
  });

  it("uses Keychain as primary secret storage", () => {
    const lib = readFileSync("scripts/onboarding/macos-lib.sh", "utf8");
    const platform = readFileSync("scripts/onboarding/platform/macos.sh", "utf8");

    expect(lib).toContain('HA_NOVA_PLATFORM_ID="$(detect_platform_id)"');
    expect(lib).toContain('source "${SCRIPT_DIR}/platform/${HA_NOVA_PLATFORM_ID}.sh"');
    expect(lib).not.toContain('source "${SCRIPT_DIR}/platform/macos.sh"');
    expect(platform).toContain("security add-generic-password");
    expect(platform).toContain("security find-generic-password");
    expect(platform).toContain("store_platform_secret()");
    expect(platform).toContain("read_platform_secret()");
    expect(platform).toContain("copy_to_clipboard()");
    expect(lib).toContain("ha-nova.relay-auth-token");
    expect(lib).not.toContain("LLAT_SERVICE");
    expect(lib).not.toContain('emit_export "HA_LLAT"');
  });

  it("supports setup, doctor, ready, env, and quick commands only", () => {
    const content = readFileSync("scripts/onboarding/macos-onboarding.sh", "utf8");

    expect(content).toContain(" setup");
    expect(content).toContain(" doctor");
    expect(content).toContain(" ready");
    expect(content).toContain(" env");
    expect(content).toContain(" quick");
    expect(content).not.toContain("start");
    expect(content).not.toContain("codex");
    expect(content).not.toContain("claude");
  });

  it("implements quick readiness gate for fresh Codex sessions", () => {
    const lib = readFileSync("scripts/onboarding/macos-lib.sh", "utf8");

    expect(lib).toContain("run_quick()");
    expect(lib).toContain("run_ready()");
    expect(lib).toContain("DOCTOR_CACHE_FILE");
    expect(lib).toContain("DOCTOR_CACHE_RELAY_TOKEN_FINGERPRINT");
    expect(lib).not.toContain("DOCTOR_CACHE_HA_LLAT_FINGERPRINT");
    expect(lib).toContain(".agents/skills/ha-nova");
    expect(lib).toContain("Codex skill install");
    expect(lib).toContain("Fresh Codex session prompt:");
  });

  it("contains no contributor bootstrap or SSH flow", () => {
    const lib = readFileSync("scripts/onboarding/macos-lib.sh", "utf8");
    const relay = readFileSync("scripts/onboarding/lib/relay.sh", "utf8");

    expect(lib).not.toContain('"bootstrap"');
    expect(lib).not.toContain("HA_SSH_KEY");
    expect(lib).not.toContain("dev:app:bootstrap");
    // homeassistant.local moved to lib/relay.sh (host detection)
    expect(relay).toContain("homeassistant.local");
  });

  it("auto-detects and validates Home Assistant host during setup", () => {
    const lib = readFileSync("scripts/onboarding/macos-lib.sh", "utf8");
    const relay = readFileSync("scripts/onboarding/lib/relay.sh", "utf8");

    // Probe functions live in lib/relay.sh
    expect(relay).toContain("probe_home_assistant_host");
    expect(relay).toContain("/api/discovery_info");
    expect(relay).toContain("Setup needs a Home Assistant address to continue");
    expect(relay).toContain("Try a different address");
    expect(relay).toContain("Continue anyway");
    expect(relay).toContain("probe_relay_health");
    expect(relay).toContain("/health");
    // Host validation error message in relay.sh
    expect(relay).toContain("Could not reach Home Assistant");
    // mDNS service discovery
    expect(relay).toContain("discover_ha_via_mdns");
    expect(relay).toContain("_home-assistant._tcp");
    // Setup-flow string still in macos-lib.sh
    expect(lib).toContain("Install NOVA Relay in Home Assistant");
  });

  it("reuses existing relay token and surfaces degraded WS diagnostics", () => {
    const content = readFileSync("scripts/onboarding/macos-lib.sh", "utf8");

    expect(content).toContain("leave empty to keep existing or auto-generate");
    expect(content).toContain("Using existing relay auth token from ${PLATFORM_SECRET_STORE_NAME}");
    expect(content).toContain("ha_ws_connected=false");
    expect(content).toContain("Home Assistant WebSocket is not connected yet");
    expect(content).toContain("Setup incomplete");
    expect(content).toContain('Home Assistant Access Token field ("ha_llat") lives in App options');
    expect(content).not.toContain("unset HA_LLAT");
  });

  it("fails fast on non-interactive setup input", () => {
    if (process.platform !== "darwin") {
      return;
    }

    // Use isolated HOME so smart resume doesn't find existing state and exit early.
    const workDir = mkdtempSync(join(tmpdir(), "ha-nova-noninteractive-"));

    const result = spawnSync("bash", ["scripts/onboarding/macos-onboarding.sh", "setup", "claude"], {
      cwd: process.cwd(),
      input: "n\n",
      encoding: "utf8",
      timeout: 15000,
      env: {
        ...process.env,
        HOME: workDir
      }
    });

    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("Interactive input required. Re-run in a terminal.");
  });

  it("preserves HA URL port when continuing with unverified host", { timeout: 20000 }, () => {
    if (process.platform !== "darwin") {
      return;
    }

    const workDir = mkdtempSync(join(tmpdir(), "ha-nova-onboarding-"));
    const binDir = join(workDir, "bin");
    mkdirSync(binDir);

    const securityPath = join(binDir, "security");
    writeFileSync(
      securityPath,
      "#!/usr/bin/env bash\nexit 0\n",
      { encoding: "utf8", mode: 0o755 }
    );

    const curlPath = join(binDir, "curl");
    writeFileSync(
      curlPath,
      `#!/usr/bin/env bash
outfile=""
headers_file=""
write_code="0"
while [[ "$#" -gt 1 ]]; do
  case "$1" in
    -o) outfile="$2"; shift 2 ;;
    -D) headers_file="$2"; shift 2 ;;
    -w) write_code="1"; shift 2 ;;
    *) shift ;;
  esac
done
url="\${@: -1}"
if [[ "$url" == *"/health" ]]; then
  if [[ -n "$outfile" ]]; then
    printf '{"status":"ok","ha_ws_connected":true}' > "$outfile"
  else
    printf '{"status":"ok","ha_ws_connected":true}'
  fi
  if [[ -n "$headers_file" ]]; then
    printf 'content-type: application/json\\n' > "$headers_file"
  fi
  if [[ "$write_code" == "1" ]]; then
    printf '200'
  fi
  exit 0
fi
exit 1
`,
      { encoding: "utf8", mode: 0o755 }
    );

    const opensslPath = join(binDir, "openssl");
    writeFileSync(
      opensslPath,
      "#!/usr/bin/env bash\necho deadbeefdeadbeefdeadbeefdeadbeef\n",
      { encoding: "utf8", mode: 0o755 }
    );

    const claudePath = join(binDir, "claude");
    writeFileSync(
      claudePath,
      "#!/usr/bin/env bash\nexit 0\n",
      { encoding: "utf8", mode: 0o755 }
    );

    const input = [
      "192.168.1.5:18123",
      "n",
      "y",
      "",
      "",
      "y",
      ""
    ].join("\n");

    const result = spawnSync("bash", ["scripts/onboarding/macos-onboarding.sh", "setup", "claude"], {
      cwd: process.cwd(),
      input,
      encoding: "utf8",
      timeout: 20000,
      env: {
        ...process.env,
        HOME: workDir,
        PATH: `${binDir}:${process.env.PATH ?? ""}`
      }
    });

    expect(result.status).toBe(0);

    const config = readFileSync(join(workDir, ".config/ha-nova/onboarding.env"), "utf8");
    expect(config).toContain("HA_HOST=192.168.1.5");
    expect(config).toContain("HA_URL=http://192.168.1.5:18123");
  });

  it("installs skills: codex/opencode as symlinks, gemini as flat copies, claude via plugin CLI", () => {
    const workDir = mkdtempSync(join(tmpdir(), "ha-nova-skill-install-"));
    const repoRoot = process.cwd();
    const binDir = createMockBinaries();

    const result = spawnSync("bash", ["scripts/onboarding/install-local-skills.sh", "all"], {
      cwd: repoRoot,
      encoding: "utf8",
      timeout: 20000,
      env: mockEnv(workDir, binDir),
    });

    expect(result.status).toBe(0);

    // Codex: symlink at ~/.agents/skills/ha-nova -> repo/skills
    const codexLink = join(workDir, ".agents/skills/ha-nova");
    const codexLinkTarget = readlinkSync(codexLink);
    expect(codexLinkTarget).toBe(join(repoRoot, "skills"));

    // Codex symlink provides all sub-skills (readable through symlink)
    const subSkills = [
      "write",
      "read",
      "entity-discovery",
      "onboarding",
      "service-call",
      "review",
    ];
    for (const sub of subSkills) {
      const skillFile = join(codexLink, sub, "SKILL.md");
      const content = readFileSync(skillFile, "utf8");
      expect(content).toContain(`name: ${sub}`);
      expect(content).not.toContain("__HA_NOVA_REPO_ROOT__");
      expect(content).not.toContain("ha-nova-managed-install");
    }
    // Context skill also accessible
    const contextContent = readFileSync(join(codexLink, "ha-nova", "SKILL.md"), "utf8");
    expect(contextContent).toContain("ha-nova:write");

    // OpenCode: symlink at ~/.config/opencode/skills/ha-nova -> repo/skills
    const openCodeLink = join(workDir, ".config/opencode/skills/ha-nova");
    const openCodeLinkTarget = readlinkSync(openCodeLink);
    expect(openCodeLinkTarget).toBe(join(repoRoot, "skills"));

    // Gemini: flat copies at ~/.gemini/skills/ha-nova-{sub}/SKILL.md
    for (const sub of subSkills) {
      const flatSkill = join(workDir, ".gemini/skills", `ha-nova-${sub}`, "SKILL.md");
      const content = readFileSync(flatSkill, "utf8");
      expect(content).toContain(`name: ${sub}`);
    }

    // Claude: uses plugin CLI (skipped in test env since `claude` not available)
    // No file-based skill install — plugin registration is handled by `claude` CLI
    expect(() => statSync(join(workDir, ".claude/skills/ha-nova"))).toThrow();
  });

  it("exposes dev-only shell shortcuts and a canonical contributor verify command", () => {
    const pkg = JSON.parse(readFileSync("package.json", "utf8")) as {
      scripts?: Record<string, string>;
    };

    expect(pkg.scripts?.["onboarding:macos"]).toBeUndefined();
    expect(pkg.scripts?.["onboarding:macos:ready"]).toBeUndefined();
    expect(pkg.scripts?.["onboarding:macos:quick"]).toBeUndefined();
    expect(pkg.scripts?.["dev:onboarding:macos"]).toBe(
      "bash scripts/onboarding/macos-onboarding.sh setup"
    );
    expect(pkg.scripts?.["dev:onboarding:macos:ready"]).toBe(
      "bash scripts/onboarding/macos-onboarding.sh ready"
    );
    expect(pkg.scripts?.["dev:onboarding:macos:quick"]).toBe(
      "bash scripts/onboarding/macos-onboarding.sh quick"
    );
    expect(pkg.scripts?.["dev:install:codex-skill"]).toBe(
      "bash scripts/onboarding/install-local-skills.sh codex"
    );
    expect(pkg.scripts?.["dev:install:claude-skill"]).toBe(
      "bash scripts/onboarding/install-local-skills.sh claude"
    );
    expect(pkg.scripts?.["dev:install:opencode-skill"]).toBe(
      "bash scripts/onboarding/install-local-skills.sh opencode"
    );
    expect(pkg.scripts?.["dev:install:gemini-skill"]).toBe(
      "bash scripts/onboarding/install-local-skills.sh gemini"
    );
    expect(pkg.scripts?.["dev:install:skills"]).toBe(
      "bash scripts/onboarding/install-local-skills.sh all"
    );
    expect(pkg.scripts?.["test:cli"]).toBe("cd cli && go test ./...");
    expect(pkg.scripts?.verify).toBe("npm run typecheck && npm test && npm run test:cli");
  });

  it("documents canonical Codex one-link install entrypoint", () => {
    const codexInstall = readFileSync(".codex/INSTALL.md", "utf8");
    const claudeInstall = readFileSync(".claude/INSTALL.md", "utf8");
    const geminiInstall = readFileSync(".gemini/INSTALL.md", "utf8");
    const openCodeInstall = readFileSync(".opencode/INSTALL.md", "utf8");
    const onboardingAlias = readFileSync(".codex/ONBOARDING.md", "utf8");
    const routerSkill = readFileSync("skills/ha-nova/SKILL.md", "utf8");

    // Simplified INSTALL.md: installer-first quick start with global ha-nova follow-up
    expect(codexInstall).toContain("## Quick Start");
    expect(codexInstall).toContain("curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/install.sh | bash");
    expect(codexInstall).toContain("ha-nova doctor");
    expect(codexInstall).toContain("ha-nova update");
    expect(codexInstall).toContain("ha-nova uninstall");
    expect(codexInstall).toContain("legacy-uninstall");
    expect(codexInstall).not.toContain("npm run onboarding:macos:start");

    expect(claudeInstall).toContain("## Quick Start");
    expect(claudeInstall).toContain("curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/install.sh | bash");
    expect(claudeInstall).toContain("ha-nova doctor");
    expect(claudeInstall).toContain("ha-nova update");
    expect(claudeInstall).toContain("ha-nova uninstall");
    expect(claudeInstall).toContain("legacy-uninstall");

    expect(geminiInstall).toContain("ha-nova doctor");
    expect(geminiInstall).toContain("ha-nova update");
    expect(geminiInstall).toContain("ha-nova uninstall");
    expect(geminiInstall).toContain("legacy-uninstall");

    expect(openCodeInstall).toContain("ha-nova doctor");
    expect(openCodeInstall).toContain("ha-nova update");
    expect(openCodeInstall).toContain("ha-nova uninstall");
    expect(openCodeInstall).toContain("legacy-uninstall");

    expect(onboardingAlias).toContain("/.codex/INSTALL.md");
    expect(routerSkill).toContain("name: ha-nova");
    expect(routerSkill).not.toContain("__HA_NOVA_REPO_ROOT__");
    expect(routerSkill).toContain("Do not ask user to paste tokens in chat.");
  });

  it("provides platform-independent UI and relay libraries", () => {
    const uiFile = "scripts/onboarding/lib/ui.sh";
    const relayFile = "scripts/onboarding/lib/relay.sh";

    const uiStats = statSync(uiFile);
    const relayStats = statSync(relayFile);
    const uiContent = readFileSync(uiFile, "utf8");
    const relayContent = readFileSync(relayFile, "utf8");

    expect((uiStats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect((relayStats.mode & constants.S_IXUSR) !== 0).toBe(true);

    // UI helpers
    expect(uiContent).toContain("prompt_with_default()");
    expect(uiContent).toContain("prompt_yes_no()");
    expect(uiContent).toContain("print_step()");
    expect(uiContent).toContain("print_success()");
    expect(uiContent).toContain("print_fail()");
    expect(uiContent).toContain("copy_secret_to_clipboard()");
    expect(uiContent).not.toContain("security ");
    expect(uiContent).not.toContain("pbcopy");

    // Relay probes
    expect(relayContent).toContain("probe_relay_health()");
    expect(relayContent).toContain("probe_relay_ws_ping()");
    expect(relayContent).toContain("probe_home_assistant_url_base()");
    expect(relayContent).toContain("explain_relay_probe_failure()");
    expect(relayContent).not.toContain("security ");
  });

  it("provides bin/ha-nova CLI entry point for npx", () => {
    const file = "scripts/onboarding/bin/ha-nova";
    const stats = statSync(file);
    const content = readFileSync(file, "utf8");

    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
    expect(content).toContain("setup");
    expect(content).toContain("doctor");
    expect(content).toContain("uninstall");
    expect(content).toContain("Usage:");
  });

  it("uninstall removes skills and config", () => {
    const workDir = mkdtempSync(join(tmpdir(), "ha-nova-uninstall-"));
    const skillDirs = [
      join(workDir, ".agents/skills"),
      join(workDir, ".gemini/skills"),
      join(workDir, ".claude/skills"),
      join(workDir, ".config/opencode/skills"),
    ];
    const localShareDir = join(workDir, ".local/share/ha-nova");
    const localBinDir = join(workDir, ".local/bin");
    const localBinLink = join(localBinDir, "ha-nova");

    // Seed nested skill structure + legacy flat dirs + Gemini flat dirs
    for (const dir of skillDirs) {
      // Nested structure (current — could be symlink or copy)
      for (const sub of ["", "write", "read", "entity-discovery", "onboarding", "service-call", "review", "helper", "fallback"]) {
        const subDir = sub ? join(dir, "ha-nova", sub) : join(dir, "ha-nova");
        mkdirSync(subDir, { recursive: true });
        writeFileSync(join(subDir, "SKILL.md"), "# skill", "utf8");
      }
      // Legacy flat dirs
      for (const legacy of ["ha-nova-write", "ha-nova-read", "ha-nova-helper", "ha-nova-fallback"]) {
        mkdirSync(join(dir, legacy), { recursive: true });
        writeFileSync(join(dir, legacy, "SKILL.md"), "# skill", "utf8");
      }
      // Gemini flat dirs
      for (const gemini of ["ha-nova-review", "ha-nova-entity-discovery"]) {
        mkdirSync(join(dir, gemini), { recursive: true });
        writeFileSync(join(dir, gemini, "SKILL.md"), "# skill", "utf8");
      }
      writeFileSync(join(dir, "ha-nova-review", "checks.md"), "# checks", "utf8");
    }
    const configDir = join(workDir, ".config/ha-nova");
    mkdirSync(configDir, { recursive: true });
    writeFileSync(join(configDir, "relay"), "#!/bin/bash", "utf8");
    writeFileSync(join(configDir, "onboarding.env"), "HA_HOST=test", "utf8");
    mkdirSync(join(localShareDir, "scripts/onboarding/bin"), { recursive: true });
    writeFileSync(join(localShareDir, "scripts/onboarding/bin/ha-nova"), "#!/bin/bash", "utf8");
    mkdirSync(localBinDir, { recursive: true });
    writeFileSync(
      localBinLink,
      `#!/usr/bin/env bash
exec go run "${process.cwd()}/cli" uninstall "$@"
`,
      { mode: 0o755 },
    );

    const result = spawnSync("go", ["run", ".", "uninstall", "--yes"], {
      cwd: join(process.cwd(), "cli"),
      encoding: "utf8",
      timeout: 10000,
      env: { ...process.env, HOME: workDir },
    });

    expect(result.status).toBe(0);

    // Nested ha-nova/ directory removed
    for (const dir of skillDirs) {
      expect(() => statSync(join(dir, "ha-nova"))).toThrow();
      // Legacy flat dirs also removed
      expect(() => statSync(join(dir, "ha-nova-write"))).toThrow();
      expect(() => statSync(join(dir, "ha-nova-read"))).toThrow();
      // Gemini flat dirs also removed
      expect(() => statSync(join(dir, "ha-nova-review"))).toThrow();
      expect(() => statSync(join(dir, "ha-nova-entity-discovery"))).toThrow();
    }
    // Config removed
    expect(() => statSync(join(configDir, "relay"))).toThrow();
    expect(() => statSync(join(configDir, "onboarding.env"))).toThrow();
    expect(() => statSync(localShareDir)).toThrow();
    expect(() => statSync(localBinLink)).toThrow();
  });

  it("package.json does not expose shell wrapper as a public bin entry", () => {
    const pkg = JSON.parse(readFileSync("package.json", "utf8")) as {
      bin?: Record<string, string>;
    };
    expect(pkg.bin?.["ha-nova"]).toBeUndefined();
  });

  it("wizard includes prerequisites check, app guide, and skill install", () => {
    const lib = readFileSync("scripts/onboarding/macos-lib.sh", "utf8");

    // Phase 1: prerequisites
    expect(lib).toContain("check_prerequisites");

    // Phase 2: app installation guide with deep-link
    expect(lib).toContain("my.home-assistant.io/redirect/supervisor_add_addon_repository");
    expect(lib).toContain("open_browser");

    // Phase 3: token setup with LLAT guide
    expect(lib).toContain("profile/security");

    // Phase 4: automatic skill installation
    expect(lib).toContain("install-local-skills.sh");
  });

  it("implements smart resume that skips completed setup phases", () => {
    const lib = readFileSync("scripts/onboarding/macos-lib.sh", "utf8");

    // State detection function
    expect(lib).toContain("detect_setup_state()");
    expect(lib).toContain("SETUP_HAS_CONFIG");
    expect(lib).toContain("SETUP_HAS_TOKEN");
    expect(lib).toContain("SETUP_RELAY_OK");
    expect(lib).toContain("SETUP_WS_OK");
    expect(lib).toContain("SETUP_SKILLS_OK");

    // Status display
    expect(lib).toContain("print_setup_status()");
    expect(lib).toContain("Checking current setup...");
    expect(lib).toContain("Relay reachable");
    expect(lib).toContain("Authentication valid");
    expect(lib).toContain("WebSocket connected");
    expect(lib).toContain("Skills installed");

    // Early exit when fully set up
    expect(lib).toContain("Everything is already set up!");
    expect(lib).toContain("ha-nova doctor");

    // Skip flags
    expect(lib).toContain("skip_app_install");
    expect(lib).toContain("skip_relay_token");
    expect(lib).toContain("skip_llat");
    expect(lib).toContain("skip_verify");
    expect(lib).toContain("skip_skills");

    // Skip summary message
    expect(lib).toContain("Already done:");

    // Sub-skill names list for state detection
    expect(lib).toContain("HA_NOVA_SUB_SKILLS=(");
  });

  it("prerequisites check validates platform and curl", () => {
    const ui = readFileSync("scripts/onboarding/lib/ui.sh", "utf8");

    expect(ui).toContain("check_prerequisites()");
    expect(ui).toContain("require_platform");
    expect(ui).not.toContain("node --version");
  });

  it("supports --host and --token CLI flags for non-interactive setup", () => {
    const cli = readFileSync("scripts/onboarding/bin/ha-nova", "utf8");
    const lib = readFileSync("scripts/onboarding/macos-lib.sh", "utf8");

    expect(cli).toContain('exec_runtime setup "$@"');

    expect(lib).toContain("HA_NOVA_HOST");
    expect(lib).toContain("HA_NOVA_TOKEN");
    expect(lib).toContain("flag_host");
    expect(lib).toContain("flag_token");
    expect(lib).toContain("non_interactive_verify");
  });

  it("supports Go-first update and check-update handoff", () => {
    const cli = readFileSync("scripts/onboarding/bin/ha-nova", "utf8");
    expect(cli).toContain("update)");
    expect(cli).toContain("check-update)");
    expect(cli).toContain("find_runtime_binary");
    expect(cli).toContain('exec "${runtime_bin}" update');
    expect(cli).toContain('exec "${runtime_bin}" check-update');
  });

  it("provides platform-specific macOS module", () => {
    const file = "scripts/onboarding/platform/macos.sh";
    const stats = statSync(file);
    const content = readFileSync(file, "utf8");

    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
    expect(content).toContain("require_platform()");
    expect(content).toContain("store_platform_secret()");
    expect(content).toContain("read_platform_secret()");
    expect(content).toContain("delete_platform_secret_if_exists()");
    expect(content).toContain("store_keychain_secret()");
    expect(content).toContain("open_browser()");
    expect(content).toContain("copy_to_clipboard()");
    expect(content).toContain('printf \'%s\' "$1" | pbcopy');
    expect(content).toContain("security add-generic-password");
    expect(content).toContain("security find-generic-password");
  });
});
