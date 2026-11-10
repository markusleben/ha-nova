# Next release body

Status: active

Unreleased claims wait here until they ship (AGENTS.md, Releases: the README describes the stable release only). Each item moves into the README or the release notes only when it is in a stable release.

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

## README for the release-prep PR

- **Logs** (#1061, already on `main`): the README's App-log example row and the updates skill row already say HA NOVA doesn't read logs. That holds for chat apps on App 0.13.24, but the terminal stops reading logs only with 0.30.0. Keep the wording when 0.30.0 ships.
