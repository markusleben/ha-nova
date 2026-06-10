# 2026-04-13 Dependabot Release Preflight Spec

- Goal: classify open Dependabot PRs into blocker now vs separate later for release prep.
- Method: inspect open PRs via gh and verify production dependency risk with npm audit / project scripts.
- Scope: only release relevance, not full PR review.
