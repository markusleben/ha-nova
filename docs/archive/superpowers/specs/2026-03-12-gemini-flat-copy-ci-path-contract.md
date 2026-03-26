# Spec: Gemini Flat-Copy Path Contract in CI

Date: 2026-03-12

## Problem

`tests/onboarding/install-skills-per-client.test.ts` assumed rewritten flat-copy references always contain `/Users/...`.

GitHub Actions runs on Linux, so valid rewritten repo paths become `/home/runner/work/...` and the contract fails even though the installer behavior is correct.

## Decision

Treat the contract as:

- relative backtick refs like `` `skills/...` `` and `` `docs/reference/...` `` must be rewritten
- rewritten refs must point to an absolute repo path
- the contract must stay cross-platform

## Scope

- test-only fix
- no runtime installer/update behavior change
