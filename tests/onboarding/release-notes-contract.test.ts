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

  it("keeps v0.30.0 release-facing wording aligned to the shipped stack", () => {
    const relayVersion = /^version:\s*"([^"]+)"\s*$/m.exec(readFileSync("nova/config.yaml", "utf8"))?.[1] ?? "";
    // Any App version the notes name is the App in nova/config.yaml: the App promote PR's `npm run bump -- --relay` sets both.
    const notedApps = [...(stableNotes ?? "").matchAll(/NOVA (?:Relay |App to )(\d+\.\d+\.\d+)/g)];
    expect(notedApps.length, "notes name the App version chat apps need").toBeGreaterThan(0);
    for (const [, noted] of notedApps) {
      expect(noted, "notes name the shipped NOVA Relay version").toBe(relayVersion);
    }
    // The App update leads, with the computers' update (the safety check, #1057); the update notice shows the first New Features bullet (calm edits, #1053).
    expect(stableNotes).toMatch(/^\*\*Update the NOVA App to \d+\.\d+\.\d+\.\*\* It now checks every change a computer sends, so update HA NOVA on every computer with it\./);
    expect(stableNotes).toMatch(/## New Features\s+- \*\*Editing one of your own automations or scripts now runs right away when HA NOVA can put it back\.\*\*/);
    expect(stableNotes).toContain("Settings > Apps > NOVA Relay > Update");
    expect(stableNotes).toContain("Settings > ⋮ (top right) > Check for updates");
    expect(stableNotes).toContain("Then run `ha-nova update` on each computer.");
    // ha_read's description changed (#1102): Claude asks once more for that tool; ChatGPT needs a Refresh (#923 reminds in the chat and on the NOVA page).
    expect(stableNotes).toContain('In Claude, "Advanced read" asks once more');
    expect(stableNotes).toContain("open chatgpt.com/plugins, choose HA NOVA, then Refresh.");
    expect(stableNotes).toContain("**ChatGPT users hear when to refresh HA NOVA.**");
    // The safety check (#1057, #1082, #1087, #1097): approvals on the phone or the NOVA page; the window only waits; the phone moves once.
    expect(stableNotes).toContain("**Big changes wait for your OK on your linked phone or on the NOVA page.**");
    expect(stableNotes).toContain("**The window on your computer only waits.**");
    expect(stableNotes).toContain("**Your linked phone moves into Home Assistant once.**");
    expect(stableNotes).toContain("**Going back to an older NOVA App:**");
    expect(stableNotes).toContain("Chat apps there can't make a change that needs an OK."); // #1114
    // The old shared access ends (#905, #1115): the re-pair step and its known limits.
    expect(stableNotes).toMatch(/## What To Watch\s+- \*\*The NOVA App no longer accepts the old shared key\.\*\*[^\n]*run `ha-nova setup` once on it \(a `--service` install: `ha-nova pair --credential-store=file`\)/);
    expect(stableNotes).toContain("Remove Link first, then run setup, then add Link again.");
    expect(stableNotes).toContain("a Home Assistant backup from before this update does.");
    expect(stableNotes).toContain('Its "Not now" leads to a key field the App no longer has.');
    // Logs stay in Home Assistant (#1056); file writes and backup locations are typed (#1046).
    expect(stableNotes).toMatch(/## What To Watch[\s\S]*- \*\*HA NOVA no longer reads Home Assistant's logs, which can hold passwords and keys\.\*\*[\s\S]*## Bug Fixes/);
    expect(stableNotes).toContain("Settings > System > Logs");
    expect(stableNotes).toContain("changing where backups go, now asks for the same confirmation as unlocking a door.");
    // www media (#1116, MAR-128).
    expect(stableNotes).toContain("**HA NOVA can now list and delete the pictures and videos in your www folder.**");
    // 0.29 carried over (no 0.29.0 was released): renames (#1054), shopping list (#1048), setup offer (#1045), stdio apply jobs (#1047), copies first (#1042, #1044, #1049), helpers (#1051), reloads (#1055), same-item edits (#1058), the App notice over Link (#1064), file refusals (#1072), confirmation windows (#1083, #1107), Marketplace updates refused up front (#1096), room/floor/label triggers (#1094).
    expect(stableNotes).toContain("**Renaming a device or entity now runs right away and can be undone.**");
    expect(stableNotes).toContain("**Automations that react to a whole room, floor, or label are now checked.**");
    expect(stableNotes).toContain("**Removing up to five items you named from the shopping list runs right away.**");
    expect(stableNotes).toContain("**When a NOVA App update is waiting, setup in a terminal offers it once setup works.**");
    expect(stableNotes).toContain("**Fewer prompts in Claude Code: setup can allow HA NOVA's read commands once.**");
    expect(stableNotes).toContain("Asking again gets the result, and nothing runs twice.");
    expect(stableNotes).toContain("only once a copy of it is saved.");
    expect(stableNotes).toContain("**Helper and dashboard-name changes are now checked before they are saved");
    expect(stableNotes).toContain("**Reloading an automation or your scripts is now checked like what it loads.**");
    expect(stableNotes).toContain("**Changing the same automation or script again right away no longer asks.**");
    expect(stableNotes).toContain("**Over Home Assistant Link, `ha-nova update` now offers the NOVA App update too.**");
    expect(stableNotes).toContain("**When the NOVA App refuses a file (for example anything under /config/custom_components), HA NOVA now says why");
    expect(stableNotes).toContain("**Confirmation windows are short, wide, and readable.** Same-kind steps share one line");
    expect(stableNotes).toContain("**Updating a Marketplace item now stops before anything is sent and says where to do it.**");
    // Chat apps are named as such; the MCP adapter itself stays unadvertised.
    expect(stableNotes).not.toMatch(/mcp/i);
  });

  it("keeps the README aligned to the shipped stack", () => {
    expect(readme).not.toContain("run `ha-nova setup` after the install");
    expect(readme).toContain('**Run setup again any time.** "Your setup" then shows what is done');
    expect(readme).toContain("clear everyday actions run right away, like \"turn off the kitchen light\", switching a room's lights");
    expect(readme).toContain("Up to 20 automation and script changes and deletes can share one confirmation");
    // The safety check (0.30): the phone or the NOVA page approves; the window only waits.
    expect(readme).toContain("**The window on your computer only waits.** It says where to confirm, opens Home Assistant, or cancels; it can't confirm.");
    expect(readme).toContain('Under "Waiting for your OK", choose Open, then Approve or Cancel.');
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
