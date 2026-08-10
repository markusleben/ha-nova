# Indirect Actuation Gate

Canonical path: `skills/ha-nova/indirect-actuation.md`

Some runtime calls actuate entities the request never names. The confirmation
tier must follow the action that actually gets performed, not the service that
was called — otherwise a scene, script, automation, or spoken utterance becomes
a natural-confirmation bypass of the high-consequence tier (context skill →
Confirmation Tiers).

Read this before previewing any call in the trigger list below.

## What triggers the gate

**Classify by the TARGET's domain first, then by the service name.** Home
Assistant's generic services accept any entity, so `homeassistant.turn_on`
with `entity_id: script.open_door` is a script run wearing another name — and
a service-name list alone would wave it through. Any call that STARTS a
`scene`, `script`, or `automation` enters this gate, whatever service was
used to get there (`homeassistant.turn_on` and `homeassistant.toggle`
included; a toggle can start a stopped script).

Only an actual run expands members. Three cases do NOT run anything and stay
at the ordinary tier, because demanding a typed code to stop or configure
something contradicts the performed-action rule:

- stopping: `homeassistant.turn_off`, `script.turn_off`, `automation.turn_off`
- enabling or disabling an AUTOMATION: `automation.turn_on|turn_off|toggle`
  and their `homeassistant.*` aliases only flip whether it may fire later;
  `automation.trigger` is the one that runs it
- `script.toggle` or `homeassistant.toggle` on a script that is currently
  running (state `on`): that stops it. Read the state first — the same call on
  an idle script starts it and does expand members.

By service name, the gate also covers:

- `scene.turn_on`, `scene.apply`
- `automation.trigger`
- any script run: `script.<script_id>`, `script.turn_on`, `script.toggle`
- writes to a trigger source — `button.press` runs a Template button's stored action, and the helper domains exist to drive automations:
  `input_boolean`, `input_number`, `input_select`, `input_text`,
  `input_datetime`, `input_button`, `counter`, `timer`, `schedule`, and
  `switch`. A counter crossing a threshold or a timer finishing is a trigger
  like any other.
- a test utterance through `conversation/process` (`ha-nova:assist`)

Ordinary device control — lights, media, comfort climate, non-access covers —
does not carry this gate.

## Expanding the members

Read the stored config:

```text
ha-nova relay core --method GET --path /api/config/{scene|script|automation}/config/<config_id>
```

- automations and storage scenes: `config_id` is `attributes.id` from the
  state read the flow already performs
- scripts: resolve `config_id` from the entity registry (`config/entity_registry/get` → `unique_id`). It usually equals the object part of the entity_id, but the registry is the authoritative source — the same rule `ha-nova:scene` already enforces
- `scene.apply`: no stored config exists — classify the entities map in the
  payload you are about to send

Then:

- Descend into nested scene, script, and automation calls, and take the union
  of every `choose`, `if`, `repeat`, and `parallel` branch: preview time
  cannot know which branch runs. Follow each branch until it ends in a
  concrete action or a node you cannot read — and Home Assistant writes an
  action two ways. The service form is the obvious one, and it
  has THREE spellings: `action: lock.unlock` (current), plus the legacy
  `service: lock.unlock` and `service_template:` that older YAML still
  carries and Home Assistant still executes. The DEVICE form (`domain: lock`,
  `type: unlock`, plus a `device_id`) says the same thing in different keys
  and is what the automation editor produces for a device-picked step. There
  is also a SHORTHAND form for a few domains — `scene: scene.open_house` is a
  scene activation written without a service, and it expands exactly like
  `scene.turn_on` on that target; classify a shorthand by the domain it names.
  Classify a device action by its `domain` + `type` exactly as you would the
  equivalent service, or a door-unlock built in the UI walks past this whole
  gate; treat a `service_template` like any templated action name — you
  cannot read what it resolves to, so escalate.
- A blueprint-backed automation reads back as `use_blueprint` with inputs, not
  as actions: the traversal reaches no member and would otherwise conclude the
  run is harmless. It is an unresolved branch, not an empty one — escalate and
  name the blueprint, unless its inputs themselves name an access-capable
  entity, which settles it the other way. Do not stop early at a
  self-imposed depth: an unresolved chain is not a clean result (see below).
  A node already visited on this path is a cycle — stop that branch there.
