# HA NOVA Test Run (Post-Write Test Offer)

Canonical path: `skills/ha-nova/test-run.md`

How to build, present, and follow up the optional test offer after an
automation or script write (write flow → Phase 5: Test Offer). Skill source
stays English; localize labels at runtime per
`skills/ha-nova/output-rules.md`.

Execution rules and confirmation validity stay owned by
`ha-nova:service-call` → Automation And Script Runtime Calls and
`skills/ha-nova/SKILL.md` → Safety Baseline → Active Preview Confirmation.
This file defines only how the test plan is chosen, presented, and verified.

## Purpose & Invariants

- The test is an offer. Never execute actions, triggers, or events before
  the user picks an option. Side-effect-free reads (entity states, a
  `POST /api/template` render of the conditions) are allowed while building
  the card — use them to annotate the options.
- Condition awareness: if a rendered condition is currently false, say so on
  the affected options ("the run would stop at <condition> right now") and
  recommend accordingly — never let a real-path test die silently at a
  condition the card did not mention.
- Home Assistant has no dry run: every executed action sequence actuates the
  real devices it targets. Safety comes from choosing what to run, naming
  the devices that will act and the state they end in, and verifying
  afterwards.
- Transparency duty: every run option names the physical devices it will
  touch and the expected end state before the user chooses.
- Single confirmation: the Test Plan card doubles as the runtime-call
  preview (exact service, target, payload, `skip_condition` value, device
  delta). The user's option choice is the natural confirmation bound to that
  exact preview — do not ask a second time. Any change to the plan expires
  the choice and requires a fresh card.

## Feasibility & Recommendation

Classify the just-written config on two axes, then recommend ONE option.

Action risk (drives the recommendation):

| Action block touches | Recommended test |
|---|---|
| notifications, logbook, `input_*` helpers, timers, variables only | real run — low consequence |
| ordinary devices: lights, switches, covers, media, fans, comfort climate | real run — name every device plus the restore plan |
| high consequence: locks, garage doors, alarm panels, valves, water heaters, heating setpoints, `homeassistant.restart`, anything irreversible | logic check first; a real run stays available but carries an explicit warning line and is never presented as consequence-free |

Co-listeners escalate: the risk class covers everything the run can set in
motion, not just the tested automation's own actions. If a named co-listener
performs physical or high-consequence actions (the classic pattern: a helper
toggle that another automation answers by unlocking a door), the real-path
option inherits that risk level — even when the tested automation's actions
are purely logical.

Unavailable targets: read the action-target states while building the card;
if one is `unavailable`, say that a run cannot prove physical behavior right
now and recommend the logic check or waiting until the device is back.

Trigger type (drives which options exist):

- `state` / `numeric_state` / `template` / `event` / `mqtt` — the full real
  path is testable (recipes below).
- `time` / `time_pattern` / `sun` / `calendar` — the real path cannot be
  faked honestly. Offer "run actions now" plus a trace check after the next
  scheduled occurrence (name the expected time). Never change the system
  clock or temporarily edit the trigger to force a test.
- Scripts have no trigger: parameterized runs replace the real-path option.
- If the automation is disabled, enabling it is part of the plan, must be
  named on the card, and the post-run restore returns it to disabled.
- Multi-target changes (write-safety → Multi-Target Changes): build ONE
  consolidated offer for the whole logical change — recommend the riskiest
  or most representative target first, never one card per target.

## Test Options

Three option types. Each card line states what runs, what it proves, and
what it touches.

### Logic check (nothing switches)

Render the automation's conditions and templates against live state via
`POST /api/template` (relay core). Zero side effects. Proves the logic under
the current state only — not that the trigger fires, not that actions work.

### Run actions now

- Automations: `automation.trigger`. Always set `skip_condition` explicitly
  in the payload — HA defaults it to `true` (conditions bypassed). Label
  `skip_condition: true` as higher risk on the card (service-call rule).
- Scripts: `script.<script_id>` (blocking; pass representative `variables`
  for parameterized scripts — one run per branch worth proving) or
  `script.turn_on` (fire-and-forget).
- Proves the action sequence executes. Does not exercise the trigger and
  cannot test branches keyed on `trigger.id` or trigger variables — say so
  when the config has them (real-path test or wait for a real run).
- Actions containing `delay` or `wait_*` can keep a blocking call open:
  prefer `script.turn_on` for long-running scripts and verify via trace. A
  slow service response is not a failure (relay-api → Timeout and Retry
  Guidance).

### Full real-path test

