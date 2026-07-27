# PostCSS source-map traversal security fix

## Context

Dependabot alert 49 reports GHSA-r28c-9q8g-f849 in `postcss` versions through
8.5.17. HA NOVA receives `postcss` only as a development-only transitive
dependency through `vitest` and `vite`; no shipped runtime processes user CSS.

## Decision

- Update only the lockfile resolution from `postcss` 8.5.16 to the patched
  8.5.18 release.
- Do not add a direct dependency or change package manifests.
- Keep this security maintenance separate from the Cloud Remote feature PR.
- Do not add a README or release claim for an internal development dependency.

## Verification

- `npm ls postcss` resolves exactly 8.5.18.
- `npm audit` reports no known vulnerabilities.
- Typecheck, tests, build, and documentation fact-check pass.