- Resolve `area_id`, `device_id`, `floor_id`, and `label_id` targets to
  entities for classification only. This never rewrites a payload — the
  stored config is not yours to change here.
- A stored `event:` action reaches further than the config you are reading:
  its listeners run too. Apply the same consumer scan the direct path uses
  (`skills/ha-nova/consumer-discovery-preflight.md` — scan readable automation
  configs for that exact `event_type`) and classify what they do. Listeners
  that are not enumerable (templated event types, non-automation consumers
  such as Node-RED or AppDaemon) cannot be judged access-capable or not, so
  their presence escalates the run on its own — see the exceptions below.
- A targetless reload has NO target, and two families qualify: the
  trigger-source helpers (`schedule.reload`, `input_*.reload`,
  `counter.reload`, `timer.reload`) and the STATE sources whose reload can
  move a sensor value an automation watches (`template.reload`, `rest.reload`,
  `command_line.reload`). Both mean the same thing here — so there
  is nothing for `search/related` to take — and that must not resolve to
  "nothing happens". Its effect set is every entity of that domain: a reload
  that moves a helper's state fires whatever listens to it. Enumerate the
  domain and scan those entities when the count allows; when it does not, say
  so and treat the run as unenumerable, exactly like an unreadable listener.
  "No target" is not evidence of no impact.
- Trigger sources expand the other way: `search/related` on the target, then
  read the actions of every automation it triggers. The classic pattern is a
  helper toggle that another automation answers by unlocking a door.
- A zero-hit scan is NOT "no consumers". `search/related` does not index
  Node-RED flows, AppDaemon apps, HACS consumer managers, templated listeners,
  or dashboard references — `skills/ha-nova/consumer-discovery-preflight.md`
  lists them as **not checkable**, and its Coverage Report is what a
  trigger-source expansion produces here too. The strongest claim an empty
  scan supports is "no consumers in the checked families". A not-checkable
  family only breaks coverage when it is actually PRESENT: no Node-RED, no
  AppDaemon, no HACS consumer manager on this instance means there is nothing
  unread and the scan really is complete — that is the ordinary-tier path, and
  it is the common case. Check presence once per session (their own App or
  integration entries), name what you checked, and escalate only when a
  present family could not be read. Presence detection sees LOCAL installs
  only: a Node-RED or AppDaemon running on another machine talks to Home
  Assistant over the API and leaves no App or integration entry. So report
  the finding as "no locally installed consumer manager" rather than "no
  consumer manager", and never call the coverage complete on that basis — the
  run stays ordinary, but the user is the one who knows whether something
  external is listening. The general unreadable-member fallback
  still does not apply here: for a trigger source the unread thing IS the
  consumer, so an installed-but-unreadable Node-RED escalates rather than
  being noted as a gap.
- A `button` or `switch` is the exception among trigger sources, because it
  can carry the action itself instead of handing it to an automation. A
  Template switch defines its own `turn_on`/`turn_off` sequences exactly as a
  Template button defines its press, so everything below applies to both — a
  zero-hit `search/related` on a `pl: "template"` switch proves nothing about
  what `switch.turn_on` will run. The registry field
  `pl` (platform) decides which kind you have: `pl: "template"` is a
  user-authored button whose press action lives in the button, so
  `search/related` returning nothing proves nothing. Read the action — the
  helper-created ones through their config entry, YAML-defined ones only from
  the file if `ha-nova:yaml-config` has read access — and if you cannot read
  it, escalate: an arbitrary user-written action is exactly the unenumerable
  case below, and a person who wrote `lock.unlock` into a button did not make
  it safer by writing it in YAML. Every other platform (`unifi`, `esphome`,
  `mqtt`, `shelly`, `reolink`, ...) is an integration button whose behaviour
  is fixed by that integration — restart, identify, update — and stays
  ordinary.

## Classifying what you found

- A member that unlocks or opens a lock, disarms an alarm panel, opens a
  garage/gate/entry-door cover by `device_class`, or is physically
  irreversible puts the WHOLE run on the typed `confirm:<token>` tier.
