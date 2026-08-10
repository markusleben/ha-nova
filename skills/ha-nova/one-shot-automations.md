# One-Shot And Temporary Automations

Canonical path: `skills/ha-nova/one-shot-automations.md`

Split out of `skills/ha-nova/automation-patterns.md` to keep that file under
the repo's ~400-line ceiling. Everything here is about automations that must
not outlive their purpose: fire-once notifications, and actions bounded by a
duration.


"Only today", "just once", "remind me when the laundry finishes" — the most
common everyday automation request is one that should not outlive its purpose.
A permanent automation the user has to remember to delete is the wrong answer.

Self-disabling is the Home Assistant-native one-shot: the automation turns
itself off with `automation.turn_off` on `{{ this.entity_id }}`, which
survives a restart (a long `delay` or `wait_for_trigger` does not) and re-arms
with a single toggle. WHERE that step sits in the sequence is decided per
pattern below, not fixed — it goes where a failure leaves the safer state.

```yaml
alias: "One-shot: tell me when the washing machine finishes"
mode: single
triggers:
  - trigger: state
    entity_id: sensor.washing_machine_state
    from: "running"          # NEVER `to:` alone — a to-only state trigger also
    to: "finished"           # fires when the entity returns from `unavailable`
                             # or is re-added at restart, which announces a
                             # finished load that never ran and burns the
                             # one-shot
actions:
  - action: automation.turn_off
    target:
      entity_id: "{{ this.entity_id }}"
    data:
      stop_actions: false
  - action: notify.mobile_app_phone
    data:
      message: "Laundry is done."
```

Two details carry this pattern, and both are easy to get backwards:

- **Disable FIRST, act second.** Home Assistant aborts an action sequence when
  a step errors, so a disable placed last never runs if the notification
  service is down or rejects its payload — and the one-shot stays armed for
  the next matching transition, firing days later with nobody expecting it.
  Disabling first makes "at most once" hold even when the action fails.
- **`stop_actions: false` is not optional.** That field defaults to `true`, so
  an automation turning ITSELF off cancels its own remaining steps — the
  notification would never be sent.

`{{ this.entity_id }}` avoids naming the automation inside itself, so a rename
cannot break the disable step.

A duration-bound request ("run the sprinkler for 30 minutes") is a WRITE, not
a service call, even though it starts with one. `ha-nova:service-call` runs
the turn-on and stops there; nothing schedules the turn-off. Route the whole
request to `ha-nova:write`, which owns both halves — say plainly that you are
creating a short-lived automation rather than just switching something on.

"For a duration" does not always mean "then turn it off". A thermostat set to
18 °C for an hour has to go back to what it was, not to off — so capture the
CURRENT value of every attribute the first half changes, and make the expiry
automation restore that captured value. Only a request whose natural
counter-action is off gets a plain turn-off — and "natural" means the
direction the request goes: "sprinkler for 30 minutes" turns off afterwards,
"light off for an hour" turns back ON. Capture the prior state either way; the
counter-action is whatever restores it, not a fixed service.
Read the value at write time and embed it, exactly like the deadline — and
re-read it at apply time for the same reason the deadline is re-checked: if
another client moved the thermostat from 21 to 23 during the confirmation
pause, restoring the previewed 21 would overwrite a change the user never saw.
A moved value re-previews rather than being embedded.

Two failure paths need naming because they leave the automation armed with
nothing to undo. If the immediate action definitively FAILS, clean up the expiry
automation you just created — through `ha-nova:write`'s delete flow, which
takes its own typed confirmation even for something created seconds ago;
disable it immediately so it cannot fire while that confirmation is pending — the temporary state never began, and leaving it
armed means a later unrelated change gets reverted at the deadline. And if
someone changes the target DURING the window (thermostat moved from your
temporary 18 to 23), the captured value is no longer what they want restored:
say so and offer to cancel the expiry rather than reverting a deliberate
change. For a RESTORE-direction counter-action (turn back on, put the setpoint back)
the expiry reads the current value at fire time and skips when it no longer
matches the temporary one it set — that also covers a session that died
between creating the expiry and running the action. A SAFETY-direction
counter-action (close, turn off) never skips: closing an already-closed valve
is free, and not closing an open one is the failure the pattern exists for.

