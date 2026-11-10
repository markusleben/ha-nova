# Next release body

Status: active

Unreleased claims wait here until they ship (AGENTS.md, Releases: the README describes the stable release only). Each item moves into the README or the release notes only when it is in a stable release.

## Migration notes for 0.30 (the NOVA App's safety check)

- **Update HA NOVA on every computer together with the NOVA App.** The App now checks every request a computer sends. An older HA NOVA on a computer can't get an OK through it: its unlocks, deletes, and other big changes are refused, and nothing is sent. Run ha-nova update on each computer. Everyday actions and previews keep working.

- **Approve big changes on your phone or on the NOVA page.** Unlocking, opening a door, gate, or garage, turning off the alarm, deletes, and file writes wait for an OK on your linked phone or in Home Assistant: open NOVA in the sidebar, then "Waiting for your OK". Any Home Assistant administrator can approve there, except the user the NOVA App signs in as. The OK covers exactly that change, once.

- **The window on your computer only waits.** It says where to confirm, opens Home Assistant, or cancels; it can't confirm. For these changes the four-digit code on a computer ends too. A dashboard change, a rooms change, and a change HA NOVA's instructions mark as needing your OK still ask on the computer as before, and the App may ask once more.

- **Your linked phone moves into Home Assistant once.** The first time a change needs your OK, the phone you linked on this computer asks whether it should get HA NOVA's confirmations. Allow it, then confirm the change itself. After that, `ha-nova setup confirm-phone` links or changes the phone in Home Assistant.

- **Your AI app may say it is waiting for your OK.** The change hasn't been sent yet. Approve it, then tell your AI app to go on; nothing runs twice.

- **Saving an automation or script no longer reloads it from HA NOVA.** Home Assistant reloads it after the save, as it does for a save in its own editor, and HA NOVA then checks, without changing anything, that Home Assistant runs the saved version. A delete still reloads.

- **Claude asks again before "Advanced read".** Its description changed (it no longer offers logs), so the "Always allow" you chose for that tool resets once: choose "Always allow" again. In ChatGPT, open chatgpt.com/plugins, choose HA NOVA, then Refresh. Chat apps otherwise keep their own confirmations: the linked phone, or a code in the chat at Manage everything.

- **UI thermostats may ask more often.** On an App with the safety check, HA NOVA can't read the heater setting of a thermostat you made in Home Assistant's UI, so changing such a thermostat asks on your phone. This is more often than needed, never less; a later release fixes it.

- **Going back to an older NOVA App:** computers that saw the safety check refuse unlocks, deletes, and other big changes until you update the App again.

- **Standalone container:** it has no NOVA page, so only a linked phone confirms. Give it the token of a Home Assistant user who isn't the owner, keep `/data` on a volume, and run it with a port mapping, not host networking (`docs/reference/relay-container.md`).

## New in 0.30

- **ChatGPT users hear when to refresh HA NOVA.** When a NOVA App update changes HA NOVA's tools, each chat-app user is told once in the chat, and the NOVA page shows it for 30 days: open chatgpt.com/plugins, choose HA NOVA, then Refresh.

## Carried over from 0.29.0

0.29.0 was not released: its release candidates (v0.29.0-rc1 to rc4) stay, and everything in them ships with 0.30.0. The 0.30.0 notes in `.goreleaser.yml` cover these items next to 0.30's own, with the App version 0.30 ships. The NOVA App 0.13.24 that the release candidates brought is already live, so the App lines below describe what chat-app users have now; the terminal part reaches stable users only with 0.30.0.

App notice (the version changes with 0.30): Claude and ChatGPT get the release's changes through the NOVA App, and HA NOVA on the computer asks for it too. Using HA NOVA in ChatGPT: after the update, open chatgpt.com/plugins, choose HA NOVA, then Refresh. From 0.30.0 on, the terminal reminds you after an App update.

### New Features

