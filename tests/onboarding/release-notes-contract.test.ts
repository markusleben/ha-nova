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

  it("keeps v0.36.0 release-facing wording aligned to the shipped stack", () => {
    const relayVersion = /^version:\s*"([^"]+)"\s*$/m.exec(readFileSync("nova/config.yaml", "utf8"))?.[1] ?? "";
    // Any App version the notes name is the App in nova/config.yaml: the App promote PR's `npm run bump -- --relay` sets both.
    const notedApps = [...(stableNotes ?? "").matchAll(/NOVA (?:Relay |App to )(\d+\.\d+\.\d+)/g)];
    expect(notedApps.length, "notes name the App version chat apps need").toBeGreaterThan(0);
    for (const [, noted] of notedApps) {
      expect(noted, "notes name the shipped NOVA Relay version").toBe(relayVersion);
    }
    // The role check on a computer an administrator connected (MAR-170) runs in the App, and the App's check runs the bundled CLI, so the App update leads.
    expect(stableNotes).toMatch(/^\*\*Update the NOVA App to \d+\.\d+\.\d+\.\*\* Much of this release runs in the App, so update it and HA NOVA on every computer together\./);
    expect(stableNotes).toContain("Settings > Apps > NOVA Relay > Update");
    expect(stableNotes).toContain("Then run `ha-nova update` on each computer.");
    // No MCP tool text changed in 0.36, so ChatGPT needs no Refresh.
    expect(stableNotes).not.toContain("chatgpt.com/plugins");
    // A 0.31 Mac that skipped 0.32 still re-pairs once, and its CLI's notice shows the first New Features bullet and its JSON the first action item (0.32 spec §5.1); cli/release_digest_mac_repair_test.go proves it.
    expect(stableNotes).toMatch(/## New Features\s+- \*\*Updating a Mac from 0\.31\? HA NOVA asks you to pair it again once\.\*\*/);
    expect(stableNotes).toMatch(/## What To Watch\s+- \*\*Updating a Mac from 0\.31\? HA NOVA asks you to pair it again once\.\*\*/);
    expect(stableNotes).toContain('choose "Allow", not "Always Allow"');
    // 0.36: faster plans (MAR-280, #1299), the dead-reference sweep (#978, #1300; computer only: chat apps keep the per-id check), and the naming-scheme rename (#979, #1301).
    expect(stableNotes).toContain("**On a slow connection, planning a light call takes less than half the time.**");
    expect(stableNotes).toContain("**One check finds what your automations use that no longer exists.**");
    expect(stableNotes).toContain("On a computer, it reads your automations and scripts and lists");
    expect(stableNotes).toContain("**Rename up to 19 entities to one naming scheme with one confirmation.**");
    expect(stableNotes).toContain("**Renaming an entity id stops what uses the old id until it is updated.**");
    // Role loss (MAR-170, #1298): computers connected before 0.36 keep access until reconnected, and an App rollback disconnects the new ones.
    expect(stableNotes).toContain("**A computer connected with an administrator's code now works only while that person stays an administrator.**");
    expect(stableNotes).toContain("For a computer connected before 0.36, revoke it on the NOVA page and connect it again.");
    expect(stableNotes).toContain("**Going back to an older NOVA App disconnects the computers an administrator connected after the update.**");
    // Empty answers and the listener recheck fail closed (MAR-272, #1299).
    expect(stableNotes).toContain("**Now and then an everyday call asks for your OK.**");
    // The plan hash changed (#1288): a plan made before the update asks for a fresh preview.
    expect(stableNotes).toContain("**Some saved plans need a fresh preview after updating.**");
    // A live-template trigger still asks, and the plan names it (#1286, decision A).
    expect(stableNotes).toContain("**Near a thermostat's heater, an automation whose trigger picks its entity with a live template still makes calls ask.**");
    // The standalone container's token-only pairing (R10, #1263) is a behavior change a container user must know.
    expect(stableNotes).toContain("**The standalone container connects with its token only.**");
    expect(stableNotes).toContain("RELAY_AUTH_TOKEN");
    // Earlier trade-offs still reach a computer that updates straight from 0.31 to 0.35.
    expect(stableNotes).toContain("**YAML thermostat checks need the NOVA App's file access on.**");
    expect(stableNotes).toContain("**A computer paired before 0.32 pairs again once to confirm with one click.**");
    expect(stableNotes).toContain("**One-click confirming trusts that computer.** On Windows and Linux, any program running as you could confirm without a click.");
    expect(stableNotes).toContain("**Your AI assistant sees the chat code too, and HA NOVA can't tell who typed it.**");
    expect(stableNotes).toContain("**The chat code needs the new App and HA NOVA, and works where your AI runs `ha-nova apply`.**");
    expect(stableNotes).toContain("**Update the NOVA App and HA NOVA together.** HA NOVA 0.32 refuses a change it would ask for but the new App wouldn't");
    expect(stableNotes).toContain("**A listener HA NOVA can't name on the logbook's events no longer makes a call ask.**");
    expect(stableNotes).toContain("**A key kept in a file trusts every program running as you.**");
    // Fixes: the chat role under the lock (MAR-181, #1298), every rename bound (#1301), unread reference lists (#1301).
    expect(stableNotes).toContain("**A change in Claude or ChatGPT uses the person's Home Assistant role as it is when the change runs.**");
    expect(stableNotes).toContain("**A plan that renames several entities checks each one right before its write.**");
    expect(stableNotes).toContain("**Finding where an entity is used no longer reads a broken answer as \"nowhere\".**");
    // Chat apps are named as such; the MCP adapter itself stays unadvertised.
    expect(stableNotes).not.toMatch(/mcp/i);
  });

  it("keeps the README aligned to the shipped stack", () => {
    expect(readme).not.toContain("run `ha-nova setup` after the install");
    expect(readme).toContain('**Run setup again any time.** "Your setup" then shows what is done');
    expect(readme).toContain("clear everyday actions run right away, like \"turn off the kitchen light\", switching a room's lights");
    expect(readme).toContain("Up to 20 automation and script changes and deletes can share one confirmation");
    // The safety check (0.30): the phone or the NOVA page approves; from 0.32 the owner's own computer confirms in its window, and every other window only waits.
    expect(readme).toContain("**In the window on your own computer**, click Confirm (OK on Windows) or Cancel. Your phone then asks only when nobody confirms at the computer.");
    expect(readme).toContain("On any other computer, the window only waits: it says where to confirm, opens this request on the NOVA page, or cancels.");
    expect(readme).toContain("On Windows and Linux, any program running as you can also read HA NOVA's key and confirm without a click.");
    expect(readme).not.toContain("**The window on your computer only waits.**");
    expect(readme).not.toContain("Is this your own computer?");
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
    expect(readme).toContain("You OK them with one click in a window on your own computer, on the NOVA page in Home Assistant, or with a tap on your iPhone or Android phone.");
    // The Mac re-pair and Link sign-out (0.32 spec §5).
    expect(readme).toContain("Coming from 0.31 on macOS? HA NOVA asks you to pair this computer again once: run `ha-nova setup` when HA NOVA says so");
    expect(readme).toContain("You pair with a six-digit code:");
    // 0.36: a computer an administrator connected works only while they are one (MAR-170), the dead-reference sweep (#978), and the naming-scheme rename (#979).
    expect(readme).toContain("A computer connected with an administrator's code works only while that person is still an administrator.");
    expect(readme).toContain('*"Do my automations use anything that no longer exists?"*');
    expect(readme).toContain('*"Rename all bedroom lights to one naming scheme"*');
    // 0.33: the chat code for a computer nobody sits at, the phone offer, and setup --server on the page.
    expect(readme).toContain("**On a computer nobody sits at**, with no phone, the Home Assistant owner can turn on a chat code for it on the NOVA page.");
    expect(readme).toContain("Your AI assistant sees that code too, and so does any program using this computer's key");
    expect(readme).toContain("in a terminal offer a phone until one is linked");
    expect(readme).toContain("`ha-nova setup --server <name>` sets it up again, on the setup page where a browser can open");
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
