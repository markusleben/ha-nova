# Backlog Sequencing 2026-08

Status: merged (all three phases shipped and tagged as v0.23.0 via #495–#507; archived 2026-08-09)
Scope: successor to [masterplan-2026-h2.md](masterplan-2026-h2.md) as the SSOT for sequencing and decisions. Orders the open issues (13 as of 2026-08-03) into three phases following the proven "harden what exists before widening surface" logic.

## Maintainer decisions (2026-08-03)

- Phase 3 order: HACS lifecycle (#478) before multi-server wizard layer 2 (#411).
- Release cut: Phases 1+2 bundle into ONE release after Phase 2; each Phase-3 feature ships its own release. All of these change delivery machinery at some point, so the RC rehearsal applies per `docs/releasing.md`.
- #463 (Odysseus) is externally blocked (no directory-based skill loading upstream; two reporter findings pending) — parked, not planned.
- #478 revises the masterplan's "Deliberately NOT: HACS management parity" entry — maintainer-confirmed scope in the issue thread, with the solaredgeoptimizers migration as the reference case.

## Phases

| Phase | Theme | Issues (order) | Release |
|-------|-------|----------------|---------|
| 1 | Safety & correctness | #493+#489+#494 (one PR: write-path fail-closed + output-contract allowlist) → #482 ∥ #446 | — (collect in 0.23.0 draft) |
| 2 | Quality & visibility of existing features | #452 → #483 → #484 → #440 → #444 (release prep) | v0.23.0 bundled, with RC |
| 3 | New capabilities | #478 (spec → impl) → #411; #463 parked | one release per feature, with RC |

## Process notes

- **Evidence batching via open PR stack (maintainer decision 2026-08-03, revised same day):** no per-PR Cloud evidence cycles and no release until the program's end — and no required check is bypassed. Each work item completes its full review cycle (real Codex clean on the head SHA, all non-evidence checks green, threads resolved) and then STAYS OPEN; `cloud-source-gate` remains red on those open PRs until the end. Branch new items from `main` when files do not overlap; stack on the predecessor branch only when they do. The final pre-release merge train then processes the stack in sequence: rebase if needed, fresh `@codex` clearance for any new SHA (clearance is SHA-specific), fresh evidence envelope per merge target, merge — including the reference-platform Cloud health smoke that #482's device-registry delta makes due (fresh isolated smoke profile only; never `cloud add` with a copied production config).
- #446's static gate must live in scripts executed from the trusted default branch — workflow files are frozen to uses:-only deltas.