- **Editing one of your own automations or scripts now runs right away when HA NOVA can put it back.** It says so, and "undo" restores it. New ones, deletes, and anything that unlocks still ask.
- **Changing the same automation or script again right away no longer asks.** "Undo" takes back the latest change.
- **Renaming a device or entity now runs right away and can be undone.** That covers its name and voice aliases. Locks, alarms, doors, gates, and garages still ask.
- **Removing up to five items you named from the shopping list runs right away.**
- **When a NOVA App update is waiting, setup in a terminal offers it once setup works.** It asks first and uses the same safety check as `ha-nova update`.
- **Fewer prompts in Claude Code: setup can allow HA NOVA's read commands once.** You still confirm every unlock or delete yourself.
- **Long changes from other AI apps on your computer keep running past the app's time limit.** That covers apps added through setup's "Other AI apps" row. Asking again gets the result, and nothing runs twice.

### What To Watch

- **HA NOVA no longer reads Home Assistant's logs, which can hold passwords and keys.** When a log would help, your AI asks you to open Settings > System > Logs and share only the lines that matter.
- **Writing a file in your Home Assistant config folder, or changing where backups go, now asks for the same confirmation as unlocking a door.**

### Bug Fixes

- **Automations that react to a whole room, floor, or label are now checked.** Turning on a lamp that sets one off asks first when that automation unlocks or opens something; before, it could run without asking.
- **Confirmation windows are short and readable.** Same-kind steps share one line, names and months replace ids and timestamps, and the instruction always shows. Too long for the window: refused, never cut.
- **Changes that would drop aliases or labels, a person's trackers, or voice-assistant settings now save a copy first.**
- **HA NOVA overwrites or deletes a file in your config folder only once a copy of it is saved.** It stops if the file changed since the preview.
- **Helper and dashboard-name changes are now checked before they are saved, and a helper change can be undone.**
- **Reloading an automation or your scripts is now checked like what it loads.** A harmless reload no longer asks like unlocking a door; one that can unlock still does. A change also checks what its reload sets off.
- **When the NOVA App refuses a file (for example anything under /config/www), HA NOVA now says why instead of reporting a sign-in problem.**
- **Over Home Assistant Link, `ha-nova update` now offers the NOVA App update too.** In a terminal it installs the App after your yes, waits while it restarts, and checks the new version.
- **Updating a Marketplace item now stops before anything is sent and says where to do it.** HA NOVA can't install Marketplace updates yet: Home Assistant asks for its warning to be accepted first.

## 0.30.0: the old shared access ends (#905)

Breaking, decided by the maintainer (10.11.): the NOVA App no longer accepts the old shared key ("Relay Auth Token"). The standalone container keeps its own token.

### What To Watch

- **The NOVA App no longer accepts the old shared key.** A computer that still uses it says "Pair this computer again": run `ha-nova setup` once on it (a `--service` install: `ha-nova pair --credential-store=file`). With Home Assistant Link saved, remove Link first, then run setup, then add Link again. Computers that already paired, and chat apps, are not affected.
- **The App's "Relay Auth Token" and "Home Assistant token" options are gone, and so is "Revoke legacy access" on the NOVA page.** The App removes the old key's record on its first start, so going back to an earlier App version does not bring the shared key back. Restoring a Home Assistant backup from before this update does.
- **HA NOVA 0.28.3 or older on a computer, after the App update:** it reports a sign-in problem and offers `ha-nova setup`; choose to switch there. Its "Not now" leads to a key field the App no longer has.

### App changelog (release-prep PR)

- Upgrade notes line for the App version 0.30 ships: "Computers that still use the shared key (Relay Auth Token) must pair again: run ha-nova setup once on each (a --service install: ha-nova pair --credential-store=file)." This is the only text a user sees when Home Assistant updates the App before the computer.

## README for the release-prep PR

- **Shared access** (#905): line 116 ("The standalone container uses one shared token instead") stays true. Line 212 ("Upgrading from an earlier HA NOVA?") describes the old shared access as still working and names "Revoke legacy access": replace it with the "What To Watch" item above.

- **Logs** (#1061, already on `main`): the README's App-log example row and the updates skill row already say HA NOVA doesn't read logs. That holds for chat apps on App 0.13.24, but the terminal stops reading logs only with 0.30.0. Keep the wording when 0.30.0 ships.
