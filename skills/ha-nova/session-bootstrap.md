# Session Bootstrap

Apply this contract before the first Home Assistant task in a session.
Cited ../<x>/<file> paths are relative to the citing file.

## Check

1. Before the selected server profile's
   first Home Assistant or HA NOVA task, and before any relay command or typed
   verb, run `ha-nova check-update --quiet` in the same shell command
   as that first call: `ha-nova check-update --quiet; ha-nova call …` (repeat
   `--server <name>` per command, not on `apply`). Once a typed verb or relay command for that server answered, a
   bare `ha-nova relay health` preflight is done; a skill reading its output
   still runs it.
2. Run one check for every server profile in this session, with its
   `--server <name>`; empty or failed counts, and empty stays silent.
3. Later status arrives in typed verbs' `notices[]` and `[ha-nova]` relay
   lines (never a substitute for step 1); never repeat one already surfaced.
   `--server demo` is the shared demo house, not the user's.

## HA NOVA Update Callout

After the requested result, surface an available HA NOVA update exactly once
per session in its own localized callout: installed → latest version, up to three supplied highlight lines, the supplied release URL, and
an offer to update. Never update without
consent. After a successful `ha-nova update`, tell the
user to start a new AI-client session.

## Relay Update Callout

The same callout, once per server profile. NOVA Relay App: name both
supplied versions, say the Relay restarts, and offer the guided update; a yes
only invokes `ha-nova:updates`, which must show its App-update preview and obtain
confirmation before installing with a partial backup. A standalone Container/Core Relay:
point to the manual image pull and container recreation.
