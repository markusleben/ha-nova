# MVP Scenario Pack Design

Date: 2026-02-27

## Goal

Define the smallest user-oriented E2E scenario pack that improves HA NOVA skill quality with measurable signal.

Constraints:
- App + Relay terminology.
- KISS/DRY.
- MVP-first, no fallback-heavy matrix.
- Real user prompts, not internal script prompts.

## P0 (Now)

Read-first Core Pack (declarative scenario JSON):
1. first 10 `switch.*` entity_ids
2. first 5 `light.*` entity_ids
3. first 15 `sensor.*` entity_ids
4. first 10 `binary_sensor.*` entity_ids

Reason:
- highest frequency first-user asks
- lowest risk
- best latency/routing signal

Per-scenario checks:
- final status contract line present
- output list is string array
- prefix correctness
- count rule via `count_mode` (`up_to` for inventory variance)
- duration bound (`max_duration_sec`)
- no `/health` before first `/ws`
- no proactive `doctor|ready|quick` before first `/ws`
- no helper-script shortcut usage

## P1 (Implemented 2026-02-28)

Add harness support for negative/behavioral scenarios:
- expected fail code per scenario (`expected_status`, `expected_error`)
- forbidden command patterns (`forbid_patterns`)
- required text in final explanation (`must_contain_text`)

Then add UX-trust scenarios:
1. forced `/health` preflight prompt should fail with `health_preflight_before_ws_detected`
2. forced proactive doctor prompt should fail with `proactive_doctor_or_ready_detected`
3. scope-boundary prompt (`create automation ...`) should return clear MVP boundary message

Implementation note:
- added second expectation mode `expect.type = "json_array_values"` for boundary/non-entity scenarios while preserving `NOVA_SCENARIO_RESULT` contract.

## Out of Scope (Not MVP)

- dashboard editing
- blueprint import
- backups/filesystem flows
- large scenario matrix across all clients
