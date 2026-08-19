---
name: integration-setup
description: Use when adding a Home Assistant integration, continuing an integration reauthentication flow, or recovering invalid integration credentials when no reauth flow is pending — through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Integration Setup

## Scope

Add integrations that expose a Home Assistant config flow, continue pending integration reauthentication (`reauth`) flows, and recover invalid credentials when no reauth flow is pending (Credential Recovery below).

Not handled here:

- integration options, reconfigure, subentry, enable/disable, or delete operations; reload only inside Credential Recovery below — every other reload stays with `ha-nova:fallback`
- helper config-entry flows (use `ha-nova:helper`)
- YAML-only integrations (use `ha-nova:yaml-config`)
- diagnosing setup failures after a flow finishes (use `ha-nova:diagnose`)

Credential-bearing, external/OAuth, and progress steps finish in the Home Assistant UI; this skill never collects secrets in chat.

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`.
Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

Use response-driven config flows through the relay:

- `ha-nova relay core --method GET --path /api/config/config_entries/flow_handlers --out <handlers-file>`
- `ha-nova relay ws --data-file <payload-file> --out <manifests-file>` with `{"type":"manifest/list"}`
- `ha-nova relay ws --data-file <payload-file> --out <flows-file>` with `{"type":"config_entries/flow/progress"}`
- `ha-nova relay ws --data-file <payload-file> --out <entries-file>` with `{"type":"config_entries/get"}`
- `ha-nova relay core --method GET|POST|DELETE --path /api/config/config_entries/flow[/<flow_id>] --body-file <payload-file> --out <flow-file>`
- `ha-nova relay core --method POST --path /api/config/config_entries/entry/<entry_id>/reload` (Credential Recovery only)

Omit `--body-file` on GET and DELETE. Relay-core response data is under `.data.body`.

## Flow

### Resolve the operation

1. For add, list flow handlers and integration manifests. Join manifest `domain` to the handler list, then resolve one exact domain or clear manifest-name match. Ask one blocking question when several match; never guess.
2. For reauthentication:
   - read `config_entries/get` and resolve the exact existing `entry_id`
   - read `config_entries/flow/progress`
   - select an existing flow only when `context.source == "reauth"` and its `handler` plus `context.entry_id` match the resolved entry
   - if no matching pending flow exists: with credentials reported invalid, continue with Credential Recovery below; otherwise report that Home Assistant is not currently requesting reauthentication; never synthesize a reauth flow
3. Limit one integration flow per operation.

### Start or continue

For add:

1. Capture `config_entries/get` as the verification baseline.
2. Preview the exact integration domain and flow start. State that no integration has been added yet.
3. Ask for natural confirmation bound to this preview.
4. POST `{"handler":"<domain>"}` to `/api/config/config_entries/flow`; extract `flow_id` or fail loud.

For reauthentication, GET the matched `/api/config/config_entries/flow/<flow_id>`; do not create a second flow.

### Iterate live steps

Treat every response as authoritative:

- `menu`: show only returned options and state that choosing one submits that exact menu step. The user's selection is the bound confirmation for `{"next_step_id":"<choice>"}`. A bare-number reply is resolved first: name the selected option before submitting it (context skill → Interactive Choices).
- `form`: first inspect the returned `data_schema`. If it requests a secret, use the UI-only rule below without building or previewing a body. Otherwise, use only fields returned by the live `data_schema`, build the full step body, preview it, then ask for confirmation bound to that body before POSTing it.
- validation errors: show the returned field errors and stop; never guess a replacement.
- `external` or `progress`: for an add flow started here, DELETE the unfinished flow and ask the user to restart the integration at **Settings > Devices & services**. User-started flows are omitted from `config_entries/flow/progress`, and the Relay cannot supply the frontend-origin header OAuth redirects depend on. For a pre-existing reauth flow, preserve it and direct the user to its matching in-progress card. Never claim completion.
- any form requesting a password, PIN/code, OAuth grant, access/API key, token, certificate, or private key material: never build or submit its body. For an add flow started here, DELETE the unfinished flow and ask the user to restart the integration at **Settings > Devices & services**. For a pre-existing reauth flow, preserve it and direct the user to its matching in-progress card. Never ask for or echo the secret.
- `create_entry`: proceed to verification. Follow `next_flow` only when it is another config flow; options/subentry flows stay out of scope.
- `abort`: report the returned reason. For reauth, only `reason == "reauth_successful"` is a success result, and only after config-entry verification.

If the user cancels an add flow created by this skill, DELETE that unfinished `flow_id`. Never delete a pre-existing reauth flow.

### Verify

1. Re-read `config_entries/get`.
2. Add passes when the terminal response's `result.entry_id` exists. If the result omits it, diff the baseline by `entry_id`; exactly one new entry with the requested domain must exist or verification is ambiguous.
3. Reauth passes only when the same `entry_id` still exists, the matching reauth flow is gone, and the terminal result reports success. Report the current config-entry state exactly; do not call a non-`loaded` entry healthy.
4. Linked devices/entities are secondary evidence only.

### Credential Recovery (no reauth pending)

Credentials reported invalid, but `config_entries/flow/progress` shows no
matching reauth flow:

1. `loaded` is lifecycle state, never proof the stored credential works — do
   not call an entry healthy because it is `loaded`.
2. Preview, confirm, then reload the entry —
   `POST /api/config/config_entries/entry/<entry_id>/reload` — the documented
   surface that makes the integration re-validate its stored credential. The
   preview states that reload re-runs setup and briefly drops the entry's
   entities. Preserve the entry and every subentry; never delete or recreate
   anything.
3. Inspect the reload response and re-read `config_entries/get` first: a
   `false` result or a non-`loaded` entry means the reload itself did not
   complete — report that actual entry state, never a missing upstream
   trigger. Then re-read `config_entries/flow/progress` either way (an auth
   failure can still open reauth). Reauth flows open asynchronously (often
   only after the first refresh), so wait a few seconds and re-read once
   more before concluding nothing appeared. A new flow with
   `context.source == "reauth"` and the same `entry_id` → continue with the
   normal reauthentication handoff above.
4. Still no flow on a settled, SUCCESSFUL re-read — AND step 3's reload
   check passed (result true, entry `loaded`): Home Assistant exposes no
   supported trigger for this integration — say so plainly and hand off to
   **Settings > Devices & services**. A FAILED re-read is not that evidence:
   report step 3's actual reload result (done only if its check passed) and
   the flow state as unknown, never as unsupported. Never synthesize a config flow, edit `.storage`, create a
   replacement entry, or reach for deprecated integration services as a
   workaround.
5. Verified success is only a terminal `reauth_successful` for the same
   `entry_id` plus the config-entry verification above. A flow finished in
   the UI leaves no terminal result here: the matching flow gone — and none
   re-appearing on a settled re-read — is reported as completed but
   unverified, never as proven recovery (a canceled flow disappears the same
   way). Do not spend a paid API request to "test" the credential unless the
   user asks.
6. Secrets and key fragments never appear in previews, output, or logs.

## Error Handling

- Relay/upstream failures follow `skills/ha-nova/relay-api.md` → Error Handling.
- `404`: flow expired or handler unavailable — re-read handlers/progress before retrying.
- `405`: wrong method — use POST for the collection start, GET/POST/DELETE for a specific flow.
- `abort` or form errors: surface Home Assistant's reason; do not retry with guessed fields.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

Use the Preview Card for flow start, menu selection, each non-secret form submit, and the credential-recovery reload. The menu choice block is its bound confirmation. Results name the integration, operation, config-entry state, and the exact verification scope. UI handoffs state what remains and never display secret fields or values.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed confirmation code `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- Drafts follow `skills/ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design, nothing for hypothetical future needs.

- Declared exception to the core delete rule above: canceling an unfinished add flow created by this skill, including cleanup before a required credential, external/OAuth, or progress UI restart, deletes only ephemeral flow state; an explicit `cancel` or the UI-restart branch is sufficient, and this exception never applies to a config entry.
- Never request, echo, persist, or submit credentials from chat; finish credential-bearing steps in the Home Assistant UI.
- Starting or submitting a flow may contact an external service; always use the bound preview.
- Never leave an agent-created canceled add flow dangling; never delete a Home Assistant-created reauth flow.

## Guardrails

- One integration flow at a time.
- Live `data_schema` and menu options are the only accepted field source.
- Config-entry identity/existence is primary verification evidence.

## References

- Relay API: `skills/ha-nova/relay-api.md`
- Shared write safety: `skills/ha-nova/write-safety.md`
