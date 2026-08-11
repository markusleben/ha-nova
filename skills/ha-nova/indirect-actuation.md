# Indirect Actuation Gate

Canonical path: `skills/ha-nova/indirect-actuation.md`

Some runtime calls actuate entities the request never names. The confirmation
tier must follow the action that actually gets performed, not the service that
was called — otherwise a scene, script, automation, or spoken utterance becomes
a natural-confirmation bypass of the high-consequence tier (context skill →
Confirmation Tiers).

Read this before previewing any call in the trigger list below.

## Three rules, and then the cases

Everything below is one of these. When a case is not listed, decide it from
the rule, not from the absence of an entry — the enumeration is illustration,
the rules are the contract.

1. **The target you see is not always the target that acts.** A service alias,
   a device action, a shorthand key, a legacy group, an area or label — each
   names one thing and actuates another. Resolve to the entities that will
   actually be acted on, then classify those.
2. **A stored action belongs to whoever authored it.** Anything on the
   `template` platform, and anything whose behaviour lives in a config rather
   than in the integration, can run whatever its author wrote — regardless of
   how harmless its domain looks. Read the stored action; ordinary-device
   reasoning does not apply to it.
3. **Unread is not empty.** A blueprint body, a YAML item without an `id`, a
   zero-hit relation scan, a targetless reload, a templated target — each
   returns nothing and means "not known", never "nothing there". Escalate, and
   name what could not be read.

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
- a script's RUNNING state never lowers the tier. Every spelling that may
  start one — `script.<script_id>`, `script.turn_on`, `homeassistant.turn_on`,
  and either `toggle` — expands and classifies its members, whatever the state
  read showed. Home Assistant does reject an overlapping call on `mode: single`,
  but that observation expires: a script running at preview time can finish
  while the confirmation waits, and then the call that "runs nothing" runs
  everything, including a `lock.unlock` nobody listed. Only a pure stop
  (`turn_off`) expands nothing, because no timing turns a stop into a start.
- the gate's conclusion is still re-checked at apply time — member configs, the
  broad targets inside them (a stored `area_id`/`label_id` keeps its selector,
  so its membership is never frozen the way a direct call's expansion is, and a
  garage door added to that label between preview and apply arrives silently),
  AND the consumer scan itself, since a helper can gain a listener during the pause
  and re-reading only what you already found cannot discover one that did not
  exist yet. Any change re-previews at the tier the new facts deserve. A
  confirmation binds to what was shown, and the gate's verdict is part of what
  was shown.

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
does not expand STORED MEMBERS, because there are none: the call actuates
exactly what it names. It still carries the CONSUMER scan, because a Home
Assistant state trigger accepts any entity: a light that another automation
answers with `lock.unlock` grants access just as a helper toggle does, and the
helper domains are only where that is most likely, never where it is possible.
One `search/related` on the target settles it, and a zero-hit scan stays
ordinary under the coverage rules below — so this costs one relation read on
the common path and escalates nothing by itself.

There is also ONE exception that is an entry condition and not a later
refinement: any target on the `template` platform (`pl: "template"` in
the registry) enters the gate whatever its domain, because a Template light,
cover, fan, lock or valve carries its author's own action sequence rather than
a device command. Check `pl` before concluding "ordinary control" — a
`light.turn_on` on a Template light can be `lock.unlock` wearing a lamp.

## Expanding the members

Read the stored config:

```text
ha-nova relay core --method GET --path /api/config/{scene|script|automation}/config/<config_id>
```

- automations: `config_id` is `attributes.id` from the state read the flow
  already performs
- storage scenes and scripts: resolve `config_id` from the entity registry
  (`config/entity_registry/get` → `unique_id`), never from a state attribute —
  `ha-nova:scene` makes the registry authoritative and forbids the substitute,
  and reading the wrong config means classifying a scene that is not the one
  being activated
- `scene.apply`: no stored config exists — classify the entities map in the
  payload you are about to send

Then:

