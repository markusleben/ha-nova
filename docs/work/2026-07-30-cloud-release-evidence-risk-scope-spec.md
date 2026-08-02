# Cloud Release Evidence Risk Scope

Status: active

## Problem

The Cloud release contract currently requires every positive path, rare
account state, operating system, and lifecycle failure to be repeated as one
real-device matrix for every evidence commit. Some states need external
accounts or subscriptions that a maintainer cannot safely manufacture. The
Cartesian matrix spends time without adding commit-specific confidence.

## Decision

Keep the existing evidence schema and fail-closed verifier. Split evidence
into two layers:

1. Exact-target checks always run: CI, security and recovery contracts,
   candidate provenance on every enabled platform, and the exact installed
   Relay App. For deltas that match an invalidation-map row with
   real-platform scope, one real reference-platform Cloud health smoke also
   runs, using the downloaded candidate binary with Census suppressed.
2. Risk-scoped qualification runs on first support and after a relevant
   implementation or evidence-harness change. A passing qualification remains
   applicable across unrelated changes.

The check owner must inspect the complete qualification-to-target diff before
attesting carried evidence. The activation or release pull request records a
non-secret qualification ledger for every carried check and keyring OS:
qualified commit and tree, evidence reference, inspected target, and the
change-class decision. The privileged evidence secret remains the attestation;
the pull-request ledger makes its inputs reviewable without expanding the
existing JSON schema. The verifier validates the privileged attestation and
exact target; it does not infer the maintainer's invalidation decision. Missing
or uncertain ledger data means rerun.

Carry-forward applies only to the qualification behind a check boolean. The
JSON envelope, commit/tree identity, candidate provenance, and installed App
remain exact-target; the reference health smoke is exact-target for deltas
with real-platform scope. Never reuse an older JSON envelope.
The verifier binds that new envelope to the exact target. Reviewers verify the
qualification ledger before the boolean is set to `true`.

## Invalidation map

Use the narrowest matching row. A change that matches multiple rows invalidates
their union. A qualification trigger includes changes to every deterministic
substitute and real evidence collection/validation harness it relies on.

| Changed surface | Qualification to repeat | Real platform scope |
|---|---|---|
| Cloud or Relay transport, Ingress session, endpoint, or route selection | `parity`, `stress_10000`, `routing`, relevant lifecycle and non-disclosure paths | One reference platform |
| Shared native-secret orchestration | `keyrings` | One reference platform, plus any OS with different behavior |
| macOS, Windows, or Linux native-store adapter | `keyrings`; relevant lifecycle path | Affected OS only |
| `internal-cloud-stress` or its evidence collection/validation harness | `stress_10000` | One reference platform |
| OAuth, Home Assistant user binding, or App discovery | `roles`, `domains_mfa`; relevant authorization lifecycle | One reference platform |
| Relay App startup, identity, device registry, or installation | `installed_relay_app`; relevant lifecycle and redirect paths | One reference platform |
| CLI setup, install, update, uninstall, signing identity, or authorization retention | `signing_and_update_matrix`; relevant lifecycle path | Affected OS only; exact provenance still runs on all enabled OSes |
| Config, argv, logs, diagnostics, or AI-visible output | `redirects_non_disclosure` | Affected surface; one retained real artifact scan |
| A deterministic substitute or real evidence collection/validation harness | Its owning qualification | Same scope as that qualification |
| Release workflow or provenance machinery only | `signing_and_update_matrix` | Exact provenance on all enabled OSes |
| Unrelated docs, tests, process, or product code | None | Exact-target layer only |

## Check contract

- `parity`: real `/health`, `/ws`, `/core`, `/files`, and `/backups` parity on
  one reference platform for every Cloud or Relay transport change.
- `stress_10000`: one real bounded run per Cloud or Relay transport change or
  stress-harness change, not once per operating system.
- `keyrings`: real happy-path and fail-closed no-UI behavior on every enabled
  OS for first support. A shared orchestration change repeats one reference OS;
  an adapter change repeats only its affected OS. Deterministic platform tests
  cover cancellation and timeout branches.
- `roles`: one real standard non-administrator binding on a reference
  platform. Exact-target tests verify that the authenticated Home Assistant
  user and Relay instance remain bound through setup and functional dispatch;
  Owner and administrator add no separate transport path.
- `domains_mfa`: one real canonical Nabu Casa OAuth flow. Home Assistant owns
  the MFA challenge before returning the same OAuth callback, so HA NOVA does
  not manufacture account-specific MFA proof. Deterministic tests cover
  custom-origin canonicalization, inactive subscription, disabled remote
  access, and authorization abort.
- `lifecycle`: one isolated Cloud-authorized profile first covers Relay App
  restart and reinstall recovery, then HA NOVA CLI standard uninstall/reinstall
  with retained authorization. Full purge is last; it revokes and verifies the
  active remote authorization and device before local cleanup. Deterministic
  crash/concurrency tests cover every durable boundary; update and
  instance-mismatch paths get a real run when those paths change.
- `redirects_non_disclosure`: exact-target redirect, argv, config, log,
  diagnostic, and AI-output tests plus one retained real artifact scan after
  a relevant transport, config, argv, log, diagnostics, or AI-output change.
- `installed_relay_app`: always exact-target. Supervisor builds the reviewed
  source, the App reports the expected version, and the reviewed Cloud routes
  exist.
- `routing`: one real automatic local-to-Cloud fallback after a routing
  change; exact-target tests cover every fail-closed non-fallback outcome.
- `signing_and_update_matrix`: always verify exact candidate signatures and
  provenance on all enabled platforms. Repeat real install/update behavior
  only after installer, updater, signing identity, or native authorization
  changes. Existing release rules still decide when an RC rehearsal is
  mandatory.

No mock may replace the required real positive path. No carried
qualification may cross a relevant implementation change.

The exact-target Cloud health smoke is not `parity`. It repeats only for a
target whose delta matches an invalidation-map row with real-platform scope;
a maintenance delta (the `None` and release-machinery rows) refreshes the
envelope and provenance without a new smoke. When due, the official
downloaded candidate binary must run with `HA_NOVA_NO_CENSUS=1`
against the exact installed Relay App (from an isolated smoke profile — see
`docs/releasing.md` for the collision-safe setup):

```bash
HA_NOVA_NO_CENSUS=1 <candidate-binary> relay health \
  --server <profile> --via cloud
```

The result must identify the expected App version and a healthy Home Assistant
WebSocket. Do not retain the private Cloud URL.

## Acceptance

- The JSON schema and verifier remain unchanged.
- `docs/releasing.md`, `docs/reference/testing.md`, and the Cloud remote spec
  describe the same risk-scoped contract.
- Repository documentation checks pass.
- No product, workflow, version metadata, README, tag, or release changes.
