# Session Bootstrap

Apply this contract before the first Home Assistant task in a session,
regardless of which HA NOVA skill was loaded directly.

## Check

1. Before the selected server profile's first Home Assistant or HA NOVA task,
   and before any relay command, run `ha-nova check-update --quiet`.
   SessionStart context and background `--quiet --json` refreshes do not
   replace this human-output check.
2. Run one check for every server profile used in this agent session. For a
   non-default profile, apply the same `HA_NOVA_SERVER=<name>` selection used
   by the task. Remember a completed profile check even when it is empty or
   fails.
3. Remember complete HA NOVA update blocks (`Update available`, `Return to
   stable`, `HA NOVA update available`, or `HA NOVA return to stable`,
   regardless of capitalization) and Relay blocks (`Relay outdated` or `Relay
   update available`).
4. Continue the requested task even when the advisory check is empty or fails;
   either outcome counts as this session's one attempt. An update notice never
   replaces or interrupts the user's request. Empty output stays silent.
5. Treat later `[ha-nova]` notices from relay commands as the same session
   status. Never repeat a notice already surfaced in this session.

## HA NOVA Update Callout

After the requested result, surface an available HA NOVA update exactly once
as a separate localized callout:

- installed version → latest version
- up to three supplied highlight lines
- supplied release URL
- offer `ha-nova update`

Surface the HA NOVA update callout at most once for the whole agent session,
even after a server-profile switch. Never update without
consent. After a successful `ha-nova update`, tell the user to start a new
AI-client session so the refreshed skills take effect.

## Relay Update Callout

After the requested result, surface a Relay warning exactly once per selected
server profile as a separate localized callout:

- For the Home Assistant NOVA Relay App, name the installed and available
  versions when supplied, say that the Relay restarts, and ask whether to
  prepare the guided update. A yes is only permission to invoke
  `ha-nova:updates`; that skill must show its App-update preview and obtain
  confirmation before installing with a partial backup.
- For a standalone Container/Core Relay, do not offer a guided install. Point
  to the manual image pull and container recreation instead.
