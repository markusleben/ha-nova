# Setup re-run overview

Status: active

## Before

The working browser re-run shows Home Assistant, the App, this computer,
and one combined skills row. AI apps found since setup have a combined
“Set up … too” button. Below are the confirmation window (on/unavailable,
no turn-on action), this computer's phone (link/move/check again), chat
apps (off/on switch, Add to Claude and connector address when on), Claude
Code rules, and Link when eligible. A re-run cannot read the chat-app
phone without an owner sign-in; it currently gives code advice with a
check-on-the-NOVA-page hint. It cannot know whether Claude added a connector.

The terminal re-run shows connection/skills checks, offers Link, another
server, and Claude Code rules, then says everything is set up. It does not
list unwired AI apps, window settings, phones, or chat-app status. `doctor`
checks connectivity and configured AI apps, not unconfigured apps or these
optional settings. Human `status` prints version/install root/drift; its
JSON includes configured/detected/attached client flags, not optional steps.
The first-run home screen repeats chat-app links in its completion notes;
it isn't the working re-run screen.

## Overview

One “Your setup” overview on a working re-run, with the same optional-item
list in the terminal. Keep the existing connection checks and repair flow.
Use ✓ only for facts read successfully; otherwise “Not yet —” plus one
plain sentence, or “Not checked —” when the state cannot be read. Unknown
must never become off or done. Visiting the overview changes nothing.

- List each supported AI app found on this computer, individually. Read
  wiring with the existing readiness check; Set up reuses client installation.
- Confirmation window: on, off (Set up reuses `setup confirm-window on`),
  or unavailable in this desktop session.
- Phone for this computer: linked, not linked, no eligible phone, or unread.
  Set up expands the existing phone link/move/check controls.
- Chat apps: off or on, with the existing switch and restart/sign-in consent.
  When on, always say “Add to Claude if you haven't yet”; retain Add to
  Claude and the ChatGPT address. Never claim Claude has added it.
- Phone for chat apps: use the App's owner-phone state only. A re-run without
  owner sign-in says “Not checked” and links the existing NOVA page; a
  phone linked only on this computer proves nothing about chat apps.
- Home Assistant Link: show only after this home's remote access was found.
  Distinguish saved authorization from automatic fallback; preserve the
  existing eligibility checks and switch. Unsupported sessions get an
  explanation, not a non-working Set up action.
- Other AI apps: never infer installed or configured state. Say “Any app
  that supports MCP can use HA NOVA: add this to its MCP settings”, show the
  `mcpServers` stdio JSON with Copy, and print the same JSON in the terminal.
  Prefer the stable public binary's absolute path (Unix `~/.local/bin/ha-nova`,
  Windows the detected install root's `ha-nova.exe`), not a shell PATH or a
  resolved symlink into a replaceable bundle. For a portable install without
  that public binary, use the running executable's absolute path. JSON-escape
  paths, including spaces and Windows backslashes. Pin `--server` to the
  selected profile when several profiles exist. Never edit another app's
  configuration automatically.

Group the items under plain headings, in the same order on the page and in
the terminal: “Your home” (Home Assistant, NOVA App, this computer, Home
Assistant Link), “AI apps on this computer” (detected apps; apps not found
here fold into one “More AI apps” row with no action; then “Other AI apps”),
“Confirmations” (window, this computer's phone), and “Chat apps (Claude,
ChatGPT)” (chat apps, chat-app phone). Empty sections are left out.

The terminal prints the same states with existing commands as actions;
it does not add an interactive menu or another setup flow. Browser Set up
expands existing controls; switches still apply on Done. Existing Claude
Code rules remain available. No word/route budget increases.

This stays one change despite touching the renderer, catalog, terminal
completion hooks, and MCP help/docs: those are the consumers of the same
overview, not separately deployable runtime features.

The stdio MCP adapter is supported, as the shipped chat bridge's same tool
and safety paths. Document it in `docs/reference/client-integration.md` and
its command help. With a linked phone, users may Always allow the tools:
typed plans are decided there. Without a phone, including window-only
setups, keep the approval prompt for `ha_apply`: a window can fail to open
and fall back to a chat code the AI can see. This is the maintainer's
phone-based approval decision; no confirmation runtime change is included.

## Sign out — next release, specification only

Planned next-release item: add sign-out while keeping HA NOVA installed.

Use `ha-nova signout`, not uninstall's keep mode: standard uninstall
removes the binary, skills, and client wiring while **keeping** pairing,
the opposite of signing out. Purge removes those too and has broader
multi-server/Cloud teardown responsibilities; it is not a trivial reuse.

Copy: “Sign out of Home Assistant on this computer? HA NOVA and your AI
apps stay installed. This computer's access key is revoked in the NOVA
App and removed here. Run ha-nova setup to connect again.” Buttons:
“Sign out” / “Keep me signed in”. Completion: “Signed out. HA NOVA is
still installed. Run ha-nova setup when you want to connect again.”

Scope is the selected server (`--server` follows existing profile selection),
not every computer or the App. Reuse the existing verified device-retirement
machinery and lifecycle locking, not uninstall's file teardown. Revoke and
verify before deleting the local key; unreachable App or unreadable secure
store must leave recoverable state and say sign-out isn't complete. Retire
the selected profile's Cloud authorization too, so it cannot reconnect via
another route. Keep binary, skills, client wiring, and non-secret settings;
mark the profile signed out so the next setup pairs again. Do not implement
this auth/lifecycle contract as part of the overview.
