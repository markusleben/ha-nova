# 2026-03-27 R-18 Reproduction Runbook

Goal:
- verify whether the historical `R-18` runtime failure still reproduces on the current Home Assistant stack
- keep product behavior advisory-only until one current repro path is confirmed again

Rules:
- use disposable test entities only
- use harmless actions only (`logbook.log` or `persistent_notification.create`)
- clean up every temporary script/automation after each run
- compare traces before and after the write path; do not infer failure from static config shape alone

Matrix:

| Case | Domain | Variables block | Origin | Write path | Expected evidence |
|------|--------|-----------------|--------|------------|-------------------|
| A | script | top-level | API-created | read -> write back unchanged | trace shows whether sibling dependency still survives |
| B | script | local block | API-created | read -> write back unchanged | same as A |
| C | automation | top-level | UI-created | read -> write back unchanged | closest match to original issue report |
| D | automation | local block | UI-created | read -> write back unchanged | same storage risk in nested block |
| E | automation | top-level | YAML/imported then UI-saved | save in UI, then read -> write | checks whether UI normalization changes behavior |
| F | control | safe pattern | any | same write path | trace stays clean; no false positive |

Per-case procedure:
1. Create the disposable script/automation with a known sibling-variable dependency pair such as `check_flag -> reading`.
2. Run it once and capture the successful trace.
3. Read the config through the normal API path.
4. Write it back unchanged through the same API path.
5. Run it again and compare the trace.
6. Record:
   - whether the trace succeeds
   - whether `UndefinedError` appears
   - whether the stored config order changed
   - whether the entity type or origin changes the outcome
7. Delete the disposable entity and verify absence.

Fragile sample pattern:

```yaml
variables:
  check_flag: "{{ reading > -998 }}"
  reading: "{{ states('sensor.flow_rate') | float(-999) }}"
```

Safe control patterns:

```yaml
variables:
  reading: "{{ states('sensor.flow_rate') | float(-999) }}"
  check_flag: "{{ reading > -998 }}"
```

```yaml
variables:
  flow_status: >
    {% set reading = states('sensor.flow_rate') | float(-999) %}
    {{ 'ok' if reading > -998 else 'missing' }}
```

Decision rule:
- if at least one current case reproduces `UndefinedError`, keep `#128` framed as a real runtime risk with advisory guardrails
- if no current case reproduces, keep `R-18` as a defensive storage-fragility warning and avoid further runtime hardening until new evidence appears
