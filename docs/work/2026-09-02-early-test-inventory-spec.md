# Early Test Inventory Preflight

Status: active

## Goal

Make CI reject missing test registration before Go setup, cache warming,
builds, or the full repository verification.

## Scope

- Run the existing inventory contract file once in a short `test-inventory`
  job after dependencies are installed, without a test-name filter that can
  silently match nothing.
- Gate `ci-gate` and every independent test or build job on `test-inventory`
  so expensive jobs can run in parallel after the preflight passes.
- Preserve GitHub Actions' default fail-fast error propagation.
- Keep every action in the new Cloud-sensitive job under the existing immutable
  action-pin verifier.
- Add no checker, dependency, matrix, release flow, or configurable knob.

## Acceptance

- The preflight precedes expensive CI work without serializing that work.
- A failing inventory oracle stops all five consumer jobs.
- A focused contract test pins ordering, consumers, and failure propagation.
