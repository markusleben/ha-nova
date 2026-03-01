# Relay-Only Automation CRUD Performance Verification (Design)

Date: 2026-03-01  
Status: Proposed

## Goal

Define a low-noise, low-runtime verification strategy for the new relay-only automation CRUD path:
- fast CI-safe performance gate
- optional live performance check
- deterministic contract coverage for perf outputs

## Scope

In scope:
- relay request path used by automation CRUD (`create/get/update/delete` + `reload`).
- latency SLOs for CI and live checks.
- script/test layout and CI wiring with minimal overhead.

Out of scope:
- load/stress testing at production concurrency.
- new runtime transport features.
- replacing current functional smoke/e2e checks.

## Constraints

- Keep CI fast and stable on shared GitHub runners.
- Keep Relay lean (no business logic in server).
- Reuse existing project conventions (Vitest contracts + shell/node scripts).

## Strategy

Use a 2-tier model:

1. CI-safe micro-benchmark (blocking)
- deterministic local mode, no external HA dependency.
- validates latency budget + zero-error contract.
- runtime target: <= 15s total.

2. Live benchmark (non-blocking by default)
- real Relay + HA path for reality check.
- used in contributor loop/nightly/manual.
- produces artifact + summary; fail only when explicitly requested.

## Proposed SLOs

### CI-safe micro-benchmark SLOs (blocking)

Test mode: local relay instance + deterministic mocked upstream ws client.

- `error_rate` = `0%` (hard fail).
- Per operation (`create/get/update/delete/reload`):
  - `p50 <= 30ms`
  - `p95 <= 75ms`
  - `max <= 150ms`
- Full CRUD sequence (`create -> reload -> get -> update -> reload -> get -> delete -> reload -> get(404)`):
  - `p95 <= 450ms`

Why these numbers:
- wide enough for GitHub-runner variance.
- strict enough to catch accidental slow paths (sync blocking, extra round-trips, serialization regressions).

### Live benchmark SLOs (optional, report-first)

Test mode: real Relay endpoint + real HA backend.

- `error_rate` = `0%` expected (warn on first failure, hard fail only with `--assert`).
- Per operation:
  - `p95 <= 900ms` (`create/get/update/delete`)
  - `p95 <= 1500ms` (`reload`)
- Full CRUD sequence:
  - `p95 <= 8000ms`

These are operational SLO targets (environment-dependent), not default PR blockers.

## Contract Tests (fast, deterministic)

Add perf-contract tests only; keep benchmark heavy work out of `npm test`.

1. `tests/perf/relay-crud-perf-script-contract.test.ts`
- script exists and is executable.
- supports `--mode ci|live`, `--assert`, `--json-out`.
- prints stable machine line: `PERF_CRUD_RESULT ...`.

2. `tests/perf/relay-crud-perf-thresholds-contract.test.ts`
- threshold config schema is valid.
- required operations exist.
- `p50 <= p95 <= max` invariant holds for each operation.

3. `tests/perf/relay-crud-perf-output-contract.test.ts`
- JSON output includes:
  - metadata (`mode`, `timestamp`, `sample_size`, `warmup`)
  - per-op stats (`count`, `p50`, `p95`, `max`, `errors`)
  - sequence stats + pass/fail decisions

## Benchmark Script Approach

Add one script:
- `scripts/perf/relay-automation-crud-bench.mjs`

Modes:

1. `--mode ci`
- starts local relay server with injected deterministic ws client stub.
- runs warmup + bounded samples (default `warmup=3`, `samples=12`).
- computes percentiles in-process.
- enforces CI thresholds when `--assert` is set.

2. `--mode live`
- requires `RELAY_BASE_URL`, `RELAY_AUTH_TOKEN`, `MVP_AUTOMATION_ID` (or generated id).
- executes same CRUD sequence against real system.
- report-only by default; optional `--assert` for strict runs.

Output:
- console summary (single-line contract + table-like lines).
- JSON artifact (default: `artifacts/perf/relay-crud-bench.json`).

## Minimal CI Integration

`package.json` scripts (proposal):
- `perf:crud:ci`: `node scripts/perf/relay-automation-crud-bench.mjs --mode ci --assert`
- `perf:crud:live`: `node scripts/perf/relay-automation-crud-bench.mjs --mode live`

CI workflow:
- add one step after `npm test`:
  - `npm run perf:crud:ci`
- expected added CI time: ~10-15s.

Live checks:
- manual contributor loop and/or scheduled workflow.
- non-blocking by default to avoid noisy failures from environment drift.

## Rollout Plan

1. Add script + threshold config + perf contract tests.
2. Add `perf:crud:ci` step to CI.
3. Document live usage in contributor guide.
4. After 1-2 weeks of data, tighten or relax SLOs once based on observed variance.

## Definition of Done

- CI has deterministic perf gate for relay-only CRUD path.
- Perf contracts protect script/output/threshold schema from drift.
- Contributors can run optional live benchmark without changing CI stability.
