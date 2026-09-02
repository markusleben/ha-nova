# Early Test Inventory Preflight

Status: active

## Goal

Make CI reject missing test registration before Go setup, cache warming,
builds, or the full repository verification.

## Scope

- Run the existing `runs every test file through verify` oracle once as an
  early `ci-gate` preflight after dependencies are installed.
- Gate every independent test or build job on `ci-gate` so none can bypass the
  preflight.
- Preserve GitHub Actions' default fail-fast error propagation.
- Add no checker, dependency, matrix, release flow, or configurable knob.

## Acceptance

- The preflight precedes expensive CI work.
- A failing inventory oracle stops the job.
- A focused contract test pins both properties.
