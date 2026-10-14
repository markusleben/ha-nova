# Session Bootstrap

Apply this contract before the first Home Assistant task in a session,
regardless of which HA NOVA skill was loaded directly.

## Check

1. Before the selected server profile's
   first Home Assistant or HA NOVA task, and before any relay command or typed
   verb, run `ha-nova check-update --quiet` (it also self-heals client
   integrations and checks for a Relay App update).
2. Typed verbs (`ha-nova find|get|history|plan|call|apply`) report later
   status in their JSON `notices[]` (`kind: update` or `relay_outdated`):
   the same status as step 1, never a substitute for it.
3. Run one check for every server profile used in this agent session, with the
   task's `HA_NOVA_SERVER=<name>` selection. Remember a completed check even
   when it is empty or fails; continue the requested task either way. Empty
   output stays silent.
4. `[ha-nova]` lines from relay commands are the same status. Never repeat a
   notice already surfaced in this session.

## HA NOVA Update Callout

After the requested result, surface an available HA NOVA update exactly once
per agent session as a separate localized callout: installed version → latest
version, up to three supplied highlight lines, the supplied release URL, and
an offer to run `ha-nova update`. Never update without
consent. After a successful `ha-nova update`, tell the
user to start a new AI-client session so the refreshed skills take effect.

## Relay Update Callout

After the requested result, surface a Relay warning exactly once per selected
server profile as a separate localized callout:

- Home Assistant NOVA Relay App: name the installed and available versions when
  supplied, say that the Relay restarts, and ask whether to prepare the guided
  update. A yes only invokes `ha-nova:updates`; that skill must
  show its App-update preview and obtain
  confirmation before installing with a partial backup.
- A standalone Container/Core Relay gets no guided install: point to the
  manual image pull and container recreation instead.
