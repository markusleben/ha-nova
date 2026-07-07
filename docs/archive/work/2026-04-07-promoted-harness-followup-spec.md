## Promoted Harness Follow-up Fixes

- Problem 1: `history_fixture()` capped candidates before neutral filtering, which could miss valid neutral entities on larger registries.
- Fix 1: apply the neutral filter first, then cap the neutral candidate list.
- Problem 2: the promoted suite `relay_ws()` accepted `{"ok": false}` as usable residue data and could hide relay/API failures.
- Fix 2: make the suite relay helper fail loudly on `ok=false`, matching the single-scenario harness.
- Verification: promoted harness contract test plus focused Python compile check.