- Skip actions carrying `enabled: false`: Home Assistant does not run them, so
  a disabled `lock.unlock` step is not a member and must not put the run on
  the typed tier. This is the one place the union does NOT apply — everything
  else is unioned because preview time cannot know which branch runs, while a
  disabled node is known not to run at all.
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
- `python_script.*`, `shell_command.*` and `rest_command.*` are terminals only
  in the sense that the traversal stops there — their implementations live in
  files or in `configuration.yaml`, not in anything you read, and a user-
  authored one can call `lock.unlock` as easily as it can log a line. Treat
  them as unread, not as concrete: escalate and name the script or command.
  This is rule 2 and rule 3 at once — the action belongs to its author and
  you cannot see it.
- A blueprint-backed automation reads back as `use_blueprint` with inputs, not
  as actions: the traversal reaches no member and would otherwise conclude the
  run is harmless. It is an unresolved branch, not an empty one — escalate and
  name the blueprint, unless its inputs themselves name an access-capable
  entity, which settles it the other way. Do not stop early at a
  self-imposed depth: an unresolved chain is not a clean result (see below).
  A node already visited on this path is a cycle — stop that branch there.
- A legacy `group.*` target forwards the call to its members, so the gate sees
  `group` and the run reaches whatever is inside — including a script or an
  access-granting scene. Read `attributes.entity_id` on the group and classify
  the members, recursively: a group can contain a group.
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
- A targetless reload has NO target, and two families qualify — both enter the
  gate, exactly like a write to a single trigger source: the
  trigger-source helpers (`schedule.reload`, `input_*.reload`,
  `counter.reload`, `timer.reload`) and the STATE sources whose reload can
  move a sensor value an automation watches (`template.reload`, `rest.reload`,
  `command_line.reload`). Both mean the same thing here — so there
  is nothing for `search/related` to take — and that must not resolve to
  "nothing happens". Its effect set is every entity that reload touches — for the helper reloads
  that is the domain (`schedule.*`, `input_boolean.*`, ...), but `template`,
  `rest` and `command_line` are PLATFORMS whose entities live in `sensor` and
  `binary_sensor`, so enumerate by `pl` in the entity registry, not by domain.
  That enumeration is incomplete by construction: a YAML-declared sensor
  without a `unique_id` has no registry row at all. Detect them — entity ids
  present in `/api/states` and absent from the registry cannot be attributed
  to a platform — and when any exist, the effect set is not fully known, so
  the reload escalates instead of reporting a clean scan. Either way: a reload
  that moves a helper's state fires whatever listens to it. Enumerate the
  domain and scan those entities when the count allows; when it does not, say
  so and treat the run as unenumerable, exactly like an unreadable listener.
  "No target" is not evidence of no impact.
- Trigger sources expand the other way: `search/related` on the target, then
  read the actions of every automation it triggers. The classic pattern is a
  helper toggle that another automation answers by unlocking a door.
- A `timer` target needs the EVENT scan on top of that relation scan, because
  its consumers need not reference the entity at all. An automation triggered
  on a timer event with a literal `event_data.entity_id` produces no relation
  for `search/related` to find — the preflight says outright that it does not
  index event listeners. Scan readable automation configs for ANY `timer.*`
  event trigger naming this entity, not a list of event names: every lifecycle
  call emits one — `timer.start` fires `started` or `restarted` and `finished`
  at expiry, `timer.finish` fires `finished` at once, `timer.pause` fires
  `paused`, `timer.cancel` fires `cancelled` — so enumerating them leaves the
  next one uncovered. Treat unenumerable listeners exactly as the
  stored-`event:` path does.
- A zero-hit scan is NOT "no consumers". `search/related` does not index
  Node-RED flows, AppDaemon apps, HACS consumer managers, templated listeners,
  or dashboard references — `skills/ha-nova/consumer-discovery-preflight.md`
  lists them as **not checkable**, and its Coverage Report is what a
  trigger-source expansion produces here too. The strongest claim an empty
  scan supports is "no consumers in the checked families". A not-checkable
  family only breaks coverage when it is actually PRESENT: no Node-RED, no
  AppDaemon, no HACS consumer manager on this instance means there is nothing
  unread and the scan really is complete — that is the ordinary-tier path, and
  it is the common case. Check presence per RUN, not once per session: an install that appears mid-session makes the earlier answer wrong, and this is the one cached fact that decides a tier (their own App or
  integration entries), name what you checked, and escalate only when a
  present family could not be read. Presence detection is also not the whole
  question: a native template trigger or template entity whose dependency is
  computed at runtime consumes a helper without producing any relation, and no
  App or integration entry marks that. Templates are their own not-checkable
  family — if the instance has any template entities or template-triggered
  automations at all, say so in the coverage report rather than reporting a
  clean scan. Presence detection also sees LOCAL installs
  only: a Node-RED or AppDaemon running on another machine talks to Home
  Assistant over the API and leaves no App or integration entry. So report
  the finding as "no locally installed consumer manager" rather than "no
  consumer manager", and never call the coverage complete on that basis — the
  run stays ordinary, but the user is the one who knows whether something
  external is listening. The general unreadable-member fallback
  still does not apply here: for a trigger source the unread thing IS the
  consumer, so an installed-but-unreadable Node-RED escalates rather than
  being noted as a gap.
