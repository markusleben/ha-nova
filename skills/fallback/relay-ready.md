# Relay-Ready Features

Split out of `skills/fallback/SKILL.md` to keep that file under the repo's
~400-line ceiling. These are the surfaces the Relay can already reach but no
skill owns yet; the fallback Flow sends `Relay-Ready` rows here.


### Blueprints -- RELAY-READY

List and import automation/script blueprints from the community or custom URLs.

**Search:** `home assistant blueprint import automation api 2026`

**Experimental relay calls (no skill guardrails):**
```text
ha-nova relay ws --data-file <payload-file>

# payload examples (verify current schema via web search first):
# {"type":"blueprint/list","domain":"automation"}
# {"type":"blueprint/import","url":"https://community.home-assistant.io/t/..."}   (fetches + previews, does not save)
# {"type":"blueprint/save","domain":"automation","path":"<folder/name.yaml>","yaml":"<blueprint yaml>"}
# {"type":"blueprint/substitute","domain":"automation","path":"<folder/name.yaml>","input":{...}}   (read-only: expands the blueprint with the given inputs)
```

**Risks:** Imported blueprints execute when instantiated. Review blueprint source before import. Instantiating a blueprint into an automation (`use_blueprint`) is a normal automation create — hand off to `ha-nova:write`.

### Other Config-Entry Helpers -- RELAY-READY

Handle unsupported config-entry helper types that are not yet owned by `ha-nova:helper`.

Owned by `ha-nova:helper` now:

- `utility_meter`
- `derivative`
- `integration`
- `min_max`
- `threshold`
- `tod`
- `statistics`
- `group`
- `history_stats`
- `template`

Still handled here:

- `trend`
- `random`
- `filter`
- `generic_thermostat`
- `switch_as_x`
- `generic_hygrostat`

**Search:** `home assistant config entry flow helper trend random filter generic_thermostat api 2026`

**Supported types in this fallback section:** `trend`, `random`, `filter`, `generic_thermostat`, `switch_as_x`, `generic_hygrostat`

**Experimental relay calls (no skill guardrails):**
```text
# Start create/reconfigure flow
ha-nova relay core --method POST --path /api/config/config_entries/flow --body-file <payload-file>

# Submit create/reconfigure step
ha-nova relay core --method POST --path /api/config/config_entries/flow/{flow_id} --body-file <payload-file>

# Start options flow for update when supported
ha-nova relay core --method POST --path /api/config/config_entries/options/flow --body-file <payload-file>

# Submit options step
ha-nova relay core --method POST --path /api/config/config_entries/options/flow/{flow_id} --body-file <payload-file>

# Delete unsupported config-entry helper by entry_id
ha-nova relay core --method DELETE --path /api/config/config_entries/entry/{entry_id}
```

**Risks:** Multi-step flows are complex. Each step returns the next step's schema. Update support can be domain- and version-specific. Delete requires correct `entry_id` resolution first. Prefer HA UI for these.

### Bounded Event Capture -- RELAY-READY

Watching what a physical button fires, or what happens in the seconds after an
action. The mechanics are already contracted in
`skills/ha-nova/relay-api.md` → Bounded Event Collection — do not restate them
here, and do not invent a bare subscription: the relay rejects one outside the
envelope with `UNSUPPORTED_WS_TYPE`.

```json
{
  "message": { "type": "subscribe_events", "event_type": "zha_event" },
  "collect_events": { "until_type": "finish", "max_events": 20, "timeout_ms": 10000, "on_limit": "return" }
}
```

`on_limit: "return"` is the mode for this: a button stream never finishes, so
the window has to close on the limit rather than error. `timeout_ms` is capped
at **10000** — the relay rejects anything larger with `VALIDATION_ERROR`
before it subscribes.

Read the result from `.data.events`. In window mode `.data.truncated` is true
whenever the window closed on a limit instead of a finish event, which is the
NORMAL ending here — it is not evidence that events were missed. Report what
arrived and the window length, and let the count speak: `max_events` reached
means there may well be more, a timeout with few or no events means that is
what happened in those seconds. An empty window is a real answer, not a
failure.

**Risks:** The window blocks for its full `timeout_ms` when nothing arrives —
say the duration before starting it. `ha-nova:mqtt` owns MQTT topics; this is
for Home Assistant's own event bus.

### Integration Entry Lifecycle -- RELAY-READY

Reload and remove for an existing config entry — those two only.
`ha-nova:integration-setup` owns ADDING an integration and continuing a
pending `reauth`. Enable/disable, options and reconfigure are `External` in
the Capability Map: their flows are UI-driven and no mechanics for them are
documented here, so point at Settings > Devices & services instead of
improvising a payload.

**Search:** `home assistant config entry reload delete api 2026`

**Experimental relay calls (no skill guardrails):**
```text
ha-nova relay core --method POST --path /api/config/config_entries/entry/<entry_id>/reload
```

