# Session Bootstrap

Apply this contract before the first Home Assistant task in a session.

## Check

1. Before the selected server profile's
   first Home Assistant or HA NOVA task, and before any relay command or typed
   verb, run `ha-nova check-update --quiet` in the same shell command
   as that first call: `ha-nova check-update --quiet; ha-nova call …` (a
   `HA_NOVA_SERVER=<name>` prefix covers only the command it precedes: repeat
   it). Once a typed verb or relay command for that server answered, a
   bare `ha-nova relay health` preflight is done; a skill reading its output
   still runs it.
2. Run one check for every server profile used in this agent session, with its
   `HA_NOVA_SERVER=<name>`. A check counts even when empty or failed;
   continue either way; empty output stays silent.
3. Later status arrives in typed verbs' `notices[]` (`update`,
   `relay_outdated`) and `[ha-nova]` relay lines — never a substitute for
   step 1. Never repeat a notice already surfaced in this session.

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
