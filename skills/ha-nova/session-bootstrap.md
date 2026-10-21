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
   `--server <name>`; empty or failed counts; empty stays silent.
3. Later status arrives in `notices[]` and `[ha-nova]` lines (never a
   substitute for step 1); never repeat one.
   `--server demo` is the shared demo house, not the user's.

## HA NOVA Update Callout

After an HA task's requested result, say an update exactly once per session:
“A new HA NOVA version is ready (<version>)[: <New: text>].
Say ‘update HA NOVA’ and I'll do it.”
App update due: add “…and the NOVA App in Home Assistant too” (no Relay callout).
Never update without consent. Yes: run `ha-nova update`, relay its result,
then the Relay callout's yes path. Other step or no shell: tell the user that step.

## Relay Update Callout

Once per server profile. NOVA Relay App: offer the guided update; a yes
only invokes `ha-nova:updates`, which must show its App-update preview
and obtain confirmation before installing with a partial backup.
A standalone Container/Core Relay: point to the manual image pull and container recreation.