- Locking, closing, and arming grant nothing and stay ordinary.
- A member whose OWNING skill already requires the typed tier carries that
  tier into the run, and a member whose tier cannot be DETERMINED escalates
  rather than defaulting down: an `mqtt.publish` with a templated `topic` or
  `retain` is exactly that case — you cannot tell whether it hits a command
  topic, so the unreadable-member fallback does not apply — `mqtt.publish` to a command or `set` topic, or any
  retained publish, is the standing case (`skills/mqtt/SKILL.md`). Physical
  access is not the only reason a member is gated, and expanding a run must
  not downgrade what calling the member directly would have required.
- For a scene the target STATE decides, not the entity's presence:
  `unlocked`, `open`, and `disarmed` grant access; `locked`, `closed`, and
  `armed` do not.
- Members set the tier and nothing else. Routing stays with the skill that
  owns the run, even when a member's own domain has an owning skill.

## When you cannot see the members

Integration-owned scenes (Hue and similar) have no Home Assistant config and
return 404. Templated targets resolve only at runtime. An utterance is never
enumerable at all.

Not every unreadable scene is the same, and the registry `pl` field separates
them exactly as it does for buttons. A scene on an INTEGRATION platform is
bounded by what that integration controls — but "bounded" only helps if the
bound excludes access. Check it instead of assuming: if that platform provides
any `lock`, `alarm_control_panel`, or access-class `cover` entity in the
registry, its scenes can reach one and the run escalates. A lighting hub
provides none, so its scenes stay ordinary with the gap named; a
general-purpose platform like SmartThings usually does. A
scene on the `homeassistant` platform is user-authored; if it also has no
readable config (a YAML scene declared without an `id:`), then its members are
arbitrary and unknown, and it escalates. This instance has 42 scenes, all
integration-owned, which is why the ordinary path has to stay reachable.

Two exceptions come first, because they are not really unknowns.

When the stored ACTION is access-capable (`lock.unlock`, `lock.open`,
`alarm_control_panel.alarm_disarm`) and only its TARGET is templated, the
service already proves what the run grants. Hiding the entity id behind a
template does not make it unknown — escalate to the typed tier and say which
entity could not be resolved.

Cover actions are the ones where the service alone does not settle it: a blind
and a garage door take the same call, and only the resolved entity's
`device_class` separates them. So an unresolved cover target fails CLOSED —
escalate — because the question "is this a garage door?" cannot be answered
and the wrong answer opens one. That covers every cover action that CAN open:
`open_cover`, `toggle` (a closed garage opens), and `set_cover_position` with
any position above the current one. Only `close_cover` is safe on an unknown
target, because closing grants nothing.

When the ACTION NAME itself is templated, or an event listener cannot be
enumerated, you know nothing about what it does — and "I could not read it"
is not evidence that it is harmless. Escalate.

Otherwise these are limits Home Assistant imposes: stay at the ordinary tier
and name in the preview which members you could not classify. Do not escalate everything
you cannot read — blanket escalation trains users to type confirmation codes
without reading them, which costs more safety than it buys, and on an instance
whose scenes all belong to an integration it would fire on every single scene.

A limit YOU imposed is the opposite case and fails closed — stopping early
would restore the exact bypass this gate exists to close, by rewarding anyone
who buries `lock.unlock` one level deeper. Escalate to the typed tier and name
the unresolved branch when:

- you stopped following a chain before it resolved, for any reason of length
  or cost. A cycle is different: revisiting a node you have ALREADY read and
  classified adds no action the traversal has not already seen, so a cycle
  between fully-read members is resolved, not cut — two scripts calling each
  other in a retry loop do not become access-capable by looping. A cycle
  through a member you could NOT read is still unresolved
- a `search/related` scan FAILED (as opposed to returning nothing) — that is
  inconclusive, never a clean result
- an utterance whose words, or whose exposed entity set, plausibly reach a
  lock, alarm panel, or access cover

## Single-confirmation cards

A Test Plan Card doubles as the runtime preview, so the option choice is the
bound confirmation (`skills/ha-nova/test-run.md`). That shortcut never applies
to a run this gate placed on the typed tier: the card choice does not replace
`confirm:<token>`.
