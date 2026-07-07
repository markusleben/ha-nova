# 2026-03-27 Review Synthesis Spec

Status: merged

Scope:
- `#128` stays `warn only`
- standalone single-target review gains explorative questions plus suggestion synthesis
- remove/simplify guidance must pass a design-intent gate
- confident suggestions rank by intervention depth
- post-write review stays compact
- bulk review stays unchanged

Defaults:
- uncertainty goes to `Questions to consider`
- only confident recommendations go to `Suggestions`
- no trace auto-run; manual trace inspection only after the next real run when persisted `R-18` remains
- review live validation uses a dedicated small harness instead of expanding the generic scenario harness
