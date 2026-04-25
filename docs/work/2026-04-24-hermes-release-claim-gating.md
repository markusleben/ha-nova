# Hermes Release Claim Gating

Status: active

## Goal

Ship Hermes Agent support in the next HA NOVA release without letting the public `README.md` claim stable Hermes support before the release commit, tag, artifacts, and live proof are aligned.

## Scope

- Keep the Hermes implementation, registry entry, installer behavior, tests, `.hermes/INSTALL.md`, and platform test-status doc in the Hermes support PR.
- Keep public `README.md` release-conservative until the final release PR.
- Use a final release PR to flip the public README support claim after the release-bound proof matrix is complete.

## Rules

- `README.md` is stable public product truth.
- `.hermes/INSTALL.md` is the Hermes overlay for the unreleased next release while this work is still in-flight.
- `docs/reference/hermes-platform-validation.md` records platform test status and must state the strongest current proof without overstating untested routes.
- The final release PR must either:
  - restore the public Hermes claim in `README.md`, if release proof is complete, or
  - keep Hermes absent from the public support list, if proof is incomplete.

## Release Blockers Before Public README Claim

- Hermes filesystem install uses namespaced directories and matching frontmatter.
- `ha-nova doctor` reports the expected Hermes state.
- `skills_list(category="ha-nova")` and `skill_view("<listed-name>")` agree on a real Hermes install.
- Linux secure-storage recovery proof covers the current release-bound matrix.
- Windows messaging stays explicit: native Windows Hermes is unsupported; WSL2 is the intended Windows route.
