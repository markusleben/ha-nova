# Relay-Ready Features

Split out of `../fallback/SKILL.md` to keep that file under the repo's
~400-line ceiling. These are the surfaces the Relay can already reach but no
skill owns yet; the fallback Flow sends `Relay-Ready` rows here.


### Zigbee / Z-Wave Network Status -- RELAY-READY

Read-only network observability for ZHA and Z-Wave JS setups: which devices
the radio integration knows, and whether the Z-Wave network is up. Pairing and
every other radio WRITE stay External per the Capability Map. A Zigbee2MQTT
setup answers this over MQTT instead: `ha-nova:mqtt` reads the retained
`zigbee2mqtt/bridge/...` topics in a bounded window.

**Search:** `home assistant zha zwave_js websocket api network status 2026`

**Experimental relay calls (no skill guardrails):**
```text
ha-nova relay ws --data-file <payload-file>

# {"type":"zha/devices"}                                        (ZHA device list)
# {"type":"zwave_js/network_status","entry_id":"<entry_id>"}    (Z-Wave JS network state)
```

These command names are pinned by no repo reference — verify them live before
first use in the research step (confirm the current name and required
arguments before sending anything; `zwave_js/*` commands typically take the
config `entry_id`).

**Risks:** none for the reads themselves; a wrong or renamed command fails
with `unknown_command` — never guess a write variant from it.

### Bounded Event Capture -- RELAY-READY

Watching what a physical button fires, or what happens in the seconds after an
action. The mechanics are already contracted in
`../ha-nova/relay-api.md` → Bounded Event Collection — do not restate them
here, and do not invent a bare subscription: the relay rejects one outside the
envelope with `UNSUPPORTED_WS_TYPE`.

This is the generic answer to "what does my button / remote / tag fire?":
resolve the SOURCE first — the device for buttons/remotes, the `tag/list` registry record (`tag_id`, `ha-nova:admin`) for NFC/QR tags, which are not device-registry devices — then arm the envelope with a named `until_type` or
explicit limits, then report the captured event TYPES together with their data
keys — the payload fields a trigger would match on (`zha_event`,
`deconz_event`, and `tag_scanned` are typical). Never infer an event you did
not capture: an automation built on a guessed payload matches nothing.

Resolve the event type from the button's own integration first — it is not one
value. ZHA fires `zha_event`, Z-Wave JS `zwave_js_value_notification`, deCONZ
`deconz_event`, and a modern `event.*` entity fires no bus event at
all. Read the entity's `platform` from the registry and pick accordingly. A remote
with no entity at all — only a device-registry row — has no `platform` to
read: resolve it through the device's singular `config_entry_id` (older rows:
`config_entries`, which hold opaque entry ids rather than domains — see
`../ha-nova/membership-resolution.md` → Device rows): read `{"type":"config_entries/get"}` (or the REST
`/api/config/config_entries/entry`) and take the entry's `domain`. Where that
is still ambiguous — several entries, or none — ASK which integration the
remote belongs to rather than guessing an event type. A guess produces an empty window that
reads as "nothing was pressed"; listening for the wrong type produces the same
false negative.

For an `event.*` entity there is no bus event to subscribe to, so watch the
entity instead — same envelope, different message:

```json
{
  "message": { "type": "subscribe_trigger",
               "trigger": { "platform": "state", "entity_id": "event.<id>" } },
  "collect_events": { "max_events": 20, "timeout_ms": 10000, "on_limit": "return" }
}
```

Each press bumps the entity's `state` to a new timestamp and puts the button
in `attributes.event_type`; read that, not the state value.

```json
{
  "message": { "type": "subscribe_events", "event_type": "<the integration's event type>" },
  "collect_events": { "until_type": "finish", "max_events": 20, "timeout_ms": 10000, "on_limit": "return" }
}
```

`on_limit: "return"` is the mode for this: a button stream never finishes, so
the window has to close on the limit rather than error. `timeout_ms` is capped
at **10000** — the relay rejects anything larger with `VALIDATION_ERROR`
before it subscribes.

