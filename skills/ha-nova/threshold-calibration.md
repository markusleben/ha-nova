# Threshold Calibration Preflight

History-based evidence before changing how an automation classifies a
PHYSICAL process (#484). A threshold edit can be structurally valid and still
silently remove a false-positive guard — a washer's low-power pause looks
exactly like "finished" until the history proves the pause lasts 40 minutes.

## When it applies

An UPDATE to an existing automation, script, or `threshold` helper
(`lower`/`upper`/`hysteresis`) that changes any of:

- a `numeric_state` trigger/condition `above` or `below` value,
- its `for:` duration,
- a `wait_for_trigger` / `wait_template` timeout,

where the compared entity measures a physical process (power, energy, current,
temperature, humidity, flow, level, …). It does NOT apply to unrelated
numeric-state edits — setting a light's brightness value or a volume level
classifies nothing and needs no history.

## Evidence (read-only, bounded)

1. Recorder history for the compared VALUE, up to 30 days, bounded reads.
   When the trigger/condition tests an `attribute`, calibrate that attribute
   from bounded raw history — primary-state statistics describe a different
   value and may only shortlist windows when they represent exactly the
   tested attribute.
   Hourly `recorder/statistics_during_period` (`min`/`mean`/`max`) only
   SHORTLISTS candidate windows — hourly aggregates cannot order events or
   time a pause. Inspect the shortlisted windows with bounded raw `history`
   reads, and report durations as "longest observed at the available
   resolution". When the sensor is recorded but has no long-term statistics
   (no buckets returned), fall back to bounded, chunked raw `history` reads
   over the window — statistics absence is not evidence absence. Never
   unbounded.
2. Type-specific run evidence when it exists: for automations and scripts,
   bounded reads of ALREADY-EXISTING traces (`trace/list`, `trace/get`) — an
   explicit preflight exception to the write flow's no-auto-trace rule; never
   trigger a run to create one. Threshold helpers have no traces and rely on
   history evidence alone.
3. From the data derive: the observed normal range on each side of the
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
