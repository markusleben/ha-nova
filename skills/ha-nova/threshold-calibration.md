# Threshold Calibration Preflight

History-based evidence before changing how an automation classifies a
PHYSICAL process (#484). A threshold edit can be structurally valid and still
silently remove a false-positive guard — a washer's low-power pause looks
exactly like "finished" until the history proves the pause lasts 40 minutes.

## When it applies

An UPDATE to an existing automation, script, or `threshold` helper
(`lower`/`upper`/`hysteresis`, or its compared `entity_id`) that changes any of:

- a `numeric_state` trigger/condition `above` or `below` value,
- its `for:` duration,
- a `wait_for_trigger` / `wait_template` timeout,
- the compared signal itself — swapping the trigger/condition `entity_id` or
  tested `attribute`, or changing its `value_template` (a transform can
  rescale or redefine the signal entirely, watts to kilowatts, while keeping
  the same entity), while keeping existing values; a changed signal has its
  own unit, scale, and noise, so inherited boundaries are uncalibrated,

where the compared entity measures a physical process (power, energy, current,
temperature, humidity, flow, level, …). It does NOT apply to unrelated
numeric-state edits — setting a light's brightness value or a volume level
classifies nothing and needs no history.

The same duty applies OUTSIDE config updates: setting a helper's VALUE
(`input_number.set_value`/`increment`/`decrement` or equivalent) changes the
effective threshold of every `numeric_state` consumer that references that
helper as `above`/`below`. Before such a service call, resolve the helper's
direct consumers (`search/related`); when one compares a physical-process
signal, run this preflight and carry its findings into the service-call
preview. The direct call is not the only path: an automation or script
UPDATE that adds or changes an ACTION invoking those setter services on a
threshold-backing helper moves the threshold on every future run — the
stored action triggers this preflight at update time, because no service
call happens while the config is written. A stored SCENE that assigns a
new state to such a helper is the same class: a scene update adding or
changing that member, and a `scene.apply` carrying it, both trigger the
preflight before the write or call.

## Evidence (read-only, bounded)

1. Recorder history for the compared VALUE, up to 30 days, bounded reads.
   When the trigger/condition tests an `attribute`, calibrate that attribute
   from bounded raw history — primary-state statistics describe a different
   value and may only shortlist windows when they represent exactly the
   tested attribute. When a `value_template` transforms the signal, the raw
   entity history is NOT the tested value: apply the same transform to each
   historical sample before deriving ranges and phases; a transform that
   cannot be reproduced offline (external entities, `now()`, non-pure
   inputs) makes the calibration insufficient — say so rather than
   calibrating the untransformed values.
   Check the statistics metadata first (`recorder/list_statistic_ids`): it
   names the available fields and the statistics unit. Only measurement-class
   statistics carry `min`/`mean`/`max`. For metered `total`/`total_increasing`
   statistics (energy, water, gas), match the field to the COMPARED dimension:
   a threshold on the absolute reading shortlists RANGE-SAFELY from
   per-bucket `state` — take every bucket whose value range (previous
   bucket's `state` through this bucket's `state`) straddles or touches the
   boundary. Endpoint-only tests miss in-bucket crossings (a bucket rising 4
   to 6 already ends across `below: 5`), and meter resets can hide an
   `above` phase — treat a decreasing absolute reading as a reset marker
   that forces raw-history inspection. Per-bucket `change` is interval
   consumption; only a consumption-style comparison shortlists from it. When
   no statistics field represents the tested value, skip the shortlist and
   go straight to bounded raw history. When the statistics unit differs from the entity's current unit
   (unit changed mid-history), normalize those samples into the threshold's
   unit or exclude them as a named data gap — never compare mixed units.
   Hourly `recorder/statistics_during_period` only
   SHORTLISTS candidate windows — hourly aggregates cannot order events or
   time a pause. Inspect the shortlisted windows with bounded raw `history`
   reads, and report durations as "longest observed at the available
   resolution". When the sensor is recorded but has no long-term statistics
   (no buckets returned), fall back to bounded, chunked raw `history` reads
   over the window — statistics absence is not evidence absence. Never
   unbounded.
2. When `above`/`below` references an `input_number`/helper instead of a
   literal, the boundary itself moved over the window: read the helper's
   bounded history too and time-align each sensor sample with the boundary
   value active at that moment — classifying old samples against only the
   proposed value misreads history. When the helper history is unavailable,
   mark the calibration insufficient rather than assuming a constant
   boundary. A numeric_state CONDITION evaluates at automation run time,
   not at sensor-sample time: align both histories to the
   condition-evaluation moments from existing traces; without usable
   traces, mark condition-time conclusions unverified instead of deriving
   them from sample-time alignment alone.
3. Type-specific run evidence when it exists: for automations and scripts,
   bounded reads of ALREADY-EXISTING traces (`trace/list`, `trace/get`) — an
   explicit preflight exception to the write flow's no-auto-trace rule; never
   trigger a run to create one. Threshold helpers have no traces and rely on
   history evidence alone.
4. From the data derive: the observed normal range on each side of the
   proposed value, the LONGEST ambiguous phase (time the signal sat on the
   "done" side of the threshold while the process was still running), and
   any data gaps (recorder downtime, entity unavailable windows).
   "Still running" needs independent evidence — a trace showing the process
   ended later, or another process-state signal; without it, mark the
   ambiguous phase as unverified instead of presenting it as fact.

## Preview duties

- Report the observed ranges and missing-data limitations — numbers, not
  adjectives. Compare the RIGHT duration to the right evidence: a debounce
  `for:` duration compares against the longest ambiguous phase; a
  `wait_for_trigger`/`wait_template` TIMEOUT runs from wait start until the
  trigger/template succeeds, so it compares against the observed
  start-to-crossing/completion latency — an ambiguous-dip comparison would
  falsely validate a timeout that always expires first.
- Threshold-helper changes are value-domain: for EVERY `lower`, `upper`, or
  `hysteresis` update, evaluate observed noise and excursions against the
  effective on/off boundaries (`lower`/`upper` ± hysteresis) so a boundary
  moved through noise — or an unvalidated false-toggle guard — cannot pass
  the preview unexamined.
- The behavior narrative states BOTH failure directions: what fires too early
  or falsely with the new values, and what fires late or never.
- Insufficient evidence (short history, large gaps, entity renamed recently)
  is said plainly; recommend keeping the existing values or an explicit user
  decision — never present an uncalibrated value as validated.
- Existing debounce, fallback, or guard branches are never removed on
  insufficient evidence without an explicit user decision naming what the
  guard protected against.
- The preflight is read-only; nothing changes outside the normal preview →
  confirm → write flow.
