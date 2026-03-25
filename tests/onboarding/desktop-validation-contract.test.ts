import { constants, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("desktop validation helpers contract", () => {
  const releasing = readFileSync("docs/releasing.md", "utf8");
  const packageJson = readFileSync("package.json", "utf8");
  const harness = readFileSync("scripts/dev/start-local-validation-harness.sh", "utf8");
  const macosSuite = readFileSync("scripts/dev/macos-private-rc-suite.sh", "utf8");
  const macosSmoke = readFileSync("scripts/dev/macos-private-rc-smoke.sh", "utf8");
  const macosSetupAll = readFileSync("scripts/dev/macos-private-rc-setup-all.sh", "utf8");
  const macosClient = readFileSync("scripts/dev/macos-private-rc-client.sh", "utf8");
  const mockServer = readFileSync("scripts/dev/mock-ha-relay.py", "utf8");
  const windowsCleanup = readFileSync("scripts/dev/windows-clean-test-state.ps1", "utf8");
  const windowsInstall = readFileSync("scripts/dev/windows-private-rc-install.ps1", "utf8");
  const windowsDesktop = readFileSync("scripts/dev/windows-desktop-setup.ps1", "utf8");

  it("ships executable macOS helper scripts", () => {
    for (const path of [
      "scripts/dev/macos-private-rc-suite.sh",
      "scripts/dev/macos-private-rc-smoke.sh",
      "scripts/dev/macos-private-rc-setup-all.sh",
      "scripts/dev/macos-private-rc-client.sh",
      "scripts/dev/start-local-validation-harness.sh",
    ]) {
      const stats = statSync(path);
      expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    }
  });

  it("documents the private-RC desktop helper entrypoints", () => {
    expect(releasing).toContain("Desktop Validation Helpers");
    expect(releasing).toContain("scripts/dev/macos-private-rc-suite.sh");
    expect(releasing).toContain("scripts/dev/macos-private-rc-smoke.sh");
    expect(releasing).toContain("scripts/dev/macos-private-rc-setup-all.sh");
    expect(releasing).toContain("scripts/dev/macos-private-rc-client.sh");
    expect(releasing).toContain("scripts/dev/start-local-validation-harness.sh");
    expect(releasing).toContain("scripts/dev/mock-ha-relay.py");
    expect(releasing).toContain("scripts/dev/windows-clean-test-state.ps1");
    expect(releasing).toContain("test:desktop:windows:headless");
    expect(releasing).toContain("test:desktop:windows:rdp");
    expect(releasing).toContain("scripts/dev/windows-private-rc-install.ps1");
    expect(releasing).toContain("scripts/dev/windows-desktop-setup.ps1");
    expect(releasing).toContain("Do not run them against `main` or a public release.");
    expect(releasing).toContain("pkill -f");
  });

  it("ships a single local validation harness entrypoint", () => {
    expect(packageJson).toContain('"dev:validation:harness"');
    expect(harness).toContain("npm run release:rc:local");
    expect(harness).toContain("python3 -m http.server");
    expect(harness).toContain("mock-ha-relay.py");
    expect(harness).toContain("HA_NOVA_BUNDLE_URL");
    expect(harness).toContain("HA_NOVA_BUNDLE_SHA256_URL");
    expect(harness).toContain("HA_NOVA_WINGET_INSTALLER_URL");
    expect(harness).toContain("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL");
    expect(harness).toContain("ha-nova-installer-bundle-macos-arm64.tar.gz");
    expect(harness).toContain("ha-nova-installer-bundle-macos-amd64.tar.gz");
    expect(harness).toContain("ha-nova-installer-bundle-windows-amd64.zip");
    expect(harness).toContain("ha-nova-winget-manifest-v");
    expect(harness).toContain("install.ps1 | iex");
    expect(harness).toContain("winget settings --enable LocalManifestFiles");
    expect(harness).toContain("winget install --manifest");
    expect(harness).toContain("--with-mock");
    expect(harness).toContain("ensure_port_free");
    expect(harness).toContain("bundle_reported_version");
    expect(harness).toContain('build-winget-manifest.sh "${reported_version}"');
    expect(harness).toContain("port ${port} is already in use");
    expect(harness).toContain("Harness asset missing or not reachable");
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
  });

  it("keeps the mock server intentionally tiny and dependency-free", () => {
    expect(mockServer).toContain("from http.server import BaseHTTPRequestHandler, HTTPServer");
    expect(mockServer).toContain('if self.path == "/health"');
    expect(mockServer).toContain('"ha_ws_connected": True');
    expect(mockServer).toContain('self.wfile.write(b"OK")');
    expect(mockServer).toContain('--reported-version');
    expect(mockServer).toContain('reported version');
    expect(mockServer).toContain('fake relay /health');
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
    expect(macosSetupAll).toContain('MOCK_REPORTED_VERSION');
    expect(macosSetupAll).toContain('${MOCK_REPORTED_VERSION:-}');
    expect(macosSetupAll).toContain("os.path.normpath");
    expect(macosSetupAll).toContain("bundle_reported_version");
    expect(macosSetupAll).toContain("Set MOCK_REPORTED_VERSION explicitly when overriding the bundle source.");
    expect(macosSetupAll).toContain("wait_for_mock_server()");
    expect(macosSetupAll).toContain('curl -fsS "${ha_url}"');
    expect(macosSetupAll).toContain("mock-server.log");
    expect(macosSetupAll).toContain('runtime_bin="${TMP_HOME}/.local/bin/ha-nova"');
    expect(macosSetupAll).toContain('"${runtime_bin}" setup all');
    expect(macosSetupAll).toContain('--relay-url "http://127.0.0.1:${MOCK_RELAY_PORT}"');
    expect(macosSetupAll).toContain('same_version="$("${runtime_bin}" version)"');
    expect(macosSetupAll).toContain('"${runtime_bin}" update --version "${same_version}"');
    expect(macosSetupAll).toContain('"${runtime_bin}" uninstall --yes');
    expect(macosSetupAll).toContain('test -e "${TMP_HOME}/.config/ha-nova/config.json"');
    expect(macosSetupAll).toContain('test ! -e "${TMP_HOME}/.config/ha-nova/state.json"');
    expect(macosSetupAll).toContain('test ! -e "${TMP_HOME}/.cache/ha-nova"');
  });

  it("keeps the macOS per-client lane explicit about expected client artifacts", () => {
    expect(macosClient).toContain("Usage: $0 <claude|codex|opencode|gemini>");
    expect(macosClient).toContain("HA_NOVA_KEYRING_SERVICE");
    expect(macosClient).toContain("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING=1");
    expect(macosClient).toContain("HA_NOVA_TEST_KEYRING_FILE");
    expect(macosClient).toContain("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1");
    expect(macosClient).toContain("HA_NOVA_NO_BROWSER=1");
    expect(macosClient).toContain("wait_for_mock_server()");
    expect(macosClient).toContain('MOCK_REPORTED_VERSION');
    expect(macosClient).toContain('${MOCK_REPORTED_VERSION:-}');
    expect(macosClient).toContain("os.path.normpath");
    expect(macosClient).toContain("bundle_reported_version");
    expect(macosClient).toContain("claude_marketplace_points_to_root()");
    expect(macosClient).toContain("Set MOCK_REPORTED_VERSION explicitly when overriding the bundle source.");
    expect(macosClient).toContain('--relay-url "http://127.0.0.1:${MOCK_RELAY_PORT}"');
    expect(macosClient).toContain(".agents/skills/ha-nova/ha-nova/SKILL.md");
    expect(macosClient).toContain(".config/opencode/skills/ha-nova/ha-nova/SKILL.md");
    expect(macosClient).toContain(".gemini/skills/ha-nova/SKILL.md");
    expect(macosClient).toContain(".claude/plugins/installed_plugins.json");
    expect(macosClient).toContain(".claude/plugins/known_marketplaces.json");
    expect(macosClient).toContain('claude_marketplace_points_to_root "${TMP_HOME}/.claude/plugins/known_marketplaces.json"');
    expect(macosClient).toContain("ha-nova@ha-nova");
    expect(macosClient).toContain('test -e "${TMP_HOME}/.config/ha-nova/config.json"');
    expect(macosClient).toContain('test ! -e "${TMP_HOME}/.config/ha-nova/state.json"');
    expect(macosClient).toContain('test ! -e "${TMP_HOME}/.cache/ha-nova"');
  });

  it("keeps the Windows cleanup helper scoped to HA NOVA-owned paths", () => {
    expect(windowsCleanup).toContain('Programs\\ha-nova');
    expect(windowsCleanup).toContain('AppData\\Roaming');
    expect(windowsCleanup).toContain('Join-Path $LocalAppDataDir "ha-nova"');
    expect(windowsCleanup).toContain('Microsoft\\WinGet\\Links\\ha-nova.exe');
    expect(windowsCleanup).toContain('markusleben.ha-nova');
    expect(windowsCleanup).toContain(".agents\\skills\\ha-nova");
    expect(windowsCleanup).toContain(".config\\opencode\\skills\\ha-nova");
    expect(windowsCleanup).toContain(".gemini\\skills\\ha-nova");
    expect(windowsCleanup).toContain(".claude\\plugins\\installed_plugins.json");
    expect(windowsCleanup).toContain('Join-Path $ConfigDir "claude-marketplace"');
    expect(windowsCleanup).toContain("Remove-ClaudePluginRecord");
    expect(windowsCleanup).toContain("Remove-ClaudeMarketplaceRecord");
    expect(windowsCleanup).toContain(".claude\\plugins\\known_marketplaces.json");
    expect(windowsCleanup).toContain('Remove-Item -LiteralPath $marketplacesJson -Force -ErrorAction SilentlyContinue');
    expect(windowsCleanup).toContain("Remove-HANovaTestCredentials");
    expect(windowsCleanup).toContain("Remove-HANovaUserEnvironment");
    expect(windowsCleanup).toContain('HKCU:\\Environment\\$name');
    expect(windowsCleanup).toContain("HA_NOVA_KEYRING_SERVICE");
    expect(windowsCleanup).toContain("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING");
    expect(windowsCleanup).toContain("HA_NOVA_TEST_KEYRING_FILE");
    expect(windowsCleanup).toContain("cmdkey.exe");
    expect(windowsCleanup).toContain("ha-nova.relay-auth-token");
    expect(windowsCleanup).not.toContain('Join-Path $HOME ".agents")');
    expect(windowsCleanup).not.toContain('Join-Path $HOME ".config\\opencode")');
    expect(windowsCleanup).not.toContain('Join-Path $HOME ".gemini")');
  });

  it("treats Windows installer and desktop runner failures as hard failures", () => {
    expect(windowsInstall).toContain("VERSION_EXIT:");
    expect(windowsInstall).toContain("UNINSTALL_EXIT:");
    expect(windowsInstall).toContain("HA_NOVA_KEYRING_SERVICE");
    expect(windowsInstall).toContain('HA_NOVA_CLAUDE_MARKETPLACE_LOCAL = "1"');
    expect(windowsInstall).toContain('Programs\\ha-nova');
    expect(windowsInstall).toContain("cmd.exe /d /s /c");
    expect(windowsInstall).toContain('throw "ha-nova version failed"');
    expect(windowsInstall).toContain('throw "ha-nova uninstall failed"');
    expect(windowsDesktop).toContain("VERSION_EXIT:");
    expect(windowsDesktop).toContain("SETUP_EXIT:");
    expect(windowsDesktop).toContain("DOCTOR_EXIT:");
    expect(windowsDesktop).toContain("UPDATE_EXIT:");
    expect(windowsDesktop).toContain("UNINSTALL_EXIT:");
    expect(windowsDesktop).toContain("HA_NOVA_KEYRING_SERVICE");
    expect(windowsDesktop).toContain('HA_NOVA_CLAUDE_MARKETPLACE_LOCAL = "1"');
    expect(windowsDesktop).toContain('Programs\\ha-nova');
    expect(windowsDesktop).toContain("Get-MergedPath");
    expect(windowsDesktop).toContain('$env:Path = Get-MergedPath');
    expect(windowsDesktop).toContain('[string]$HAHost = "127.0.0.1"');
    expect(windowsDesktop).not.toContain('[string]$Host = "127.0.0.1"');
    expect(windowsDesktop).toContain("cmd.exe /d /s /c");
    expect(windowsDesktop).toContain('claude installed_plugins.json missing');
    expect(windowsDesktop).toContain('claude known_marketplaces.json missing');
    expect(windowsDesktop).toContain('ha-nova@ha-nova');
    expect(windowsDesktop).toContain('claude marketplace source is not the local install root');
    expect(windowsDesktop).toContain('"all" {');
    expect(windowsDesktop).toContain('all-client validation missing Claude plugin record');
    expect(windowsDesktop).toContain('all-client validation missing local Claude marketplace source');
    expect(windowsDesktop).toContain('throw "setup failed"');
    expect(windowsDesktop).toContain('throw "doctor failed"');
    expect(windowsDesktop).toContain('throw "update failed"');
    expect(windowsDesktop).toContain('throw "uninstall failed"');
    expect(windowsDesktop).toContain('throw "version failed"');
  });

  it("uses the private bundle override path for the macOS smoke lane", () => {
    expect(macosSmoke).toContain("HA_NOVA_BUNDLE_URL");
    expect(macosSmoke).toContain("HA_NOVA_BUNDLE_SHA256_URL");
    expect(macosSmoke).toContain("HA_NOVA_NO_SETUP=1");
    expect(macosSmoke).toContain("HA_NOVA_NO_BROWSER=1");
    expect(macosSmoke).toContain("os.path.normpath");
    expect(macosSmoke).toContain('test ! -e "${TMP_HOME}/.config/ha-nova/config.json"');
    expect(macosSmoke).toContain('test ! -e "${TMP_HOME}/.config/ha-nova/state.json"');
    expect(macosSmoke).toContain('test ! -e "${TMP_HOME}/.cache/ha-nova"');
    expect(macosSmoke).toContain('printf \'MACOS_PRIVATE_RC_SMOKE_OK:');
  });
});