Filter the result to the button you resolved: the subscription takes an event
TYPE, so every other ZHA or Z-Wave device on the same integration lands in the
same window. Match the event's own device field (`device_id`, or the
integration's `device_ieee`/`node_id`) before reporting anything — an
unfiltered window turns the neighbour's motion sensor into "your button fires
this".

Read the result from `.data.events`. In window mode `.data.truncated` is true
whenever the window closed on a limit instead of a finish event, which is the
NORMAL ending here — it is not evidence that events were missed. Report what
arrived and the window length, and let the count speak: `max_events` reached
means there may well be more, a timeout with few or no events means that is
what happened in those seconds. An empty window is a real answer, not a failure — with ONE exception:
when the user says they pressed the button and nothing arrived, do not report
that as "nothing was published". They may have pressed before the subscription
was live, and the blocking command exposes no readiness signal. Report the
empty window as timing-inconclusive, never as proof of device silence, even
after a retry.

**Risks:** The window blocks for its full `timeout_ms` when nothing arrives —
say the duration before starting it. `ha-nova:mqtt` owns MQTT topics; this is
for Home Assistant's own event bus.

### Assist Custom Sentences -- RELAY-READY

Owned by `ha-nova:assist` → Custom Sentences — hand the workflow off there.
This section keeps only the file mechanics that workflow references. The write
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

Rolling back an `intent_script` is restore + `intent_script.reload`: the
reload removes the stale handler along with re-reading the file. Only when the
block never loaded (first block, restart still pending) is the restore alone
complete.

**Verify, and be ready to undo:** `write_file` with `backup: true` (the
default) so a `.bak` exists — a brand-new file has none, so remember that you
created it. Reload the right thing: `conversation.reload` reloads the SENTENCE
matcher and is enough when only the sentence file changed. A handler added to
an existing `intent_script:` block loads via `intent_script.reload`. Only the
FIRST-ever top-level `intent_script:` block takes a restart: the integration
is not set up yet, so its reload service does not exist, and neither
`homeassistant.reload_core_config` nor `reload_all` sets it up. Say which
applies, and do not run the phrase test before that reload or restart: it
would fail for a valid file and trigger a pointless rollback. Then run the
exact phrase
through `ha-nova:assist` (`ha-nova call conversation.process --skill ha-nova:assist --response`): a sentence file
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

All three are bare WS messages with no target: `{"type":"otbr/info"}`,
`{"type":"thread/list_datasets"}`. Send `thread/list_datasets` with `--out
<file>` and never without: its response carries the operational dataset, which
IS the network credential, and an unredirected call prints it to stdout and
into the transcript. Read the file, report dataset names and channel only. `matter/node_diagnostics` is the exception
and takes the device, not the entity: `{"type":"matter/node_diagnostics",
"device_id":"<device_id>"}` — resolve it from
`{"type":"config/device_registry/list"}`. Confirm the current argument names
in the research step before sending: these commands are young and this section
pins no schema.

**Risks:** none for the reads themselves; do not surface dataset credentials
(a Thread operational dataset is a network key) in output.

### Custom-Integration Configuration APIs -- RELAY-READY

Integrations that ship their OWN configuration API outside the config-entry flow (Alarmo, Scheduler, Frigate, ...). Runtime control of their entities stays with the owning skills (for example alarm arm/disarm via `service-call`); this section covers the configuration layer only. An integration configured through a standard config-entry OptionsFlow (for example Adaptive Lighting) does not belong here — its changes follow the config-entry rows of the Capability Map (precedence rule).

**Search:** `<integration name> home assistant configuration api endpoints 2026` — prefer the integration's own repository docs for payload schemas.

**Experimental relay calls (read only):**
```text
ha-nova relay core --method GET --path /api/<integration>/<resource> --out <result-file>
```

Writes to these APIs are refused (`POST /api/<integration>/…` is not on the raw allowlist): such an API can arm, disarm, or change codes with no tier. Read the config here; make the change in the integration's own panel in the Home Assistant UI.

**Risks:** These APIs are private and version-dependent; a read can still be large — use `--out`.
