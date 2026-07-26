import { createHash } from "node:crypto";
import { execFile, execFileSync } from "node:child_process";
import { createServer, type Server } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";
import { constants, mkdtempSync, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

const execFileAsync = promisify(execFile);

function hasBash(): boolean {
  try {
    execFileSync("bash", ["-c", "exit 0"], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}

async function listen(server: Server): Promise<number> {
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (address === null || typeof address === "string") {
    throw new Error("test server did not expose a port");
  }
  return address.port;
}

async function runInstallerFunctions(script: string): Promise<string> {
  const result = await execFileAsync("bash", [
    "-c",
    `set -euo pipefail; export HA_NOVA_INSTALLER_TEST_EXPORT=1 HA_NOVA_PLAIN_UI=1; source install.sh; ${script}`,
  ]);
  return `${result.stdout}${result.stderr}`;
}

describe("install.sh contract", () => {
  const content = readFileSync("install.sh", "utf8");

  it("is executable with proper shebang", () => {
    const stats = statSync("install.sh");
    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
  });

  it("uses a thin Unix bootstrap with macOS and Linux support", () => {
    expect(content).toContain("set -euo pipefail");
    expect(content).toContain('Darwin) printf \'%s\\n\' "macos"');
    expect(content).toContain('Linux) printf \'%s\\n\' "linux"');
    expect(content).toContain("curl");
    expect(content).toContain("tar");
    expect(content).not.toContain("git clone");
    expect(content).not.toContain("npm install");
  });

  it("supports plain UI mode and no-color presentation", () => {
    expect(content).toContain("HA_NOVA_PLAIN_UI");
    expect(content).toContain("NO_COLOR");
    expect(content).toContain("TERM:-");
    expect(content).toContain("UI_ACCENT=$'\\033[93m'");
    expect(content).toContain('[!] $*');
    expect(content).toContain("No interactive terminal detected; setup was not started automatically.");
  });

  it("resolves the version from GitHub Releases latest unless HA_NOVA_VERSION is pinned", () => {
    expect(content).toContain("https://api.github.com/repos/markusleben/ha-nova/releases/latest");
    expect(content).toContain("HA_NOVA_VERSION");
    expect(content).toContain("tag_name");
    expect(content).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/version.json");
  });

  it("supports maintainer-only bundle URL overrides for private RC tests", () => {
    expect(content).toContain("HA_NOVA_BUNDLE_URL");
    expect(content).toContain("HA_NOVA_BUNDLE_SHA256_URL");
    expect(content).toContain("bundle_version_tag()");
    expect(content).toContain("Downloaded bundle version");
  });

  it("downloads a platform bundle and validates bundle.json before install", () => {
    expect(content).toContain("ha-nova-installer-bundle-macos");
    expect(content).toContain("ha-nova-installer-bundle-linux");
    expect(content).toContain("bundle.json");
    expect(content).toContain(".sha256");
    expect(content).toContain("Downloaded bundle is missing the ha-nova binary.");
    expect(content).toContain(".local/share/ha-nova");
  });

  it("keeps temp/backup cleanup best-effort so an antivirus/indexer lock cannot abort a completed install", () => {
    // Regression: under `set -euo pipefail` an unguarded `rm -rf` on the freshly
    // extracted unsigned binary (macOS Gatekeeper/XProtect/Spotlight holding it,
    // or an NFS .nfsXXXX handle) would abort AFTER a successful swap but BEFORE
    // PATH setup + setup launch — installed yet unreachable. Cleanup must tolerate
    // a failed delete, and the swap must catch a lock instead of orphaning next_root.
    expect(content).toContain('rm -rf "${TMP_DIR}" 2>/dev/null || true');
    expect(content).toContain('rm -rf "${backup_root}" 2>/dev/null || true');
    expect(content).toContain('if ! mv "${INSTALL_DIR}" "${backup_root}"; then');
    // The rollback (restore the old install if the swap-in fails) must itself be
    // best-effort so a busy backup_root can't crash the failure path under set -e.
    expect(content).toContain(
      '[[ -d "${backup_root}" ]] && mv "${backup_root}" "${INSTALL_DIR}" 2>/dev/null || true',
    );
  });

  it("detects legacy installs and prints the dedicated cleanup one-liner", () => {
    expect(content).toContain("legacy-uninstall.sh");
    expect(content).toContain("raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.sh");
    expect(content).toContain("onboarding.env");
    expect(content).toContain("check-update.cmd");
    expect(content).not.toContain('[[ -f "${CONFIG_DIR}/relay" ]]');
    expect(content).not.toContain('[[ -f "${CONFIG_DIR}/relay.exe" ]]');
    expect(content).not.toContain('[[ -f "${CONFIG_DIR}/version-check" ]]');
  });

  it("installs ha-nova into ~/.local/bin as a single public command and manages PATH", () => {
    expect(content).toContain("BIN_DIR");
    expect(content).toContain("BIN_LINK");
    expect(content).toContain("install_binary");
    expect(content).toContain("ensure_bin_dir_on_path()");
    expect(content).toContain('export PATH="$HOME/.local/bin:$PATH"');
    expect(content).not.toContain('cp "${runtime_bin}" "${BIN_DIR}/ha-nova"');
  });

  it("keeps bootstrap logic out of product state persistence", () => {
    expect(content).not.toContain("write_state()");
    expect(content).not.toContain("STATE_FILE");
    expect(content).not.toContain("install_source");
    expect(content).not.toContain("path_managed");
  });

  it("starts ha-nova setup only when interactive and respects HA_NOVA_NO_SETUP", () => {
    expect(content).toContain("has_interactive_tty()");
    expect(content).toContain('if : 2>/dev/null </dev/tty; then');
    expect(content).toContain("HA_NOVA_NO_SETUP");
    expect(content).toContain('run_setup "${BIN_LINK}"');
    expect(content).toContain("Next step: ha-nova setup");
    expect(content).toContain(
      "Setup will ask for the six-digit pairing code shown in NOVA Home Base.",
    );
    expect(content).toContain("Need help later? Run: ha-nova doctor");
  });

  it("prints the exact pairing-aware continuation without reading hidden input", async () => {
    if (!hasBash()) return;
    const output = await runInstallerFunctions(
      'has_interactive_tty() { return 1; }; run_setup "/bin/false"',
    );
    expect(output).toContain(
      "No interactive terminal detected; setup was not started automatically.",
    );
    expect(output).toContain("Next step: ha-nova setup");
    expect(output).toContain(
      "Setup will ask for the six-digit pairing code shown in NOVA Home Base.",
    );
  });

  it("uses resilient curl downloads with checksum-first verification", () => {
    expect(content).toContain("fetch_url()");
    expect(content).toContain("--connect-timeout 15");
    expect(content).toContain("--retry 4");
    expect(content).toContain("--retry-all-errors");
    expect(content).toContain("download_bundle_archive()");
    expect(content).toContain('fail_download "the HA NOVA release bundle"');
    expect(content).toContain('fail_download "the HA NOVA bundle checksum"');
    expect(content).toContain("Could not read the latest HA NOVA release from");
    expect(content).toContain("HA_NOVA_INSTALLER_TEST_EXPORT");
    // Checksum is downloaded before the bundle so a wrong-bytes archive can be
    // discarded and re-downloaded once before failing.
    const helper = content.slice(content.indexOf("download_bundle_archive()"));
    expect(helper.indexOf("${checksum_url}")).toBeLessThan(helper.indexOf("${archive_url}"));
    expect(helper).toContain('rm -f "${archive_path}"');
  });

  it("retries a transient server error during download", async () => {
    if (!hasBash()) {
      return;
    }

    let calls = 0;
    const server = createServer((req, res) => {
      calls += 1;
      if (calls === 1) {
        res.writeHead(503).end("flaky");
        return;
      }
      res.writeHead(200).end("payload");
    });
    const port = await listen(server);
    const outFile = join(mkdtempSync(join(tmpdir(), "ha-nova-installer-test-")), "asset");

    try {
      await runInstallerFunctions(`fetch_url "http://127.0.0.1:${port}/asset" "${outFile}"`);
    } finally {
      server.close();
    }

    expect(calls).toBe(2);
    expect(readFileSync(outFile, "utf8")).toBe("payload");
  }, 30_000);

  it("re-downloads the bundle once when the checksum does not match", async () => {
    if (!hasBash()) {
      return;
    }

    const payload = "verified bundle payload";
    const expectedHash = createHash("sha256").update(payload).digest("hex");
    let bundleCalls = 0;
    const server = createServer((req, res) => {
      if (req.url === "/bundle.sha256") {
        res.writeHead(200).end(`${expectedHash}  bundle\n`);
        return;
      }
      bundleCalls += 1;
      if (bundleCalls === 1) {
        // 200 OK with wrong bytes: proxy substitution / captive portal.
        res.writeHead(200).end("<html>not the bundle</html>");
        return;
      }
      res.writeHead(200).end(payload);
    });
    const port = await listen(server);
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-installer-test-"));
    const archiveFile = join(tempDir, "bundle.tar.gz");
    const checksumFile = `${archiveFile}.sha256`;

    try {
      await runInstallerFunctions(
        `download_bundle_archive "http://127.0.0.1:${port}/bundle" "http://127.0.0.1:${port}/bundle.sha256" "${archiveFile}" "${checksumFile}"`,
      );
    } finally {
      server.close();
    }

    expect(bundleCalls).toBe(2);
    expect(readFileSync(archiveFile, "utf8")).toBe(payload);
  }, 30_000);

  it("does not use sudo or eval", () => {
    expect(content).not.toContain("sudo ");
    expect(content).not.toMatch(/\beval\b/);
  });
});
