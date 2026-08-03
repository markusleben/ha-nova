# Backlog Sequencing 2026-08

Status: active
Scope: successor to [masterplan-2026-h2.md](../archive/work/masterplan-2026-h2.md) as the SSOT for sequencing and decisions. Orders the 12 open issues (as of 2026-08-03) into three phases following the proven "harden what exists before widening surface" logic.

## Maintainer decisions (2026-08-03)

- Phase 3 order: HACS lifecycle (#478) before multi-server wizard layer 2 (#411).
- Release cut: Phases 1+2 bundle into ONE release after Phase 2; each Phase-3 feature ships its own release. All of these change delivery machinery at some point, so the RC rehearsal applies per `docs/releasing.md`.
- #463 (Odysseus) is externally blocked (no directory-based skill loading upstream; two reporter findings pending) — parked, not planned.
- #478 revises the masterplan's "Deliberately NOT: HACS management parity" entry — maintainer-confirmed scope in the issue thread, with the solaredgeoptimizers migration as the reference case.

## Phases

| Phase | Theme | Issues (order) | Release |
|-------|-------|----------------|---------|
| 1 | Safety & correctness | #493+#489 (one PR: write-path fail-closed) → #482 ∥ #446 | — (collect in 0.23.0 draft) |
| 2 | Quality & visibility of existing features | #452 → #483 → #484 → #440 → #444 (release prep) | v0.23.0 bundled, with RC |
| 3 | New capabilities | #478 (spec → impl) → #411; #463 parked | one release per feature, with RC |

## Process notes

- Every product PR needs a fresh Cloud evidence envelope while `cloud_remote_enabled` is true. Skills/docs/tests deltas match the invalidation map's "None" row (envelope refresh, no new real-platform smoke); see `docs/releasing.md` and [2026-07-30-cloud-release-evidence-risk-scope-spec.md](2026-07-30-cloud-release-evidence-risk-scope-spec.md).
- #482 touches the device registry surface → expect a reference-platform Cloud health smoke (fresh isolated smoke profile only; never `cloud add` with a copied production config).
- #446's static gate must live in scripts executed from the trusted default branch — workflow files are frozen to uses:-only deltas.
