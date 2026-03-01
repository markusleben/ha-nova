# Relay-Only Automation CRUD Performance Verification (Design)

Date: 2026-03-01  
Status: Proposed

## Goal

Define a low-noise, low-runtime verification strategy for the new relay-only automation CRUD path:
- fast CI-safe performance gate
- deterministic output contract

## Scope

In scope:
- relay request path used by automation CRUD through `POST /core`.
- latency SLOs for CI checks.
- script/test layout and CI wiring with minimal overhead.

Out of scope:
- live benchmark mode in this first delivery (follow-up phase).
- load/stress testing at production concurrency.
- replacing current functional smoke/e2e checks.

## Constraints

- Keep CI fast and stable on shared GitHub runners.
- Keep Relay lean (MITM transport only).
- Reuse existing project conventions (Vitest contracts + shell/node scripts).

## Strategy

Use one blocking CI-safe benchmark in v1:
- deterministic local mode, no external HA dependency.
- runs real relay `/core` path and real `rest-client` against a local fake HA HTTP server (loopback).
- validates zero-error + sequence-latency budget.
- runtime target: <= 15s total.

## Proposed SLOs (CI v1)

Blocking gates:
1. `error_rate = 0%` (hard fail)
2. sequence `p95 <= 650ms` for:
   - `create -> get -> update -> get -> delete -> get(404)`
3. `relay_calls_per_write == 1` in nominal write path (hard fail)

Sampling/stability defaults:
- `warmup = 5`
- `samples = 30`
- `max` is report-only (not a blocker) to reduce flakiness on shared runners.

## Contract Tests (minimal v1)

One fast contract test only:
- `tests/perf/relay-crud-perf-script-contract.test.ts`
  - script exists/executable
  - supports `--mode ci`, `--assert`, `--json-out`
  - emits one machine line: `PERF_CRUD_RESULT ...`
  - JSON output includes required metadata and pass/fail fields

## Benchmark Script Approach

Add one script:
- `scripts/perf/relay-automation-crud-bench.mjs`

Mode:
- `--mode ci`
  - starts local relay server
  - starts local fake HA HTTP server (loopback)
  - routes `/core` through real relay transport + real rest client
  - runs warmup + samples (`warmup=5`, `samples=30`)
  - computes percentiles in-process (monotonic clock)
  - enforces CI thresholds with `--assert`

Output:
- console summary (single-line contract + table-like lines).
- JSON artifact (default: `artifacts/perf/relay-crud-bench.json`).
- include auth-source metadata:
  - `client_has_ha_llat` (must be `false` in benchmark environment)
  - `upstream_auth_source` (must be `relay_runtime`)

## Minimal CI Integration

`package.json` scripts (proposal):
- `perf:crud:ci`: `node scripts/perf/relay-automation-crud-bench.mjs --mode ci --assert`

CI workflow:
- add one step after `npm test`:
  - `npm run perf:crud:ci`
- expected added CI time: ~10-15s.
- run this step only on relay/skills/perf path changes (or nightly fallback) to keep PR latency low.
- optional retry-once before final fail to reduce shared-runner flakiness.

## Rollout Plan

1. Implement relay MITM endpoint (`POST /core`) first.
2. Add CI benchmark script + minimal contract test.
3. Add `perf:crud:ci` step to CI (path-scoped trigger).
4. Tune thresholds after 1-2 weeks of data.
5. Add live benchmark mode as explicit follow-up.

## Definition of Done

- CI has deterministic perf gate for relay-only CRUD path.
- Gate measures real relay `/core` path, not stub-bypassed internals.
- Auth model proof is present (`client_has_ha_llat=false`, `upstream_auth_source=relay_runtime`).