- Any entity on the `template` platform can carry the action itself instead of
  handing it to an automation, and that is what makes it an exception here.
  A Template button defines its press, a Template switch its
  `turn_on`/`turn_off`, a Template cover its `open_cover`/`close_cover`, a
  Template lock its `lock`/`unlock`, a Template light or fan its own
  control sequence — the carrier is the platform, not the domain, and the
  ordinary-device-control exemption does NOT apply to it. A `light.turn_on`
  is ordinary control on a real bulb and an arbitrary stored sequence on a
  `pl: "template"` light. A non-access Template cover can
  run any sequence its author wrote, so a zero-hit `search/related` on a
  `pl: "template"` entity proves nothing about what actuating it will run.
  Read the stored action; if you cannot, escalate. The registry field
  `pl` (platform) decides which kind you have: `pl: "template"` is a
  user-authored button whose press action lives in the button, so
  `search/related` returning nothing proves nothing. Read the action — the
  helper-created ones through their config entry, YAML-defined ones only from
  the file if `ha-nova:yaml-config` has read access — and if you cannot read
  it, escalate: an arbitrary user-written action is exactly the unenumerable
  case below, and a person who wrote `lock.unlock` into a button did not make
  it safer by writing it in YAML. Integration buttons split in two. Where the integration
  defines the behaviour — `unifi`, `shelly`, `reolink`, an `update` entity:
  restart, identify, update — the press is fixed and stays ordinary. Where the
  USER defines it in the device's own configuration — `esphome` (an `on_press`
  block in device YAML), `mqtt` (a command topic the user chose) — the action
  is authored, unreadable from here, and escalates. Rules 2 and 3 again; the
  registry platform tells you which kind you have.

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
arbitrary and unknown, and it escalates. The same holds for a YAML AUTOMATION
without an `id`: there is no config id to read its actions with, so
`automation.trigger` on it is an unresolved run, not an empty one. Most homes have far more
integration-owned scenes than hand-written ones, which is why the ordinary
path has to stay reachable rather than escalating every unreadable scene.

Two exceptions come first, because they are not really unknowns.

A nested RUN whose own target is templated — `script.turn_on`,
`scene.turn_on`, `homeassistant.turn_on|toggle` with a `{{ ... }}` entity_id —
is unresolved for the same reason a templated action name is: you cannot read
what it runs, so you know nothing about its members. Escalate and name the
unresolved target. Only a run whose target resolves statically can be expanded
and then judged.

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
`open_cover`, `toggle` (a closed garage opens), and `set_cover_position` at
ANY position above 0 — not "above the current one". A door moves: it can
finish closing while the confirmation waits, and then a position the preview
read as a partial close opens it. Comparing against a live reading is the same
expiring-observation mistake as keying a tier on a running script. Only `close_cover` is safe on an unknown
target, because closing grants nothing.

When the ACTION NAME itself is templated, or an event listener cannot be
enumerated, you know nothing about what it does — and "I could not read it"
is not evidence that it is harmless. Escalate.

Otherwise — meaning an unreadable INTEGRATION-owned member, whose platform
bounds what it can reach — these are limits Home Assistant imposes: stay at the
ordinary tier and name in the preview which members you could not classify.
That is the only class this paragraph covers; a USER-AUTHORED member you could
not read is rule 3 and escalates, which is what the escalation list above says. Do not escalate everything
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
