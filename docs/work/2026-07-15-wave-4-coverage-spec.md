# Wave 4 Coverage Spec

Status: complete
Date: 2026-07-15
Sequencing SSOT: `docs/work/masterplan-2026-h2.md` → Wave 4

## Goal

Close the remaining high-value skill coverage gaps without adding Relay business logic.

## Delivery lanes

1. `integration-setup`
   - Add UI-configurable integrations through `/api/config/config_entries/flow`.
   - Continue existing `reauth` flows discovered through `config_entries/flow/progress`.
   - Reuse the response-driven menu/form loop proven by `helper` Family 2.
   - Never collect passwords, PINs, OAuth grants, access/API keys, tokens, or private key material in chat. Relay-started add flows that reach credential, external/OAuth, or progress steps restart in the Home Assistant UI; user-started flows are omitted from the pending-flow feed, and the Relay cannot provide the frontend-origin header.
   - Verify add/reauth at the config-entry layer.
2. Calendar writes
   - Create through `calendar.create_event`.
   - Update/delete through `calendar/event/update|delete` WebSocket commands; Home Assistant 2026.7 does not expose update/delete as calendar services.
   - Gate every operation with `supported_features`, preview/confirm, exact event identity, recurrence-scope handling, and read-back verification.
3. Runtime event/security coverage
   - Move custom event firing and webhook triggering from experimental fallback to `service-call`; both are runtime actions that can execute automations.
   - Keep listener-impact scanning, payload preview, bounded verification, and webhook-ID secrecy.
   - Add explicit alarm modes, lock/open capability gates, and secret-code handling. Codes never enter chat; code-required actions finish in the Home Assistant UI.

## PR boundaries

- PR A: new `integration-setup` skill, dispatch/inventory/docs/contracts, fallback ownership flip.
- PR B: calendar create/update/delete, architecture/contracts.
- PR C: event/webhook ownership plus alarm/lock guidance, architecture/contracts.

Each PR completes the repository PR merge checklist independently. No release/version bump or README feature claim lands in Wave 4; release-bound truth stays deferred to release prep.

## Research basis

- Home Assistant config flows are response-driven data-entry flows with form, menu, external, progress, create-entry, and abort results.
- The current frontend starts/fetches/submits/deletes config flows through `/api/config/config_entries/flow`, lists handlers through `/flow_handlers`, and discovers pending flows through WS `config_entries/flow/progress`.
- Current Home Assistant Core registers calendar create as a service and calendar update/delete as feature-gated WebSocket commands.
- Webhook IDs are authentication secrets. One registered handler can dispatch multiple automation triggers sharing the same ID; automation webhooks default to local-only.
- Home Assistant deliberately returns HTTP 200 for unknown webhook IDs, blocked non-local calls, and handler failures, so only fresh listener evidence can verify an effect.

## Acceptance

- Dedicated contract tests pin routing, transport, capability gates, confirmation binding, secret handling, and verification.
- Dynamic installers discover the new skill without installer-specific logic.
- `npm run verify` and documentation checks pass.
- Every PR receives a real clean Codex result on its final SHA, all threads are resolved, and the merge uses squash/delete-branch only after the mandatory checklist is complete.
