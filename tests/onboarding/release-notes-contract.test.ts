import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("release notes contract", () => {
  const goreleaser = readFileSync(".goreleaser.yml", "utf8");
  const readme = readFileSync("README.md", "utf8");
  const privacy = readFileSync("PRIVACY.md", "utf8");
  const safety = readFileSync("docs/reference/safety.md", "utf8");
  const clientIntegration = readFileSync("docs/reference/client-integration.md", "utf8");
  const stableNotes = goreleaser
    .split("{{ else }}", 2)[1]
    ?.split("Stable install commands are release-pinned", 1)[0];

  it("keeps release notes aligned to the single supported Windows install path", () => {
    expect(goreleaser).toContain(
      "Stable install commands are release-pinned for this tag.",
    );
    expect(goreleaser).toContain(
      "https://raw.githubusercontent.com/markusleben/ha-nova/{{ .Tag }}/install.sh",
    );
    expect(goreleaser).toContain("HA_NOVA_VERSION={{ .Tag }}");
    expect(goreleaser).toContain(
      "https://raw.githubusercontent.com/markusleben/ha-nova/{{ .Tag }}/install.ps1",
    );
    expect(goreleaser).toContain(
      "Windows uses a single supported install path: `install.ps1`.",
    );
    expect(goreleaser).not.toContain(
      "raw.githubusercontent.com/markusleben/ha-nova/main/install.sh",
    );
    expect(goreleaser).not.toContain(
      "raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1",
    );
    expect(goreleaser).not.toContain(
      "$ProgressPreference = 'SilentlyContinue'",
    );
    expect(goreleaser).not.toContain("winget");
  });

  it("keeps v0.28.0 release-facing wording aligned to the shipped stack", () => {
    const relayVersion = /^version:\s*"([^"]+)"\s*$/m.exec(readFileSync("nova/config.yaml", "utf8"))?.[1] ?? "";
    // Any App version the notes name is the shipped App.
    const notedApps = [...(stableNotes ?? "").matchAll(/NOVA (?:Relay |App to )(\d+\.\d+\.\d+)/g)];
    expect(notedApps.length, "notes name the App version chat apps need").toBeGreaterThan(0);
    for (const [, noted] of notedApps) {
      expect(noted, "notes name the shipped NOVA Relay version").toBe(relayVersion);
    }
    expect(stableNotes).toMatch(/^## New Features\s+- \*\*Chat apps do what HA NOVA on a computer does\.\*\*/);
    // Chat apps at CLI parity (#863, #864, #865, #867); only setting up HA NOVA itself stays on a computer.
    expect(stableNotes).toContain("files (with file access on), and more. Only setting up HA NOVA itself stays on a computer.");
    // "Always allow" advice by phone (#867).
    expect(stableNotes).toContain('with a linked phone, every tool on "Always allow"; without one, all but "Run a confirmed change".');
    // Codex in the desktop app; stale copies flagged and moved (fix/codex-app-guidance).
    expect(stableNotes).toContain("**HA NOVA works in the Codex app too.**");
    expect(stableNotes).toContain("Doctor flags an older HA NOVA copy that Codex would load; setup updates it, or moves HA NOVA's own old copies to a backup.");
    // HACS and dashboard resources take the typed confirmation (#864, maintainer decision 3).
    expect(stableNotes).toMatch(/## What To Watch\s+- \*\*HACS installs and updates, and dashboard resources, now take your own confirmation, like a delete\.\*\*/);
    expect(stableNotes).toContain("**Camera sign-in tokens stay hidden from the AI.**");
    expect(stableNotes).toContain("**A leftover Home Assistant Link is explained.**");
    expect(stableNotes).toContain("**Setup says which Claude gets HA NOVA.** Claude Code is the claude command and the Code tab in Claude Desktop.");
    expect(stableNotes).toContain("**Setup explains the owner sign-in.** The owner connects each computer once");
    expect(stableNotes).toContain("**Disabling a lock's device is refused.**");
    // App 0.12.0 carries the 0.28.0 chat bridge; its tools changed, so ChatGPT needs a Refresh and Claude asks once more.
    expect(stableNotes).toContain("Settings > Apps > NOVA Relay > Update");
    expect(stableNotes).toContain("Then in ChatGPT: open chatgpt.com/plugins, choose HA NOVA, then Refresh.");
    expect(stableNotes).toContain('Claude asks once more before HA NOVA\'s tools: set them to "Always allow" again.');
    expect(stableNotes).not.toContain("which this release doesn't change");
    // Chat apps are named as such; the MCP adapter itself stays unadvertised.
    expect(stableNotes).not.toMatch(/mcp/i);
    expect(stableNotes).not.toContain("Cloud Remote is back on Windows and Linux");
    expect(readme).toContain("only clear everyday actions run right away");
    expect(readme).toContain("Each automation or script delete is confirmed on its own");
    expect(readme).toContain("the AI can see it too, so it only counts when you type it yourself");
    expect(readme).toContain("ha-nova setup confirm-window off");
    expect(readme).toContain("or a tap on your iPhone or Android phone.");
    expect(readme).toContain("You pair with a six-digit code:");
    // Chat apps: "Always allow" by phone; only setting up HA NOVA itself stays on a computer; Link sign-in (0.28).
    expect(readme).toContain('With a linked phone, set every tool to "Always allow"');
    expect(readme).toContain('No phone? Set every tool to "Always allow", except "Run a confirmed change"');
    expect(readme).toContain("Only setting up HA NOVA itself (setup, pairing, updates, turning chat apps on or off) runs on a computer.");
    expect(readme).toContain("That only changes which login Link keeps on this computer: the owner still connects it once");
    expect(readme).not.toContain("Codex CLI");
    // claude.ai's connector list lives under Customize, not Settings.
    expect(readme).toContain("open Customize → Connectors → HA NOVA");
    expect(readme).not.toContain("Settings → Connectors");
    expect(readme).toContain("ChatGPT (web, on a computer; on a phone it can only answer)");
    expect(readme).toContain('No form in Claude? In Claude, open Customize → Connectors, click +, then Add custom connector, and paste the address. On a Claude Team or Enterprise plan, the owner of your Claude plan adds it once for everyone, with "Add for my organization".');
    expect(readme).toContain("https://hanova.app/demo/");
    // Only Pro was tested for changes in ChatGPT; Plus stays unnamed.
    expect(readme).toContain("ChatGPT needs a paid plan that lets you add your own plugins. We tested Pro. The free plan can't. On a phone, ChatGPT can only answer.");
    expect(readme).not.toMatch(/\bPlus\b/);
    for (const doc of ["docs/reference/chat-apps.md", "docs/reference/hosted-demo.md"]) {
      expect(readFileSync(doc, "utf8")).not.toMatch(/\bPlus\b/);
    }
    expect(readme).toContain("34 task skills plus the HA NOVA context skill");
    expect(readme).not.toContain("ha-nova mcp");
    expect(readme).not.toContain("ha_apply");
    expect(readme).not.toContain("Local MCP clients");
    expect(clientIntegration).toContain("The local stdio MCP adapter is supported.");
    expect(clientIntegration).not.toContain("Experimental — not supported yet");
    expect(clientIntegration).toContain("**A phone is linked for this computer:**");
    expect(clientIntegration).toContain("**No linked phone, even with a confirmation window:**");
    expect(clientIntegration).toContain("A window can fail to open and fall back to a chat code the AI can see.");
    expect(clientIntegration).toContain("keep `ha_apply` (“Run a confirmed change”) on “Needs approval”");
    expect(readme).not.toContain("Every change is previewed and approved first");
    expect(readme).not.toContain("A reviewed batch delete takes one code");
    expect(readme).not.toMatch(/census/i);
    expect(readme).toContain(
      "Optional remote access with Home Assistant Link (Beta)",
    );
    expect(readme).toContain("#optional-remote-access-with-home-assistant-link-beta");
    // User copy says "Home Assistant Link"; the old name and Nabu Casa appear once, as the explanation.
    expect(readme).toContain("(formerly Home Assistant Cloud; Nabu Casa)");
    expect(readme.split("Home Assistant Cloud").length - 1).toBe(1);
    expect(readme).toContain("Local only");
    expect(readme).toContain("automatic remote fallback");
    expect(readme).toContain("HA NOVA runs no additional public tunnel");
    expect(readme).toContain("ha-nova cloud add");
    expect(readme).not.toContain("ha-nova cloud add --server default");
    expect(readme).toContain("finish over SSH with `ha-nova cloud add --remote-resume`");
    expect(readme).toContain("Headless, WSL");
    expect(readme).toContain(
      "OAuth authorization in this computer's native credential store",
    );
    expect(readme).toContain("Optional Link mode uses your own Home Assistant Link service");
    expect(readme.split("Nabu Casa").length - 1).toBe(1);
    expect(readme).not.toContain(
      "Your Home Assistant credentials never leave the server",
    );
    expect(privacy).toContain(
      "available\nonly in release-gated desktop Beta builds",
    );
    expect(safety).toContain(
      "Every publication carries signed bundle provenance and passes the three-OS",
    );
  });

  it("keeps every curated stable highlight below the 220-character digest cap", () => {
    expect(stableNotes).toBeTruthy();
    const bullets = stableNotes?.match(/^    - (.+)$/gm) ?? [];
    expect(bullets.length).toBeGreaterThan(0);
    for (const bullet of bullets) {
      expect([...bullet.slice(6)].length).toBeLessThanOrEqual(220);
    }
  });
});