**Risks:** Reload re-runs setup and briefly drops the entry's entities. Remove
(`DELETE /api/config/config_entries/entry/<entry_id>`) deletes every device and
entity that entry owns and is not undoable — preview the counts from
`search/related` and take the typed confirmation code.

### Assist Custom Sentences -- RELAY-READY

Teach Assist a phrase Home Assistant does not understand out of the box. This
needs the opt-in file access (`ha-nova:yaml-config` → Bootstrap explains how to
turn it on); its own scope covers sensors, packages and themes, so the file
mechanics live here.

**Search:** `home assistant custom_sentences intent_script yaml 2026`

**Experimental relay calls (no skill guardrails):**
```text
ha-nova relay files --data-file <payload-file>
```
Write `/config/custom_sentences/<lang>/<name>.yaml` — NOT `/config/ha_nova/`;
Home Assistant only reads sentences from that fixed path. An `intent_script:`
block in `configuration.yaml` supplies the action when the intent is new.

**Validate before you reload, always** — when the change touches
`configuration.yaml`, `POST /api/config/core/check_config` FIRST. An invalid
`configuration.yaml` does not just fail the reload: it stays on disk and stops
Home Assistant from starting on the next boot, which is unrecoverable from
here. On `{"result":"invalid"}` restore that file's `.bak` immediately, before
reporting, and do not reload. The sentence file and `configuration.yaml` are
two independent restores — roll back the one that is broken.

**Verify, and be ready to undo:** `write_file` with `backup: true` (the
default) so a `.bak` exists — a brand-new file has none, so remember that you
created it. Reload the right thing: `conversation.reload` reloads the SENTENCE
matcher and is enough when only the sentence file changed. A new
`intent_script:` block is NOT reloadable — Home Assistant registers no
`intent_script.reload`, and neither `homeassistant.reload_core_config` nor
`reload_all` loads those handlers. It takes a restart. So when the change adds
an intent handler, say that plainly, and do not run the phrase test before the
restart: it would fail for a valid file and trigger a pointless rollback. Then run the
exact phrase
through `ha-nova:assist` (`POST /api/conversation/process`): a sentence file
that parses is not a sentence Assist matched. If the phrase does not match, or
a previously working phrase stops matching, restore immediately — write the
`.bak` back (or `delete_file` the new file), reload again, and re-test one
known-good phrase before reporting. Leaving the reload in place is what turns
one bad file into an outage of every custom sentence.

**Risks:** `write_file` replaces the whole file, so read it first. A malformed
sentence file makes the conversation agent drop ALL custom sentences, not just
the new one — the assist test is what catches that.

### Matter And Thread Status -- RELAY-READY

Read-only network state for Matter/Thread setups: `otbr/info`,
`thread/list_datasets`, `matter/node_diagnostics`. Commissioning stays
external — it needs BLE from the companion app and is not an API surface.

**Search:** `home assistant thread otbr websocket api matter diagnostics 2026`

**Experimental relay calls (no skill guardrails):**
```text
ha-nova relay ws --data-file <payload-file>
```

**Risks:** none for the reads themselves; do not surface dataset credentials
(a Thread operational dataset is a network key) in output.

### Device Config-Entry Detach -- RELAY-READY

Remove a config entry from a device (entity-registry removal is owned by `ha-nova:maintenance`).

**Search:** `home assistant device registry remove config entry websocket api 2026`

**Experimental relay calls (no skill guardrails):**
```text
ha-nova relay ws --data-file <payload-file>
```

**Risks:** Device detach depends on integration support (`supports_remove_device`) and can sever the current device/config-entry relationship. Preview impact first.

### Custom-Integration Configuration APIs -- RELAY-READY

Integrations that ship their OWN configuration API outside the config-entry flow (Alarmo, Scheduler, Adaptive Lighting, Frigate, ...). Runtime control of their entities stays with the owning skills (for example alarm arm/disarm via `service-call`); this section covers the configuration layer only.

**Search:** `<integration name> home assistant configuration api endpoints 2026` — prefer the integration's own repository docs for payload schemas.

**Experimental relay calls (no skill guardrails):**
```text
ha-nova relay core --method GET --path /api/<integration>/<resource> --out <result-file>
ha-nova relay core --method POST --path /api/<integration>/<resource> --body-file <payload-file>
```

**Observed API behavior (Alarmo; treat as the default assumption for this class):**
- Write commands often exist only as HTTP POST paths, not WS commands (`unknown_command`); such paths answer GET with `405`, which reveals their existence.
- Whether a POST creates or updates depends solely on whether an identifier is present in the body (`area_id`, `entity_id`, `automation_id`, ...) — `POST` with `{}` silently CREATES an empty object (`skills/ha-nova/relay-api.md` → Write-Probing Asymmetry).
- Nested blocks must be sent complete; partial objects overwrite the rest.

**Risks:** These APIs are private and version-dependent. Never probe schemas with empty or partial POST bodies — resolve the schema via web search first. After ANY write, read the affected list/config back and verify no unintended object appeared; an accidental create can trigger integration side effects (Alarmo auto-enables its master panel at two areas).
