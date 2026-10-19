# Session Bootstrap

Apply this contract before the first Home Assistant task in a session.
Cited ../<x>/<file> paths are relative to the citing file.

## Check

1. Before the selected server profile's
   first Home Assistant or HA NOVA task, and before any relay command or typed
   verb, run `ha-nova check-update --quiet` in the same shell command
   as that first call: `ha-nova check-update --quiet; ha-nova call …` (a
   `HA_NOVA_SERVER=<name>` prefix covers only the command it precedes: repeat
   it). Once a typed verb or relay command for that server answered, a
   bare `ha-nova relay health` preflight is done; a skill reading its output
   still runs it.
2. Run one check for every server profile in this session, with its
   `HA_NOVA_SERVER=<name>`; empty or failed counts, and empty stays silent.
3. Later status arrives in typed verbs' `notices[]` (`update`,
   `relay_outdated`, `demo`) and `[ha-nova]` relay lines — never a substitute
   for step 1. Never repeat a notice already surfaced in this session.
   `HA_NOVA_SERVER=demo`: the shared demo house, never the user's home.

## HA NOVA Update Callout

After the requested result, surface an available HA NOVA update exactly once
per session as a separate localized callout: installed → latest version, up to three supplied highlight lines, the supplied release URL, and
an offer to update. Never update without
consent. After a successful `ha-nova update`, tell the
user to start a new AI-client session.

## Relay Update Callout

After the requested result, surface a Relay warning exactly once per selected
server profile as a separate localized callout:

- NOVA Relay App: name the supplied installed and available
  versions, say the Relay restarts, and ask whether to prepare the guided
  update. A yes only invokes `ha-nova:updates`, which must
  show its App-update preview and obtain
  confirmation before installing with a partial backup.
- A standalone Container/Core Relay gets no guided install: point to the
  manual image pull and container recreation.
