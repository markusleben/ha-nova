# Client Registry Bundle Parity Design

Date: 2026-03-17

## Problem

The new early-client-availability flow depends on `clients/registry.json`.
Windows setup crashed because install bundles did not ship that file and runtime code still panicked when the registry was missing.

## Goal

- ship `clients/registry.json` in every install bundle
- reject staged bundles that omit it
- return normal command errors instead of panicking when the registry cannot be loaded

## Non-Goals

- no registry schema expansion
- no fallback embedded registry
- no new user-facing flags

## Design

1. Bundle builder copies `clients/` into the bundle root.
2. `validateBundleRoot()` requires `clients/registry.json`.
3. Registry-dependent runtime helpers return errors up the stack instead of panicking.
4. Setup/doctor/update surface the error through existing command error output.
5. Release contract tests inspect built bundles for the registry file.

## Success Criteria

- setup on a bad bundle fails with a readable error, not a panic
- freshly built bundles contain `ha-nova/clients/registry.json`
- staged update validation rejects bundles without the registry
