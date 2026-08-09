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

Stopping is not starting. `homeassistant.turn_off`, `script.turn_off`, and
`automation.turn_off` halt or disable — they perform none of the member
actions, so they stay at the ordinary tier. Demanding a typed code to STOP
something contradicts the performed-action rule and adds friction exactly
when a user is trying to make behavior end.

By service name, the gate also covers:

- `scene.turn_on`, `scene.apply`
- `automation.trigger`
- any script run: `script.<script_id>`, `script.turn_on`, `script.toggle`
- writes to a trigger source — the helper domains exist to drive automations:
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
- scripts: `config_id` is the object part of the entity_id
- `scene.apply`: no stored config exists — classify the entities map in the
  payload you are about to send

Then:

- Descend into nested scene, script, and automation calls, and take the union
  of every `choose`, `if`, `repeat`, and `parallel` branch: preview time
  cannot know which branch runs. Follow each branch until it ends in a
  concrete service action or a node you cannot read. Do not stop early at a
  self-imposed depth: an unresolved chain is not a clean result (see below).
  A node already visited on this path is a cycle — stop that branch there.
- Resolve `area_id`, `device_id`, `floor_id`, and `label_id` targets to
  entities for classification only. This never rewrites a payload — the
  stored config is not yours to change here.
- Trigger sources expand the other way: `search/related` on the target, then
  read the actions of every automation it triggers. The classic pattern is a
  helper toggle that another automation answers by unlocking a door.

## Classifying what you found

- A member that unlocks or opens a lock, disarms an alarm panel, opens a
  garage/gate/entry-door cover by `device_class`, or is physically
  irreversible puts the WHOLE run on the typed `confirm:<token>` tier.
- Locking, closing, and arming grant nothing and stay ordinary.
- For a scene the target STATE decides, not the entity's presence:
  `unlocked`, `open`, and `disarmed` grant access; `locked`, `closed`, and
  `armed` do not.
- Members set the tier and nothing else. Routing stays with the skill that
  owns the run, even when a member's own domain has an owning skill.

## When you cannot see the members

Integration-owned scenes (Hue and similar) have no Home Assistant config and
return 404. Templated targets resolve only at runtime. An utterance is never
enumerable at all.

Those are limits Home Assistant imposes: stay at the ordinary tier and name in
the preview which members you could not classify. Do not escalate everything
you cannot read — blanket escalation trains users to type confirmation codes
without reading them, which costs more safety than it buys, and on an instance
whose scenes all belong to an integration it would fire on every single scene.

A limit YOU imposed is the opposite case and fails closed — stopping early
would restore the exact bypass this gate exists to close, by rewarding anyone
who buries `lock.unlock` one level deeper. Escalate to the typed tier and name
the unresolved branch when:

- you stopped following a chain before it resolved, for any reason (length,
  cost, a cycle you cut)
- a `search/related` scan FAILED (as opposed to returning nothing) — that is
  inconclusive, never a clean result
- an utterance whose words, or whose exposed entity set, plausibly reach a
  lock, alarm panel, or access cover

## Single-confirmation cards

A Test Plan Card doubles as the runtime preview, so the option choice is the
bound confirmation (`skills/ha-nova/test-run.md`). That shortcut never applies
to a run this gate placed on the typed tier: the card choice does not replace
`confirm:<token>`.
