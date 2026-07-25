import { constants, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("desktop validation helpers contract", () => {
  const releasing = readFileSync("docs/releasing.md", "utf8");
  const packageJson = readFileSync("package.json", "utf8");
  const harness = readFileSync(
    "scripts/dev/start-local-validation-harness.sh",
    "utf8",
  );
  const macosSuite = readFileSync(
    "scripts/dev/macos-private-rc-suite.sh",
    "utf8",
  );
  const macosSmoke = readFileSync(
    "scripts/dev/macos-private-rc-smoke.sh",
    "utf8",
  );
  const macosSetupAll = readFileSync(
    "scripts/dev/macos-private-rc-setup-all.sh",
    "utf8",
  );
  const macosClient = readFileSync(
    "scripts/dev/macos-private-rc-client.sh",
    "utf8",
  );
  const linuxHeadless = readFileSync(
    "scripts/smoke/linux-headless-setup-check.sh",
    "utf8",
  );
  const validationCommon = readFileSync(
    "scripts/dev/lib/validation-common.sh",
    "utf8",
  );
  const mockServer = readFileSync("scripts/dev/mock-ha-relay.py", "utf8");

  it("ships executable macOS helper scripts", () => {
    for (const path of [
      "scripts/dev/macos-private-rc-suite.sh",
      "scripts/dev/macos-private-rc-smoke.sh",
      "scripts/dev/macos-private-rc-setup-all.sh",
      "scripts/dev/macos-private-rc-client.sh",
      "scripts/dev/start-local-validation-harness.sh",
      "scripts/smoke/linux-headless-setup-check.sh",
    ]) {
      const stats = statSync(path);
      expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    }
  });

  it("documents the private helpers plus the public Windows onboarding entrypoint", () => {
    expect(releasing).toContain("Desktop Validation Helpers");
    expect(releasing).toContain("scripts/dev/macos-private-rc-suite.sh");
    expect(releasing).toContain("scripts/dev/macos-private-rc-smoke.sh");
    expect(releasing).toContain("scripts/dev/macos-private-rc-setup-all.sh");
    expect(releasing).toContain("scripts/dev/macos-private-rc-client.sh");
    expect(releasing).toContain(
      "scripts/dev/start-local-validation-harness.sh",
    );
    expect(releasing).toContain("scripts/dev/mock-ha-relay.py");
    expect(releasing).toContain("macOS Public Onboarding Lane");
    expect(releasing).toContain("local interactive macOS Terminal session");
    expect(releasing).toContain(
      "run the printed macOS install command for your host architecture",
    );
    expect(releasing).toContain("do not set `HA_NOVA_NO_SETUP=1`");
    expect(releasing).toContain(
      "the same public `install.sh` flow is still valid when it installs HA NOVA locally",
    );
    expect(releasing).toContain(
      "scripts/dev/macos-private-rc-suite.sh` is the canonical technical start",
    );
    expect(releasing).toContain(
      "are leaf lanes; run them only after the suite",
    );
    expect(releasing).toContain(
      "they do not prove the public same-session `install.sh` setup handoff",
    );
    expect(releasing).toContain(
      "proves private standard-remove plus purge cleanup",
    );
    expect(releasing).toContain(
      "proves private same-version setup/doctor/update/uninstall lifecycle plus standard config/token retention",
    );
    expect(releasing).toContain(
      "`setup-all` is not a substitute for those client assertions",
    );
    expect(releasing).toContain("scripts/dev/windows-clean-test-state.ps1");
    expect(releasing).toContain("test:desktop:windows:headless");
    expect(releasing).toContain("test:desktop:windows:rdp");
    expect(releasing).toContain("test:desktop:windows:public");
    expect(releasing).toContain("scripts/dev/windows-private-rc-install.ps1");
    expect(releasing).toContain("scripts/dev/windows-desktop-setup.ps1");
    expect(releasing).toContain("scripts/dev/windows-public-onboarding.ps1");
    expect(releasing).toContain(
      "use the macOS/private Windows helpers only for private validation against local or RC bundles",
    );
    expect(releasing).toContain("private mechanics/lifecycle lane");
    expect(releasing).toContain(
      "background-complete, not same-console-complete",
    );
    expect(releasing).toContain(
      "do not run them against `main` or a public stable release without intent",
    );
    expect(releasing).toContain(
      "only helper in this group that targets the public end-user contract",
    );
    expect(releasing).toContain("execute the real installer inline");
    expect(releasing).toContain("Additional supported public outcome:");
    expect(releasing).toContain(
      "local-install-plus-missing-client-guidance path",
    );
    expect(releasing).toContain("standard per-user Desktop install");
    expect(releasing).toContain(
      "%LOCALAPPDATA%\\Programs\\Antigravity\\Antigravity.exe",
    );
    expect(releasing).toContain("Hermes does not count on native Windows");
    expect(releasing).toContain("file-based test keyring override");
    expect(releasing).toContain(
      "Windows Credential Manager interop stays covered",
    );
    expect(releasing).toContain("open a new shell and run `ha-nova doctor`");
    expect(releasing).toContain("Linux real-machine onboarding");
    expect(releasing).toContain("scripts/smoke/linux-headless-setup-check.sh");
    expect(releasing).toContain("npm run test:desktop:linux:antigravity");
    expect(releasing).toContain(
      "HA_NOVA_LIVE_SETUP_CMD='HA_NOVA_NO_BROWSER=1 ha-nova setup antigravity'",
    );
    expect(releasing).toContain(
      "real Linux host with a local graphical desktop session",
    );
    expect(releasing).toContain(
      "validate the fail-closed SSH/headless path separately",
    );
    expect(releasing).toContain(
      "explicit provider prerequisite message instead of raw `org.freedesktop.secrets` D-Bus text",
    );
    expect(releasing).toContain("provider-owned desktop prompt");
    expect(releasing).toContain(
      "CLI terminal never asks for, reads, or confirms the keyring master password",
    );
  });

  it("keeps the Linux headless helper explicit and privacy-safe", () => {
    expect(linuxHeadless).toContain("HA_NOVA_LIVE_SSH_HOST");
    expect(linuxHeadless).toContain("HA_NOVA_LIVE_INSTALL_CMD");
    expect(linuxHeadless).toContain("DBUS_SESSION_BUS_ADDRESS");
    expect(linuxHeadless).toContain(
      "org.freedesktop.Secret.Service.ReadAlias default",
    );
    expect(linuxHeadless).toContain("HA_NOVA_NO_BROWSER=1 ha-nova setup");
    expect(linuxHeadless).toContain("store hostnames, tokens, or passwords");
    expect(linuxHeadless).toContain("same logged-in desktop");
    expect(packageJson).toContain('"test:desktop:linux:antigravity"');
    expect(packageJson).toContain("ha-nova setup antigravity");
  });

  it("ships a single local validation harness entrypoint", () => {
    expect(packageJson).toContain('"dev:validation:harness"');
    expect(packageJson).toContain('"test:desktop:windows:public"');
    expect(harness).toContain("npm run release:rc:local");
    expect(harness).toContain("python3 -m http.server");
    expect(harness).toContain("mock-ha-relay.py");
    expect(harness).toContain("HA_NOVA_BUNDLE_URL");
    expect(harness).toContain("HA_NOVA_BUNDLE_SHA256_URL");
    expect(harness).toContain("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL");
    expect(harness).toContain("ha-nova-installer-bundle-macos-arm64.tar.gz");
    expect(harness).toContain("ha-nova-installer-bundle-macos-amd64.tar.gz");
    expect(harness).toContain("ha-nova-installer-bundle-windows-amd64.zip");
    expect(harness).toContain("install.ps1 | iex");
    expect(harness).toContain("--with-mock");
    expect(harness).toContain("ensure_port_free");
    expect(harness).toContain("bundle_reported_version");
    expect(harness).toContain("port ${port} is already in use");
    expect(harness).toContain("Harness asset missing or not reachable");
    expect(harness).not.toContain("winget");
    expect(harness).not.toContain("$ProgressPreference");
  });

  it("keeps the macOS suite self-refreshing and self-contained", () => {
    expect(macosSuite).toContain("npm run release:rc:local");
    expect(macosSuite).toContain("python3 -m http.server");
    expect(macosSuite).toContain("wait_for_server()");
    expect(macosSuite).toContain("BUNDLE_SERVER_BASE_URL");
    expect(macosSuite).toContain("ensure_port_free");
    expect(macosSuite).toContain("port ${port} is already in use");
    expect(macosSuite).toContain("macos-private-rc-smoke.sh");
    expect(macosSuite).toContain("macos-private-rc-setup-all.sh");
    expect(macosSuite).toContain("macos-private-rc-client.sh claude");
    expect(macosSuite).toContain("macos-private-rc-client.sh hermes");
  });

  it("keeps the mock server intentionally tiny and dependency-free", () => {
    expect(mockServer).toContain(
      "from http.server import BaseHTTPRequestHandler, HTTPServer",
    );
    expect(mockServer).toContain('if self.path == "/health"');
    expect(mockServer).toContain('"ha_ws_connected": True');
    expect(mockServer).toContain('self.wfile.write(b"OK")');
    expect(mockServer).toContain("--reported-version");
    expect(mockServer).toContain("reported version");
    expect(mockServer).toContain("fake relay /health");
  });

  it("runs the macOS setup-all lane against private bundles and same-version update", () => {
    expect(macosSetupAll).toContain("HA_NOVA_BUNDLE_URL");
    expect(macosSetupAll).toContain("HA_NOVA_BUNDLE_SHA256_URL");
    expect(macosSetupAll).toContain("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1");
    expect(macosSetupAll).toContain("HA_NOVA_NO_SETUP=1");
    expect(macosSetupAll).toContain("HA_NOVA_NO_BROWSER=1");
    expect(macosSetupAll).toContain("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING=1");
    expect(macosSetupAll).toContain("HA_NOVA_TEST_KEYRING_FILE");
    expect(macosSetupAll).toContain("HA_NOVA_KEYRING_SERVICE");
    expect(macosSetupAll).toContain("MOCK_REPORTED_VERSION");
    expect(macosSetupAll).toContain("require_bundle_assets_ready");
    expect(macosSetupAll).toContain("wait_for_mock_server()");
    expect(macosSetupAll).toContain('curl -fsS "${ha_url}"');
    expect(macosSetupAll).toContain("mock-server.log");
    expect(macosSetupAll).toContain(
      'runtime_bin="${TMP_HOME}/.local/bin/ha-nova"',
    );
    expect(macosSetupAll).toContain('"${runtime_bin}" setup all');
    expect(macosSetupAll).toContain(
      '--relay-url "http://127.0.0.1:${MOCK_RELAY_PORT}"',
    );
    expect(macosSetupAll).toContain(
      'same_version="$("${runtime_bin}" version)"',
    );
    expect(macosSetupAll).toContain(
      '"${runtime_bin}" update --version "${same_version}"',
    );
    expect(macosSetupAll).toContain('"${runtime_bin}" uninstall --yes');
    expect(macosSetupAll).toContain(
      'test -e "${TMP_HOME}/.config/ha-nova/config.json"',
    );
    expect(macosSetupAll).toContain(
      'test ! -e "${TMP_HOME}/.config/ha-nova/state.json"',
    );
    expect(macosSetupAll).toContain('test ! -e "${TMP_HOME}/.cache/ha-nova"');
    expect(macosSetupAll).toContain(
      'token_file="${HA_NOVA_TEST_KEYRING_FILE}"',
    );
    expect(macosSetupAll).toContain('test -e "${token_file}"');
    expect(macosSetupAll).toContain(
      'test "$(tr -d \'\\r\\n\' < "${token_file}")" = "${RELAY_TOKEN}"',
    );
    expect(macosSetupAll).toContain(
      'test ! -e "${TMP_HOME}/.local/bin/ha-nova"',
    );
    expect(validationCommon).toContain("bundle_reported_version");
    expect(validationCommon).toContain(
      "Set MOCK_REPORTED_VERSION explicitly when overriding the bundle source.",
    );
    expect(validationCommon).toContain("require_bundle_assets_ready()");
    expect(validationCommon).toContain(
      "Run 'npm run test:desktop:macos' first",
    );
  });

  it("keeps the macOS per-client lane explicit about expected client artifacts", () => {
    expect(macosClient).toContain(
      "Usage: $0 <claude|codex|opencode|antigravity|hermes>",
    );
    expect(macosClient).toContain("HA_NOVA_KEYRING_SERVICE");
    expect(macosClient).toContain("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING=1");
    expect(macosClient).toContain("HA_NOVA_TEST_KEYRING_FILE");
    expect(macosClient).toContain("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1");
    expect(macosClient).toContain("HA_NOVA_NO_BROWSER=1");
    expect(macosClient).toContain("wait_for_mock_server()");
    expect(macosClient).toContain("MOCK_REPORTED_VERSION");
    expect(macosClient).toContain("require_bundle_assets_ready");
    expect(macosClient).toContain("claude_marketplace_points_to_root()");
    expect(macosClient).toContain(
      '--relay-url "http://127.0.0.1:${MOCK_RELAY_PORT}"',
    );
    expect(macosClient).toContain(".agents/skills/ha-nova/ha-nova/SKILL.md");
    expect(macosClient).toContain(
      ".config/opencode/skills/ha-nova/ha-nova/SKILL.md",
    );
    expect(macosClient).toContain(".gemini/config/skills/ha-nova/SKILL.md");
    expect(macosClient).toContain(".hermes/skills/ha-nova/ha-nova/SKILL.md");
    expect(macosClient).toContain(
      ".hermes/skills/ha-nova/ha-nova-read/SKILL.md",
    );
    expect(macosClient).toContain("name: ha-nova-read");
    expect(macosClient).toContain(".claude/plugins/installed_plugins.json");
    expect(macosClient).toContain(".claude/plugins/known_marketplaces.json");
    expect(macosClient).toContain(
      'claude_marketplace_points_to_root "${TMP_HOME}/.claude/plugins/known_marketplaces.json"',
    );
    expect(macosClient).toContain("ha-nova@ha-nova");
    expect(macosClient).toContain('test ! -e "${TMP_HOME}/.local/bin/ha-nova"');
    expect(macosClient).toContain(
      'test -e "${TMP_HOME}/.config/ha-nova/config.json"',
    );
    expect(macosClient).toContain(
      'test ! -e "${TMP_HOME}/.config/ha-nova/state.json"',
    );
    expect(macosClient).toContain('test ! -e "${TMP_HOME}/.cache/ha-nova"');
    expect(macosClient).toContain(
      'test ! -e "${TMP_HOME}/.hermes/skills/ha-nova"',
    );
  });

  it("uses the private bundle override path for the macOS smoke lane", () => {
    expect(macosSmoke).toContain("HA_NOVA_BUNDLE_URL");
    expect(macosSmoke).toContain("HA_NOVA_BUNDLE_SHA256_URL");
    expect(macosSmoke).toContain("HA_NOVA_NO_SETUP=1");
    expect(macosSmoke).toContain("HA_NOVA_NO_BROWSER=1");
    expect(macosSmoke).toContain("require_bundle_assets_ready");
    expect(macosSmoke).toContain("run_lane standard --yes");
    expect(macosSmoke).toContain("run_lane purge --yes --purge");
    expect(macosSmoke).toContain(
      "printf 'test-relay-token\\n' > \"${token_file}\"",
    );
    expect(macosSmoke).toContain('test ! -e "${tmp_home}/.local/bin/ha-nova"');
    expect(macosSmoke).toContain('test -e "${token_file}"');
    expect(macosSmoke).toContain('test ! -e "${token_file}"');
    expect(macosSmoke).toContain(
      'test -e "${tmp_home}/.config/ha-nova/config.json"',
    );
    expect(macosSmoke).toContain(
      'test ! -e "${tmp_home}/.config/ha-nova/config.json"',
    );
    expect(macosSmoke).toContain(
      'test ! -e "${tmp_home}/.config/ha-nova/state.json"',
    );
    expect(macosSmoke).toContain('test ! -e "${tmp_home}/.cache/ha-nova"');
    expect(macosSmoke).toContain("printf 'MACOS_PRIVATE_RC_SMOKE_OK");
  });
});