Both halves means both, and the ORDER is the safety property: create the
expiry automation FIRST, verify it exists, and only then run the immediate
action. Check the deadline again at that moment, and require MARGIN, not just a future
deadline: the expiry automation is already armed, so a deadline a few seconds
out can fire and disable itself before the immediate action even runs — the
device then starts with nothing left to stop it. Under about a minute of
remaining time, re-preview with a fresh deadline instead of actuating. A
confirmation that arrives after the deadline re-previews for the same reason.

If the immediate action grants physical access ("unlock the front door for
five minutes", "open the garage for ten"), the one preview still takes the
typed `confirm:<token>` — a duration does not soften what the first half
does, and the automatic re-lock is not a mitigation because the door is open
for the whole window. Same tier as calling the service directly
(`skills/ha-nova/SKILL.md` → confirmation tiers) — and `ha-nova:write` applies
that tier here even though its own create flow is otherwise natural
confirmation: the duration request is the exception, because its first half is
a runtime access grant rather than a config change.

Preview all three parts, not two: the immediate action, the expiry automation,
and what happens if the action fails — that the automation is disabled and
cleaned up. A cleanup the user never saw previewed is a change they did not
confirm. Reverse that and a failed automation write leaves the valve open with
nothing scheduled to close it — reporting the partial result does not help a
user who has already walked away. Both go under one preview; if the automation
write fails, nothing has been turned on yet and there is nothing to undo.

The counter-action must also survive Home Assistant being down at the
deadline. A bare `time` trigger is MISSED, not replayed, so the valve stays
open until someone notices. Give the expiry automation a second trigger on
startup and let a condition decide:

```yaml
alias: "One-shot: close irrigation valve at 19:30"
mode: single
max_exceeded: silent   # the close triggers a state change this run is still
                       # waiting on; the re-entry is expected
triggers:
  - trigger: time
    at: "19:30:00"
  - trigger: homeassistant
    event: start
  # and when the device finally shows up: an integration slower than the
  # startup wait would otherwise strand the valve until the next restart
  # NOT `from: "unavailable"` — an entity that did not exist yet first
  # appears from `None`/`unknown`, so bind the trigger to the usable state
  # instead of to a particular unusable one
  - trigger: state
    entity_id: valve.irrigation_lawn
    to: ["open", "closed", "opening", "closing"]
  # and a slow retry while it is still open past the deadline: a transient
  # close failure otherwise waits for a restart or the next day
  # repeating, not once: a single five-minute trigger fires before the
  # deadline, gets rejected by the condition, and never comes back
  - trigger: time_pattern
    minutes: "/5"     # bounded by the give-up arm below — an unbounded retry
                      # on a jammed actuator runs forever and its no-op traces
                      # evict the real one (HA keeps 5 by default)
conditions:
  # on the time path this is already true; on the startup path it catches a
  # deadline that passed while Home Assistant was off
  - condition: template
    # the ABSOLUTE deadline, substituted at write time. Take the zone from
    # Home Assistant (`GET /api/config` -> `time_zone`), never from this
    # machine, and render `at:` in that same zone — a UTC agent writing a
    # Berlin deadline puts the trigger two hours before the condition allows,
    # so the one-shot survives its own deadline. The offset is mandatory: a
    # naive datetime makes the comparison raise and the expiry never fires. `today_at('19:30')`
    # would mean today's 19:30 on every later day: a restart the next morning
    # reads it as not-yet-due and leaves the valve open, and a cross-midnight
    # duration closes it hours early.
    value_template: "{{ now() >= as_datetime('2026-08-09T19:30:00+02:00') }}"
actions:
  # ALWAYS attempt the safe state first, even long past the window: a restart
  # three hours late is precisely when the valve is still open. Giving up
  # before trying would strand it open forever — the opposite of recovery.
  #
  # On the startup path the integration may not have the entity yet, and a
  # call against an unavailable target fails silently. Wait for a KNOWN state:
  # an entity that does not exist yet is neither `unavailable` nor usable, so
  # negating `unavailable` alone passes instantly.
  - wait_template: >-
      {{ states('valve.irrigation_lawn') not in
         ['unavailable', 'unknown'] }}
    timeout: "00:02:00"
    # continue, then BRANCH. `continue_on_timeout: false` would stop the run
    # here, which also skips the give-up path below — an entity that never
    # comes back would retry every five minutes forever. Continuing and
    # checking explicitly avoids both that and the blind call against an
    # unusable target.
    continue_on_timeout: true
  - if:
      - condition: not
        conditions:
          - condition: state
            entity_id: valve.irrigation_lawn
            state: ["unavailable", "unknown"]
    then:
      - action: valve.close_valve
        target: {entity_id: valve.irrigation_lawn}
        # a raising call — the entity goes unavailable between the check and
        # the call, or the integration rejects it — aborts the whole sequence,
        # which past the cutoff would skip the terminal disarm and leave the
        # five-minute retry unbounded. Let it fail and decide below.
        continue_on_error: true
      # HA accepting the call is not the device having closed, so confirm the
      # safe state before removing the only retry
      - wait_template: "{{ is_state('valve.irrigation_lawn', 'closed') }}"
        timeout: "00:01:00"
  - if:
      - condition: state
        entity_id: valve.irrigation_lawn
        state: "closed"
    then:
      - action: automation.turn_off
        target: {entity_id: "{{ this.entity_id }}"}
        data: {stop_actions: false}
      - stop: "closed"
  # not closed. Inside the window, stay armed and TELL someone — a condition
  # that silently stops the run means the lawn floods and the only artifact is
  # a trace nobody opens.
  - if:
      - condition: template
        value_template: >-
          {{ now() < as_datetime('2026-08-09T19:30:00+02:00')
             + timedelta(hours=2) }}
    then:
      - action: notify.mobile_app_phone
        data: {message: "Irrigation valve did not close — retrying."}
      - stop: "not closed yet"
  # past the window: DISARM FIRST. A notification service that is down or
  # rejects the payload aborts the sequence, so a turn_off placed after it
  # never runs and the five-minute retry resurrects itself forever. The
  # upper bound also must NOT sit in `conditions:` — there it would fail the
  # very trigger that disables this. Expiring is a state to reach, not one to
  # fall out of.
  - action: automation.turn_off
    target: {entity_id: "{{ this.entity_id }}"}
    data: {stop_actions: false}
  # the CURRENT state, not "failed": "still open" and "still opening" send a
  # person to different places
  - action: notify.mobile_app_phone
    data:
      message: >-
        Irrigation valve is {{ states('valve.irrigation_lawn') }} two hours
        after its deadline. Giving up — it needs a look.
```

Note the order is the OPPOSITE of the notification one-shot above, and for the
same reason: put the disable where a failure leaves the safer state. A
notification that fails is spent, so disable first or it fires again
tomorrow. A close that fails must be retried, so disable LAST — Home Assistant
aborts the sequence on the error, the automation stays armed, and the startup
trigger closes the valve on the next restart. Never disable a safety
counter-action before it has succeeded.

A `delay` inside the run is for short waits only — it does not survive a
restart at all.

A deadline-bound one-shot needs a second way out. "Only today at 19:00" that
never fires — Home Assistant was down, the trigger entity never reached its
state — stays armed and goes off tomorrow, which is precisely the surprise the
pattern exists to prevent. Give it an expiry the main trigger cannot skip:

```yaml
triggers:
  - trigger: state
    entity_id: sensor.washing_machine_state
    from: "running"          # NEVER `to:` alone — a to-only state trigger also
    to: "finished"           # fires when the entity returns from `unavailable`
                             # or is re-added at restart, which announces a
                             # finished load that never ran and burns the
                             # one-shot
    id: fired
  - trigger: time
    at: "23:59:00"
    id: expired
  # a missed 23:59 leaves this armed for tomorrow, so recover at startup —
  # but only when the deadline really has passed, or an ordinary restart at
  # 18:00 would disable a one-shot that still has the evening to fire
  - trigger: homeassistant
    event: start
    id: expired
conditions:
  - condition: or
    conditions:
      # the primary path still has to be inside the window: if HA was down at
      # 23:59 and the sensor reaches its target during startup, this fires
      # after the deadline and must not count
      - condition: and
        conditions:
          - condition: trigger
            id: fired
          - condition: template
            value_template: "{{ now() < as_datetime('2026-08-10T23:59:00+02:00') }}"
      # the expiry arm must ALSO be scoped to its own triggers, or a
      # post-deadline `fired` satisfies this one and still reaches the
      # notification branch
      - condition: and
        conditions:
          - condition: trigger
            id: expired
          - condition: template
            # the ABSOLUTE deadline, substituted at write time. Take the zone from
    # Home Assistant (`GET /api/config` -> `time_zone`), never from this
    # machine, and render `at:` in that same zone — a UTC agent writing a
    # Berlin deadline puts the trigger two hours before the condition allows,
    # so the one-shot survives its own deadline. The offset is mandatory: a
    # naive datetime makes the comparison raise and the expiry never fires — `today_at`
            # re-reads as the CURRENT day, so a restart the next morning
            # would see 23:59 as still ahead and leave this armed
            # NO upper bound here. A first restart three hours late would
            # fail it, and then this automation never reaches its own
            # `turn_off` and stays armed forever. The staleness bound belongs
            # to the MESSAGE, below, not to the disarm.
            value_template: >-
              {{ now() >= as_datetime('2026-08-10T23:59:00+02:00') }}
actions:
  # disable first on both paths — a notification is spent whether or not it
  # was delivered, and a failed send must not leave this armed for tomorrow
  - action: automation.turn_off
    target: {entity_id: "{{ this.entity_id }}"}
    data: {stop_actions: false}
  - choose:
      - conditions: [{condition: trigger, id: fired}]
        sequence:
          - action: notify.mobile_app_phone
            data: {message: "Laundry is done."}
      # an expiry that says nothing leaves the user waiting for a message
      # that will never come
      - conditions:
          - condition: trigger
            id: expired
          # only say it while it is still worth saying: a restart hours later
          # must still DISARM (it already did, above), but "your timer
          # expired" about last night is noise
          - condition: template
            value_template: >-
              {{ now() < as_datetime('2026-08-10T23:59:00+02:00')
                 + timedelta(hours=2) }}
        sequence:
          - action: notify.mobile_app_phone
            data: {message: "Laundry watch expired — it did not finish today."}
```

The disable runs on both paths, so the automation is gone either way — and the
startup trigger means a deadline missed while Home Assistant was down still
clears it, instead of leaving it to fire on tomorrow's laundry. Say in
the preview when it expires.

Rules for this family:
- Name it so it is findable later: start the `alias` with `One-shot:`. A label
  would be tidier, but labels are entity-registry metadata that
  `ha-nova:organize` owns and the write flow does not touch — promising one
  here would leave every one-shot unlabelled. The alias is written with the
  automation itself, so it is always there.
- Say in the preview that it disables itself after running once, and that it
  stays in the automation list until deleted.
- After it has fired, offer to delete it — do not delete anything unprompted.
- A recurring request ("every Monday") is NOT this pattern: that is an ordinary
  automation with a time trigger.