Fire the actual trigger source so trigger → condition → action runs exactly
as in production. Highest fidelity, widest blast radius: the state change or
event reaches every listener, not just the new automation. Before offering,
find co-listeners and name them on the card: `search/related` on a
manipulated entity; for fired events `search/related` does not index
listeners — scan automation configs for the same `event_type`, or state on
the card that other event listeners cannot be ruled out.

## Real-Path Recipes

- `state` / `numeric_state` on a controllable source (helpers, switches,
  lights): set or toggle the source entity via the service-call flow; plan
  the restore of the source in the same card.
- Triggers with a `for:` duration or a numeric threshold fire only after
  the state holds, or crosses from the non-matching side: start from a
  non-matching state, cross the threshold, and wait out the full `for:`
  window before reading the trace or restoring — an early restore resets
  the pending trigger and produces a false "did not fire". When that wait
  is impractical, fall back to actions-only or the logic check and say so.
- `state` on a physical sensor (motion, door, presence): there is no honest
  service to fake it — ask the user to trigger the device physically, then
  read the trace; otherwise fall back to "run actions now".
- `template`: run the logic check first; then manipulate the underlying
  entities as above.
- `event`: `POST /api/events/<event_type>` with the payload the trigger
  expects. Fires for every listener in the instance — name co-listeners.
- `mqtt`: publish the expected topic and payload via `ha-nova:mqtt`.
- Branches keyed on `trigger.id`: one real-path run per branch, each via its
  own trigger source.

## Post-Run Verification (automatic after any consented run)

Applies to actions-only and real-path runs. A logic check creates no run
and no trace — report the rendered condition/template results instead of
reading traces. For runs, the chosen plan includes this follow-up — execute
it without asking again:

1. Read the trace: `ha-nova trace latest <entity_id> --json` (entity is
   positional). Extract: which trigger fired, which condition passed or
   stopped the run, which action steps ran or errored. Automations keep only
   the last 5 traces by default.
2. Read the states of the acted-on entities — never infer device safety from
   a successful service response alone.
3. Restore what the confirmed card planned: manipulated trigger sources, an
   automation enabled only for the test (back to disabled), and — when the
   card said so — actuated devices back to their pre-test state (read before
   the run). Leave no test residue. Anything the card did not name needs its
   own previewed call.
4. Report compactly: path taken, deciding condition, end state of the
   devices, restore status.
5. Honesty line: one passing run proves this path once — not every branch,
   not future timing. Keep the untested scope in the wording.

## When the Test Fails

- No new trace after the run: the trigger did not fire. Report that plainly
  — do not guess. For a user-assisted physical test, check the source
  entity's recent history once to tell "device never reacted" apart from
  "trigger config did not match".
- Trace stopped at a condition or an action errored, or a device did not
  reach the expected state: report the exact stop point, then offer the next
  step — fix it (normal write flow), root-cause analysis (`ha-nova:diagnose`),
  or for updates the already-saved `revert` from the post-write review.
- A failed test never auto-triggers a config change; every fix goes through
  the regular write preview and confirmation.

## Offer Format (Test Plan card)

Follows `skills/ha-nova/SKILL.md` → Interactive Choices and the card rules
in `skills/ha-nova/output-rules.md` (labels localized at runtime):

- Recommended option first and marked; at most 3 options plus `skip`.
- One line per option: what runs, what it proves, what it switches.
- Real-run options carry a consequence line: the devices that will act, the
  end state, and the restore plan (including whether actuated devices are
  returned to their pre-test state).
- Options lead with what the user will experience in plain words; the
  technical binding (service, payload, `skip_condition`) stays on the card
  but never opens the line.
- High-consequence actions add an explicit warning line (for example: "this
  unlocks the front door for real").
- `skip` is always valid. On skip, the Verification Honesty wording
  (write-safety) keeps the untested scope in the closing sentence.
- Session de-escalation: after the user skips once, later writes in the same
  session shrink the offer to one line ("A test run is available — say the
  word."). An explicit user request re-expands the full card.

Example card (labels localized at runtime):

```
📝 Test: automation.morning_lights (saved — runtime not exercised yet)
Right now: presence + time conditions are met — a run would go through.
1 (recommended) — Real test: walk past the hall motion sensor and the
   hallway light comes on at 40% (full trigger path; also reacts:
   automation.hall_night_alert).
2 — Actions only: the hallway light comes on at 40% exactly as the
   automation would do it, without the motion trigger (automation.trigger,
   skip_condition: false).
3 — Logic check: I show how each condition evaluates right now; nothing
   switches.
skip — test later (I can check the trace after the next real run)
After a run I read the trace, verify the light, and switch it back off.
```
