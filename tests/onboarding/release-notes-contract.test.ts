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
    // The App update leads the notes; the update notice still leads with the first feature.
    expect(stableNotes).toMatch(/^\*\*Update the NOVA App to \d+\.\d+\.\d+\.\*\*/);
    expect(stableNotes).toMatch(/\n {4}## New Features\s+- \*\*Chat apps do what HA NOVA on a computer does\.\*\*/);
    // Chat apps at CLI parity (#863, #864, #865, #867); only setup and InfluxDB history stay outside the chat.
    expect(stableNotes).toContain("files (with file access on), and more. Only setup and InfluxDB history stay outside the chat.");
    // "Always allow" advice by phone (#867).
    expect(stableNotes).toContain('with a phone linked for chat apps, every tool on "Always allow"; without one, all but "Run a confirmed change".');
    // Codex in the desktop app; stale copies flagged and moved (fix/codex-app-guidance).
    expect(stableNotes).toContain("**Setup finds Codex in the ChatGPT desktop app too.**");
    expect(stableNotes).toContain("Doctor flags an older HA NOVA copy that Codex would load; setup updates it, or moves HA NOVA's own old copies to a backup.");
    // HACS and dashboard resources take the typed confirmation (#864, maintainer decision 3).
    expect(stableNotes).toMatch(/## What To Watch\s+- \*\*HACS installs and updates, and dashboard resources, now take your own confirmation, like a delete\.\*\*/);
    expect(stableNotes).toContain("**Camera sign-in tokens stay hidden from the AI.**");
    expect(stableNotes).toContain("**A leftover Home Assistant Link is explained.**");
    expect(stableNotes).toContain("**Setup says which Claude gets HA NOVA.** Claude Code is the claude command and the Code tab in Claude Desktop.");
    expect(stableNotes).toContain("**Setup explains the owner sign-in.** The owner connects each computer once");
    // Every claude call has a deadline (#911); the update to 0.28.0 still runs the old binary's sync, so only later updates.
    expect(stableNotes).toContain("**A stuck Claude Code no longer hangs setup, doctor, or later updates.**");
    expect(stableNotes).toContain("**Disabling a lock's device is refused.**");
    // Household levels for chat apps (#900 and the NOVA page's list).
    expect(stableNotes).toContain("**Chat apps for everyone in your home.**");
    // Each person's phone, and moving or stopping a chat-app phone (#894, #897, #902).
    expect(stableNotes).toContain("**Each person confirms on their own phone.**");
    // Confirmations in Home Assistant's language (#890, #898, #901), batch confirmation (#896), everyday lines (#899).
    expect(stableNotes).toContain("**Confirmations in your home's language.**");
    expect(stableNotes).toContain("**One confirmation for up to 20 automation and script changes.**");
    expect(stableNotes).toContain("**Clearer confirmations for technical requests.**");
    // Answered on the computer, the phone's question goes away (#891); setup asks whose phone (#893).
    expect(stableNotes).toContain("**Answered on the computer, the phone's question disappears.**");
    expect(stableNotes).toContain("**Setup asks whose phone should confirm on this computer.**");
    expect(stableNotes).not.toContain("Setup proposes your own phone");
    // App 0.13.2 carries the 0.28.0 chat bridge; its tools changed, so ChatGPT needs a Refresh and Claude asks once more,
    // and "Always allow" keeps its phone condition there too (0.28 review).
    expect(stableNotes).toContain("Settings > Apps > NOVA Relay > Update");
    expect(stableNotes).toContain("Then in ChatGPT: open chatgpt.com/plugins, choose HA NOVA, then Refresh.");
    expect(stableNotes).toContain('Claude asks once more before HA NOVA\'s tools: set them again as the NOVA page says. With a phone linked for chat apps, every tool on "Always allow"; without one, all but "Run a confirmed change".');
    expect(stableNotes).not.toContain('set them to "Always allow" again');
    // HA UI parity (#873, #875-#877), Supervisor Apps and logs (#883), "Your setup" (#879), Claude Desktop alone (#878), 0.28 review fixes (#885).
    expect(stableNotes).toContain("**More of Home Assistant's settings.**");
    expect(stableNotes).toContain("**Apps and their logs, on HA OS.**");
    expect(stableNotes).toContain("not through Home Assistant Link.");
    expect(stableNotes).toContain('**"Your setup" when you run setup again.**');
    expect(stableNotes).toContain("**The NOVA Relay App asks for more Supervisor access.**");
    expect(stableNotes).toContain("**Moving a device with a lock to another room takes your own confirmation.**");
    expect(stableNotes).toContain("On a Mac, Claude Desktop alone is enough once its Code tab was opened.");
    expect(readme).toContain('**Run setup again any time.** "Your setup" then shows what is done');
    expect(stableNotes).not.toContain("which this release doesn't change");
    // Chat apps are named as such; the MCP adapter itself stays unadvertised.
    expect(stableNotes).not.toMatch(/mcp/i);
    expect(stableNotes).not.toContain("Cloud Remote is back on Windows and Linux");
    expect(readme).toContain("only clear everyday actions run right away");
    expect(readme).toContain("Up to 20 automation and script changes and deletes can share one confirmation");
    expect(readme).toContain("Answered on the phone, the phone shows the result; answered on the computer, HA NOVA withdraws the phone's question.");
    expect(readme).toContain("the owner picks who else may use them, at Ask only, Use the home, or Manage everything");
    expect(readme).not.toContain("Only the Home Assistant owner can turn chat apps on and use them.");
    // The old shared access: only what 0.28 does, no release it ends with (the App accepts it locally, never over Link).
    expect(readme).toContain("Still on the old shared access with the NOVA App? It still works on the local connection, not through Home Assistant Link.");
    for (const doc of [readme, readFileSync("nova/DOCS.md", "utf8")]) {
      expect(doc).not.toContain("last release that accepts");
    }
    expect(readme).toContain("the AI can see it too, so it only counts when you type it yourself");
    expect(readme).toContain("ha-nova setup confirm-window off");
    expect(readme).toContain("or a tap on your iPhone or Android phone.");
    expect(readme).toContain("You pair with a six-digit code:");
    // Chat apps: "Always allow" by phone; only setting up HA NOVA itself stays on a computer; Link sign-in (0.28).
    expect(readme).toContain('With a linked phone, set every tool to "Always allow"');
    expect(readme).toContain('No phone? Set every tool to "Always allow", except "Run a confirmed change"');
    expect(readme).toContain("Only setting up and updating HA NOVA (setup, pairing, updates) and InfluxDB history stay outside the chat.");
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
