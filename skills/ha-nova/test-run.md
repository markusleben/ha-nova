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
- Single confirmation: the Test Plan Card doubles as the runtime-call
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
| ordinary devices: lights, switches, media, fans, non-access covers, comfort climate | real run — name every device plus the restore plan |
| high consequence: locks, garage doors, alarm panels, valves, water heaters, heating setpoints, `homeassistant.restart`, anything irreversible | logic check first; a real run stays available but carries an explicit warning line and is never presented as consequence-free |

Domain alone is not enough for covers and climate: check `device_class` and
what the entity controls before using the ordinary row — a garage door,
gate, or entry door exposed as `cover.*` and any heating/cooling setpoint
change belong in the high-consequence row.

Co-listeners escalate: the risk class covers everything the run can set in
motion, not just the tested automation's own actions. Before recommending
any run option, check `search/related` on every entity the test will change
— manipulated trigger sources and action targets alike (fail-closed read per
`skills/ha-nova/relay-api.md` → Parsing rule; a failed scan means the
co-listener check is incomplete — say so on the card, never treat it as
"no co-listeners"). If a named
co-listener performs physical or high-consequence actions (the classic
pattern: a helper toggle that another automation answers by unlocking a
door), that run option inherits the risk level — even when the tested
automation's actions are purely logical.

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
- If the automation is disabled: `automation.trigger` runs the actions even
  while it is off, so actions-only tests never enable it. Only a full
  real-path test needs temporary enablement — name it on the card, and the
  post-run restore returns it to disabled.
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
  service to fake it — arm the baseline first (sequence: User-Assisted
  Readiness below), only then ask the user to trigger the device physically,
  then read the trace; otherwise fall back to "run actions now".
- `template`: run the logic check first; then manipulate the underlying
  entities as above.
- `event`: `POST /api/events/<event_type>` with the payload the trigger
  expects. Fires for every listener in the instance — name co-listeners.
- `mqtt`: publish the expected topic and payload via `ha-nova:mqtt`.
- Branches keyed on `trigger.id`: one real-path run per branch, each via its
  own trigger source.

## User-Assisted Readiness (physical-action tests)

When the plan needs the user to act physically (motion, door, presence, a
device's own button), follow context skill → User-Assisted Readiness. Trace
evidence is persistent, so arming is baseline capture — there is no listen
window to miss, but traces rotate (automations keep only ~5 by default): on
a busy automation ask the user to act promptly, and re-capture the baseline
before instructing again if runs pile up in between:

1. The card choice selects the test.
2. Arm BEFORE any instruction: capture the current run_id baseline
   (Post-Run Verification step 1) and read the trigger source's state.
3. Confirm readiness in one line: "Baseline captured — ready when you are."
4. Instruct exactly one action: the device, the movement, and when — "walk
   past the hall motion sensor now, then tell me when done." Name any `for:`
   hold the trigger needs.
5. After the user reports done, list the traces and inspect every run newer
   than the captured baseline — not only the latest, since an unrelated
   trigger can fire after the user acted and hide the matching run. Accept
   the newest run whose fired trigger matches the requested source; if none
   matches, the new traces are not this test's result — say so and offer a
   retry. Then verify device states, restore per the card, and report.

Never tell the user to act before step 2 is complete. A test whose trigger
listens on MQTT verifies via `ha-nova:mqtt` bounded-window readiness instead
(the window cannot wait — ready-check first, "act now" as it opens).

## Post-Run Verification (automatic after any consented run)

Applies to actions-only and real-path runs. A logic check creates no run
and no trace — report the rendered condition/template results instead of
reading traces. For runs, the chosen plan includes this follow-up — execute
it without asking again:

1. Capture the latest run_id before the run
   (`ha-nova trace list <entity_id> --json`). After
   the run, read `ha-nova trace latest <entity_id> --json` (entity is
   positional) and accept it only if its run_id is new — otherwise treat it
   as "no new trace", never report a stale run as the test result. Extract:
   which trigger fired, which condition passed or stopped the run, which
   action steps ran or errored. Automations keep only the last 5 traces by
   default.
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

## Offer Format (Test Plan Card)

This is the Test Plan Card of `skills/ha-nova/output-rules.md` → Cards; menu
mechanics follow `skills/ha-nova/SKILL.md` → Interactive Choices (labels
localized at runtime):

- Recommended option first and marked; at most 3 options plus `skip`.
- Each option opens with its effect class — Logic check / Actions only / Real
  test (scripts: Run script) — so a bare number always maps to a named
  effect; then one line: what runs, what it proves, what it switches.
- Real-run options carry a consequence line: the devices that will act, the
  end state, and the restore plan (including whether actuated devices are
  returned to their pre-test state).
- After the effect class, options lead with what the user will experience in
  plain words; the technical binding (service, payload, `skip_condition`)
  stays on the card but never opens the line.
- On a bare-number reply, restate the chosen effect in the next response
  before executing ("Actions only — running the hallway light sequence
  now"); never start what a bare number selected without naming it.
- Physical-action options describe the upcoming action, never command it
  ("you trigger the hall sensor when I say go") — the imperative "act now"
  instruction is reserved for User-Assisted Readiness step 4, after arming.
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
