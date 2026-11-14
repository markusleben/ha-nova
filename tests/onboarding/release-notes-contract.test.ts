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

  it("keeps v0.31.0 release-facing wording aligned to the shipped stack", () => {
    const relayVersion = /^version:\s*"([^"]+)"\s*$/m.exec(readFileSync("nova/config.yaml", "utf8"))?.[1] ?? "";
    // Any App version the notes name is the App in nova/config.yaml: the App promote PR's `npm run bump -- --relay` sets both.
    const notedApps = [...(stableNotes ?? "").matchAll(/NOVA (?:Relay |App to )(\d+\.\d+\.\d+)/g)];
    expect(notedApps.length, "notes name the App version chat apps need").toBeGreaterThan(0);
    for (const [, noted] of notedApps) {
      expect(noted, "notes name the shipped NOVA Relay version").toBe(relayVersion);
    }
    // The App update leads; the update notice shows the first New Features bullet (the Marketplace, D2, #1132).
    expect(stableNotes).toMatch(/^\*\*Update the NOVA App to \d+\.\d+\.\d+\.\*\* Much of this release runs in the App, so update it and HA NOVA on every computer together\./);
    expect(stableNotes).toMatch(/## New Features\s+- \*\*HA NOVA now installs, updates, and removes Marketplace items, each after your OK \(Home Assistant 2026\.11\+\)\.\*\*/);
    expect(stableNotes).toContain("Settings > Apps > NOVA Relay > Update");
    expect(stableNotes).toContain("Settings > ⋮ (top right) > Check for updates");
    expect(stableNotes).toContain("Then run `ha-nova update` on each computer.");
    // ha_map gained the updates, after_update, and checkup sections: ChatGPT needs a Refresh to see them.
    expect(stableNotes).toContain("open chatgpt.com/plugins, choose HA NOVA, then Refresh.");
    // The update advisor (#1137–#1139), check-up (#1148), log facts (#1141), the NOVA page (#1145, #1158), the window's button (#1161), the phone first (#1163, #1166), container chat (#1155), reads beside a change (#1149), room triggers (#1151), helper creates (#1133).
    expect(stableNotes).toContain('**Ask "Can I update now?" and get a checked answer.**');
    expect(stableNotes).toContain("**A system check-up says whether Home Assistant is OK");
    expect(stableNotes).toContain("**See which integration keeps failing, without HA NOVA reading the log.**");
    expect(stableNotes).toContain("**Every Home Assistant administrator can use the NOVA page:**");
    expect(stableNotes).toContain("**A clearer NOVA page and approval page that update on their own.**");
    expect(stableNotes).toContain("**The window on your computer opens the request in Home Assistant.**");
    expect(stableNotes).toContain("**Your phone asks sooner.**");
    expect(stableNotes).toContain("nothing is sent unless both pass.");
    expect(stableNotes).toContain("**Chat apps on the standalone container ask the phone linked to it**");
    expect(stableNotes).toContain("**In chat apps, reading your home no longer waits for a change in progress.**");
    // The pause (#1157) and the wider typed rating (#1134) are behavior changes.
    expect(stableNotes).toMatch(/## What To Watch\s+- \*\*Phone asks pause less often, and you can end a pause\.\*\*/);
    expect(stableNotes).toMatch(/## What To Watch[\s\S]*- \*\*More changes ask when an automation would react to them\.\*\*[\s\S]*## Bug Fixes/);
    // Fixes: large registries (#1170), UI thermostats (#1131), the guided App update (#1162), the bounded App check (#1165), Windows (#1130), the role re-read (#1160).
    expect(stableNotes).toContain("**In homes with thousands of entities, everyday changes such as a light no longer all ask on the phone.**");
    expect(stableNotes).toContain("**Fewer phone asks in homes with a thermostat or hygrostat made in Home Assistant's UI.**");
    expect(stableNotes).toContain("**The guided NOVA App update now asks for your OK instead of refusing**");
    expect(stableNotes).toContain("**After `ha-nova update`, the NOVA App check no longer hangs.**");
    expect(stableNotes).toContain("**Windows: `ha-nova update` retries a briefly locked install folder**");
    expect(stableNotes).toContain("**Chat apps check a user's role again right before a change their phone approved.**");
    // Repair and sub-entry flows asking shipped in 0.30.0 (#1152): not listed again.
    expect(stableNotes).not.toContain("Fixing a repair");
    // Chat apps are named as such; the MCP adapter itself stays unadvertised.
    expect(stableNotes).not.toMatch(/mcp/i);
  });

  it("keeps the README aligned to the shipped stack", () => {
    expect(readme).not.toContain("run `ha-nova setup` after the install");
    expect(readme).toContain('**Run setup again any time.** "Your setup" then shows what is done');
    expect(readme).toContain("clear everyday actions run right away, like \"turn off the kitchen light\", switching a room's lights");
    expect(readme).toContain("Up to 20 automation and script changes and deletes can share one confirmation");
    // The safety check (0.30): the phone or the NOVA page approves; the window only waits.
    expect(readme).toContain("**The window on your computer only waits.** It says where to confirm, opens this request on the NOVA page, or cancels; it can't confirm.");
    expect(readme).toContain('Under "Waiting for your OK", choose "Confirm or decline", then Approve or Cancel.');
    // Every administrator uses the NOVA page (0.31, #1145): codes and approvals, not only the owner.
    expect(readme).toContain('a Home Assistant administrator clicks **"Connect a device"**');
    expect(readme).not.toContain("Requests waiting for your OK");
    expect(readme).not.toContain("Answered on the phone, the phone shows the result");
    expect(readme).toContain("the owner picks who else may use them, at Ask only, Use the home, or Manage everything");
    expect(readme).not.toContain("Only the Home Assistant owner can turn chat apps on and use them.");
    // The old shared access ended with 0.30 (#905): the re-pair step, no "Revoke legacy access" left.
    expect(readme).toContain("The NOVA App no longer accepts the old shared key: a computer that still uses it says \"Pair this computer again\", so run `ha-nova setup` once on it (a `--service` install: `ha-nova pair --credential-store=file`).");
    expect(readme).not.toContain("Revoke legacy access");
    for (const doc of [readme, readFileSync("nova/DOCS.md", "utf8")]) {
      expect(doc).not.toContain("last release that accepts");
    }
    expect(readme).toContain("In the chat, the AI sees the code too and HA NOVA can't tell who typed it");
    expect(readme).toContain("ha-nova setup confirm-window off");
    expect(readme).toContain("You OK them on the NOVA page in Home Assistant, or with a tap on your iPhone or Android phone.");
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
