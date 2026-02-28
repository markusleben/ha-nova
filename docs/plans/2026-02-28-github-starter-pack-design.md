# GitHub Starter Pack Design

Date: 2026-02-28

## Goal

Establish a minimal, production-ready GitHub repository baseline for `ha-nova`.

Focus:
- MVP-first.
- KISS.
- Strong contributor trust + low maintenance overhead.

## Scope (Now)

1. Add public repository contract files.
2. Add `.github` automation baseline (CI, dependency review, templates, ownership).
3. Add lightweight release-notes categorization.
4. Keep current runtime code and folder layout unchanged.

## Out of Scope (Now)

- No large repo refactor (`apps/*`, `packages/*`) in this step.
- No release publishing automation (Changesets/Release Please).
- No branch/ruleset mutation via CLI in this step.

## Deliverables

- Root files:
  - `README.md`
  - `LICENSE` (MIT)
  - `CONTRIBUTING.md`
  - `CODE_OF_CONDUCT.md`
  - `SECURITY.md`
  - `SUPPORT.md`
  - `CHANGELOG.md`
- GitHub files:
  - `.github/CODEOWNERS`
  - `.github/PULL_REQUEST_TEMPLATE.md`
  - `.github/release.yml`
  - `.github/dependabot.yml`
  - `.github/workflows/ci.yml`
  - `.github/workflows/dependency-review.yml`
  - `.github/workflows/codeql.yml`
  - `.github/ISSUE_TEMPLATE/bug.yml`
  - `.github/ISSUE_TEMPLATE/feature.yml`
  - `.github/ISSUE_TEMPLATE/config.yml`

## Verification

1. Ensure all new files exist.
2. Run local quality checks:
   - `npm run typecheck`
   - `npm test`
3. Confirm no unrelated file modifications were reverted.
