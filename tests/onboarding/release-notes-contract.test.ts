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

  it("keeps v0.28.1 release-facing wording aligned to the shipped stack", () => {
    const relayVersion = /^version:\s*"([^"]+)"\s*$/m.exec(readFileSync("nova/config.yaml", "utf8"))?.[1] ?? "";
    // Any App version the notes name is the shipped App.
    const notedApps = [...(stableNotes ?? "").matchAll(/NOVA (?:Relay |App to )(\d+\.\d+\.\d+)/g)];
    expect(notedApps.length, "notes name the App version chat apps need").toBeGreaterThan(0);
    for (const [, noted] of notedApps) {
      expect(noted, "notes name the shipped NOVA Relay version").toBe(relayVersion);
    }
    // The App update leads the notes; without New Features the update notice leads with the first bug fix (#957).
    expect(stableNotes).toMatch(/^\*\*Update the NOVA App to \d+\.\d+\.\d+\.\*\*/);
    expect(stableNotes).not.toContain("## New Features");
    expect(stableNotes).toMatch(/## Bug Fixes\s+- \*\*Works with Home Assistant 2026\.11's Marketplace\.\*\*/);
    // Marketplace installs and updates stay in Home Assistant's UI (skills/hacs/hacs-commands.md, Marketplace mode).
    expect(stableNotes).toContain("it shows what's installed and the steps in Home Assistant for the rest.");
    // Typed actions take the person's own confirmation: window, phone, or code, whichever applies.
    expect(stableNotes).not.toContain("one-time code");
    // App 0.13.19 keeps the 0.28.0 tool text (TestMCPToolTextStable), so ChatGPT needs no Refresh and Claude doesn't ask again.
    expect(stableNotes).toContain("Settings > Apps > NOVA Relay > Update");
    expect(stableNotes).not.toContain("chatgpt.com/plugins");
    expect(stableNotes).not.toContain("Claude asks once more");
    // A series that loses dates takes the delete code (#954); a reorder goes alone (#940).
    expect(stableNotes).toMatch(/## What To Watch\s+- \*\*Shortening a repeating calendar event takes your own confirmation, like a delete\.\*\*/);
    expect(stableNotes).toMatch(/## What To Watch[\s\S]*- \*\*A shopping-list reorder goes on its own\.\*\*[\s\S]*## Bug Fixes/);
    // Link confirmations (#944, #947), cut impact checks (#951, #952), helper lists (#943, #948), passwords (#941, #949, #958), calendar previews (#945), Go 1.26.10 (#953).
    expect(stableNotes).toContain("**Confirmations over Home Assistant Link no longer give up after 5 s.**");
    expect(stableNotes).toContain("**Changes to an automation many others react to go through.**");
    // Only a failure that sent nothing says nothing changed (CONFIRMED_NOT_SENT); a cut check keeps its known limit.
    expect(stableNotes).toContain("When a change you confirmed never reached Home Assistant, HA NOVA now says so: nothing changed.");
    expect(stableNotes).toContain("A change in what the preview checked still needs a new one.");
    expect(stableNotes).toContain("**Changing a dropdown helper or a schedule keeps its other options and times.**");
    expect(stableNotes).toContain("**Passwords stay out of App settings and saved web addresses.**");
    expect(stableNotes).toContain("**Calendar previews describe repeating events correctly.**");
    expect(stableNotes).toContain("**Security updates.** HA NOVA and the chat-app part of the NOVA App are built with Go 1.26.10");
    // Chat apps are named as such; the MCP adapter itself stays unadvertised.
    expect(stableNotes).not.toMatch(/mcp/i);
  });

  it("keeps the README aligned to the shipped stack", () => {
    expect(readme).not.toContain("run `ha-nova setup` after the install");
    expect(readme).toContain('**Run setup again any time.** "Your setup" then shows what is done');
    expect(readme).toContain("clear everyday actions run right away, like \"turn off the kitchen light\", switching a room's lights");
    expect(readme).toContain("Up to 20 automation and script changes and deletes can share one confirmation");
    expect(readme).toContain("Answered on the phone, the phone shows the result; answered on the computer, HA NOVA withdraws the phone's question.");
    expect(readme).toContain("the owner picks who else may use them, at Ask only, Use the home, or Manage everything");
    expect(readme).not.toContain("Only the Home Assistant owner can turn chat apps on and use them.");
    // The old shared access: only what 0.28 does, no release it ends with (the App accepts it locally, never over Link).
    expect(readme).toContain("Still on the old shared access with the NOVA App? It still works on the local connection, not through Home Assistant Link.");
    for (const doc of [readme, readFileSync("nova/DOCS.md", "utf8")]) {
      expect(doc).not.toContain("last release that accepts");
    }
    expect(readme).toContain("In the chat, the AI sees the code too and HA NOVA can't tell who typed it");
    expect(readme).toContain("ha-nova setup confirm-window off");
    expect(readme).toContain("or a tap on your iPhone or Android phone.");
    expect(readme).toContain("You pair with a six-digit code:");
    // Chat apps: "Always allow" by phone; only setting up HA NOVA itself stays on a computer; Link sign-in (0.28).
    expect(readme).toContain('With a linked phone, set every tool to "Always allow"');
    expect(readme).toContain('No phone? Set every tool to "Always allow", except "Run a confirmed change"');
    expect(readme).toContain("Only setting up and updating HA NOVA (setup, pairing, updates) and InfluxDB history stay outside the chat. A setup or repair step that asks for a password, PIN, or key finishes in Home Assistant.");
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
