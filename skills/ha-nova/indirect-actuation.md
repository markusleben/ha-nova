# Indirect Actuation Gate

Canonical path: `skills/ha-nova/indirect-actuation.md`

Some runtime calls actuate entities the request never names. The confirmation
tier must follow the action that actually gets performed, not the service that
was called — otherwise a scene, script, automation, or spoken utterance becomes
a natural-confirmation bypass of the high-consequence tier (context skill →
Confirmation Tiers).

Read this before previewing any call in the trigger list below.

## What triggers the gate

- `scene.turn_on`, `scene.apply`
- `automation.trigger`
- any script run: `script.<script_id>`, `script.turn_on`, `script.toggle`
- writes to a trigger source: `input_button.press` and writes to an
  `input_*` or `switch` helper
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
  cannot know which branch runs. Stop at depth 3 or a repeat visit.
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
return 404. Templated targets resolve only at runtime. A depth or cycle cutoff
leaves the rest unseen. An utterance is never enumerable at all.

In those cases: stay at the ordinary tier and name in the preview which
members you could not classify. Do not escalate everything you cannot read —
blanket escalation trains users to type confirmation codes without reading
them, which costs more safety than it buys. The two exceptions, where
plausibility alone is enough to escalate:

- a `search/related` scan that FAILED (as opposed to returning nothing) is
  inconclusive, never a clean result
- an utterance whose words, or whose exposed entity set, plausibly reach a
  lock, alarm panel, or access cover

## Single-confirmation cards

A Test Plan Card doubles as the runtime preview, so the option choice is the
bound confirmation (`skills/ha-nova/test-run.md`). That shortcut never applies
to a run this gate placed on the typed tier: the card choice does not replace
`confirm:<token>`.
