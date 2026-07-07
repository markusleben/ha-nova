# Release Note Comparison Spec

Date: 2026-04-08
Status: completed

## Goal

Produce a decision-complete release-note comparison artifact for the next HA NOVA release train.

## Scope

- Resolve the baseline from the latest published stable GitHub release, not from drafts or historical release branches.
- Resolve the target from the exact compare SHA; use `origin/main` only as a provisional preview target when no release SHA exists yet.
- Build a full delta classification table before drafting any public note text.
- Keep the output user-facing and short.

## Current Target

- Baseline release: `v0.3.2`
- Baseline shape: published stable release
- Provisional compare target: `main` at `b5e94aa`
- Compare window: `v0.3.2..b5e94aa`
- Current unpublished delta size: 4 commits total, but only 1 product-feature commit (`#162`) plus 3 docs-only follow-up commits

## Rules Applied

- Compare by exact SHA, not by branch name alone.
- Treat promoted named supported workflows as `New Features`, even when the raw capability previously lived behind `fallback`.
- Classify by changed user contract, not by directory.
- Group by user-facing behavior slice, not by commit headline.
- Exclude workflow/test/spec/harness churn unless it changes shipped user behavior directly.
- Do not derive or propose the next release version in this pass.

## Deliverables

- Analysis matrix with explicit include/omit decisions for every user-facing delta slice.
- Exclusion audit for internal-only churn.
- Recommended release-note draft in the current short stable-release style.
- Install block kept as a tag placeholder template until a real release tag exists.
