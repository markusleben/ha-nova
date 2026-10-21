/**
 * S-6: SessionStart hook (4 variants)
 * Tests the hooks/session-start script output in various configurations.
 */
import { readFileSync, readdirSync, mkdirSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

import { createMockBinaries, createMockHome, mockEnv, REPO_ROOT } from "../onboarding/_helpers.js";
import { makeTempDir } from "../helpers/temp-dir.js";

// Every run gets a throwaway HOME and a PATH without the real install
// (~/.local/bin/ha-nova), so no test reaches a real Relay or repairs a real
// client. Tests that need a CLI put a stub first on PATH.
const isolatedEnv = (path = "/usr/bin:/bin", home = makeTempDir("ha-nova-hook-home-")) => ({ PATH: path, HOME: home });

describe("S-6: session-start hook", () => {
  it("outputs valid JSON with skill content", () => {
    // No ha-nova on PATH: the budget measures the injected files only, and the
    // test never reaches a real install or its Relay.
    const result = spawnSync("bash", ["hooks/session-start"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 15000,
      env: isolatedEnv(),
    });

    expect(result.status).toBe(0);

    const output = result.stdout.trim();
    const json = JSON.parse(output);

    expect(json).toHaveProperty("additional_context");
    expect(json).toHaveProperty("hookSpecificOutput");
    expect(json.additional_context).toContain("HA NOVA Skills");
    expect(json.additional_context).not.toContain("Relay unreachable");
    expect(json.additional_context).not.toContain("not on PATH");
    // Compact index + bootstrap only; the full router loads on demand.
    expect(json.additional_context).toContain("# HA NOVA Index");
    expect(json.additional_context).toContain("load the full context skill `ha-nova:ha-nova`");
    expect(json.additional_context).not.toContain("name: ha-nova");
    expect(json.additional_context).toContain("# Session Bootstrap");
    expect(json.additional_context).toContain("ha-nova check-update --quiet");
    expect(json.additional_context).toContain("`notices[]`");
    // Typed-verbs PR4 budget: ~700 words (was ~6.1k with the full router).
    expect(json.additional_context.split(/\s+/).length).toBeLessThan(760);
  });

  // A copied plugin root, so the hook can run against a broken or hostile tree.
  const pluginCopy = (index: string | null): string => {
    const root = makeTempDir("ha-nova-hook-root-");
    mkdirSync(join(root, "hooks"));
    mkdirSync(join(root, "skills", "ha-nova"), { recursive: true });
    writeFileSync(join(root, "hooks", "session-start"), readFileSync("hooks/session-start", "utf8"), { mode: 0o755 });
    for (const file of ["SKILL.md", "session-bootstrap.md"]) {
      writeFileSync(join(root, "skills", "ha-nova", file), readFileSync(`skills/ha-nova/${file}`, "utf8"));
    }
    if (index !== null) writeFileSync(join(root, "skills", "ha-nova", "index.md"), index);
    return root;
  };

  it("names the demo house and drops the Relay warning when only the demo is connected", () => {
    const home = makeTempDir("ha-nova-hook-demo-");
    const bin = makeTempDir("ha-nova-hook-bin-");
    mkdirSync(join(home, ".config", "ha-nova"), { recursive: true });
    writeFileSync(join(home, ".config", "ha-nova", "demo.json"), "{}");
    writeFileSync(join(bin, "ha-nova"), "#!/usr/bin/env bash\nexit 1\n", { mode: 0o755 });
    const run = () =>
      JSON.parse(
        spawnSync("bash", ["hooks/session-start"], {
          cwd: REPO_ROOT,
          encoding: "utf8",
          timeout: 15000,
          env: { PATH: `${bin}:/usr/bin:/bin`, HOME: home },
        }).stdout.trim(),
      ).additional_context as string;
    let context = run();
    expect(context).toContain("The demo house is connected (ha-nova try)");
    expect(context).not.toContain("Relay unreachable");
    writeFileSync(join(home, ".config", "ha-nova", "config.json"), "{}");
    context = run();
    expect(context).toContain("The demo house is connected (ha-nova try)");
    expect(context).toContain("Relay unreachable"); // a real home's Relay still gets its warning
  });

  // 2026-10-21: once the hook really ran, its background jobs inherited
  // Claude Code's stdout pipe, so every session start waited for them.
  it("returns at once while its background jobs still run, and repairs only on a fresh start", () => {
    const home = makeTempDir("ha-nova-hook-slow-");
    const bin = makeTempDir("ha-nova-hook-slow-bin-");
    const log = join(home, "calls.log");
    writeFileSync(
      join(bin, "ha-nova"),
      `#!/usr/bin/env bash\necho "$1" >> "${log}"\ncase "$1" in relay) exit 1;; esac\nsleep 5\n`,
      { mode: 0o755 },
    );
    const run = (source: string | null) => {
      const started = Date.now();
      const result = spawnSync("bash", ["hooks/session-start"], {
        cwd: REPO_ROOT,
        encoding: "utf8",
        timeout: 15000,
        input: source === null ? "" : JSON.stringify({ hook_event_name: "SessionStart", source }),
        env: { PATH: `${bin}:/usr/bin:/bin`, HOME: home },
      });
      expect(result.status).toBe(0);
      JSON.parse(result.stdout.trim());
      return Date.now() - started;
    };
    expect(run("compact")).toBeLessThan(2000);
    expect(run("resume")).toBeLessThan(2000);
    expect(run(null)).toBeLessThan(2000);
    expect(run("startup")).toBeLessThan(2000);
    spawnSync("sleep", ["0.5"]);
    const calls = readFileSync(log, "utf8").split("\n").filter((line) => line === "doctor");
    expect(calls).toHaveLength(1);
  });

  // CLI installs: a GUI-launched Claude Code may start without ~/.local/bin on
  // PATH. The hook then finds the installer's symlink and names its full path.
  it("finds the installed CLI outside PATH and names its full path", () => {
    const home = makeTempDir("ha-nova-hook-gui-");
    const log = join(home, "calls.log");
    mkdirSync(join(home, ".local", "bin"), { recursive: true });
    writeFileSync(
      join(home, ".local", "bin", "ha-nova"),
      `#!/usr/bin/env bash\necho "$1" >> "${log}"\ncase "$1" in relay) echo '{"version":"99.0.0"}';; esac\n`,
      { mode: 0o755 },
    );
    const result = spawnSync("bash", ["hooks/session-start"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 15000,
      input: JSON.stringify({ hook_event_name: "SessionStart", source: "resume" }),
      env: isolatedEnv("/usr/bin:/bin", home),
    });
    expect(result.status).toBe(0);
    const context = JSON.parse(result.stdout.trim()).additional_context as string;
    expect(context).toContain(`ha-nova is not on PATH here. Run it by its full path: ${join(home, ".local", "bin", "ha-nova")}`);
    expect(context).not.toContain("Relay unreachable");
    expect(readFileSync(log, "utf8")).toContain("relay");
  });

  // A dead Relay must not hold the session: the stub fails at once, like a
  // refused connection.
  it("stays fast with an unreachable Relay", () => {
    const bin = makeTempDir("ha-nova-hook-dead-bin-");
    writeFileSync(join(bin, "ha-nova"), "#!/usr/bin/env bash\nexit 1\n", { mode: 0o755 });
    const started = Date.now();
    const result = spawnSync("bash", ["hooks/session-start"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 15000,
      input: JSON.stringify({ hook_event_name: "SessionStart", source: "startup" }),
      env: isolatedEnv(`${bin}:/usr/bin:/bin`),
    });
    expect(Date.now() - started).toBeLessThan(2000);
    expect(result.status).toBe(0);
    expect(JSON.parse(result.stdout.trim()).additional_context).toContain("Relay unreachable");
  });

  it("fails loud when the index is missing", () => {
    const root = pluginCopy(null);
    const result = spawnSync("bash", [join(root, "hooks", "session-start")], { encoding: "utf8", timeout: 15000, env: isolatedEnv() });
    expect(result.status).toBe(1);
    expect(result.stderr).toContain("Missing HA NOVA file: skills/ha-nova/index.md");
    expect(result.stdout).toBe("");
  });

  it("escapes every control character into valid JSON", () => {
    const root = pluginCopy("# Index\nbell\u0007 esc\u001b vt\u000b nul-adjacent\u0001 quote\" slash\\ tab\t");
    const result = spawnSync("bash", [join(root, "hooks", "session-start")], { encoding: "utf8", timeout: 15000, env: isolatedEnv() });
    expect(result.status).toBe(0);
    const json = JSON.parse(result.stdout.trim());
    expect(json.additional_context).toContain("bell\u0007 esc\u001b vt\u000b nul-adjacent\u0001 quote\" slash\\ tab\t");
  });

  it("lists every skill in the injected index", () => {
    const index = readFileSync("skills/ha-nova/index.md", "utf8");
    for (const dir of readdirSync("skills", { withFileTypes: true })) {
      if (!dir.isDirectory()) continue;
      expect(index, `index must list ha-nova:${dir.name}`).toContain(`\`ha-nova:${dir.name}\``);
    }
  });

  it("includes sub-skill discovery list", () => {
    const result = spawnSync("bash", ["hooks/session-start"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 15000,
      env: isolatedEnv(),
    });

    const json = JSON.parse(result.stdout.trim());
    expect(json.additional_context).toContain("ha-nova:read");
    expect(json.additional_context).toContain("ha-nova:write");
    expect(json.additional_context).toContain("ha-nova:review");
    expect(json.additional_context).toContain("ha-nova:entity-discovery");
    expect(json.additional_context).toContain("ha-nova:health");
    expect(json.additional_context).toContain("ha-nova:calendar");
    expect(json.additional_context).toContain("ha-nova:service-call");
    expect(json.additional_context).toContain("ha-nova:onboarding");
  });

  it("includes version from version.json", () => {
    const versionJson = JSON.parse(readFileSync("version.json", "utf8"));
    const result = spawnSync("bash", ["hooks/session-start"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 15000,
      env: isolatedEnv(),
    });

    const json = JSON.parse(result.stdout.trim());
    expect(json.additional_context).toContain(`HA NOVA Skills v${versionJson.skill_version}`);
  });

  it("warns when relay version is outdated (via relay CLI)", () => {
    // This test verifies the hook checks relay version.
    // The hook sources the relay CLI which calls /health.
    // We verify the hook *script* contains the version comparison logic.
    const hookContent = readFileSync("hooks/session-start", "utf8");

    expect(hookContent).toContain("semver_lt");
    expect(hookContent).toContain("min_relay_version");
    expect(hookContent).toContain("WARNING:");
    expect(hookContent).toContain("Relay version");
  });

  it("uses shared release cache and background CLI refresh", () => {
    const hookContent = readFileSync("hooks/session-start", "utf8");

    expect(hookContent).toContain("latest-release.json");
    expect(hookContent).toContain('"version"');
    expect(hookContent).toContain("UPDATE AVAILABLE");
    expect(hookContent).toContain("ha-nova update");
    expect(hookContent).toContain('"$ha_nova_bin" check-update --quiet --json');
    expect(hookContent).not.toContain("api.github.com/repos/markusleben/ha-nova/releases/latest");
    // Compact release highlights come from the shared cache via grep-based
    // extraction of the "text" fields — never a jq dependency.
    expect(hookContent).toContain("release_highlights");
    expect(hookContent).toContain('"text"');
    expect(hookContent).toContain("Release notes:");
  });

  it("surfaces cached release highlights and release URL in the update notice", () => {
    const home = makeTempDir("ha-nova-hook-home-");
    const cacheDir = join(home, ".cache", "ha-nova");
    mkdirSync(cacheDir, { recursive: true });
    writeFileSync(
      join(cacheDir, "latest-release.json"),
      JSON.stringify(
        {
          version: "99.0.0",
          html_url: "https://example.test/releases/v99.0.0",
          published_at: "2026-07-21T10:00:00Z",
          release_highlights: [
            { kind: "action", text: "Re-run ha-nova setup after updating" },
            { kind: "feature", text: "New energy skill" },
            { kind: "fix", text: "Fix relay reconnect loop" },
          ],
        },
        null,
        2,
      ),
    );

    // Deterministic ha-nova mock: a released (non-dev) version so the update
    // notice branch runs; every other subcommand no-ops. Keep the normal test
    // PATH otherwise — the other hook tests rely on the same bash resolution.
    const binDir = makeTempDir("ha-nova-hook-bin-");
    writeFileSync(
      join(binDir, "ha-nova"),
      `#!/usr/bin/env bash
case "$1" in
  version) echo "0.19.0" ;;
  relay) exit 1 ;;
  *) exit 0 ;;
esac
`,
      { mode: 0o755 },
    );

    const result = spawnSync("bash", ["hooks/session-start"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 15000,
      env: isolatedEnv(`${binDir}:/usr/bin:/bin`, home),
    });

    expect(result.status).toBe(0);
    const json = JSON.parse(result.stdout.trim());
    expect(json.additional_context).toContain("UPDATE AVAILABLE");
    expect(json.additional_context).toContain("v99.0.0");
    expect(json.additional_context).toContain("- Re-run ha-nova setup after updating");
    expect(json.additional_context).toContain("- New energy skill");
    expect(json.additional_context).toContain("- Fix relay reconnect loop");
    expect(json.additional_context).toContain("Release notes: https://example.test/releases/v99.0.0");
  });

  it("keeps the SessionStart refresh throttle within the CLI freshness floor", () => {
    // The hook's spawn throttle must stay <= the CLI's release-cache TTL, or a
    // newly published release stays hidden from the session banner until the
    // longer window expires (this is exactly how the 24h cache hid v0.5.0).
    const hook = readFileSync("hooks/session-start", "utf8");
    const paths = readFileSync("cli/paths.go", "utf8");

    const hookTtl = Number(hook.match(/update_ttl=(\d+)/)?.[1]);
    const cliExpr = paths.match(/updateCacheTTLSeconds\s*=\s*([0-9*\s]+)/)?.[1] ?? "";
    const cliTtl = cliExpr.split("*").reduce((acc, part) => acc * Number(part.trim()), 1);

    expect(hookTtl).toBeGreaterThan(0);
    expect(cliTtl).toBeGreaterThan(0);
    expect(hookTtl).toBeLessThanOrEqual(cliTtl);
    expect(cliTtl).toBeLessThanOrEqual(3600);
    expect(hook).not.toContain("update_ttl=86400");
  });

  it("does not leak secrets in JSON output", () => {
    const result = spawnSync("bash", ["hooks/session-start"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 15000,
      env: isolatedEnv(),
    });

    const output = result.stdout;
    expect(output).not.toContain("RELAY_AUTH_TOKEN");
    // The skill text may NAME the standalone env var (documentation); the
    // guard is against value assignments leaking into hook output.
    expect(output).not.toMatch(/HA_LLAT\s*[=:]/);
    expect(output).not.toContain("Bearer");
  });
});
