---
name: integration-setup
description: Use when adding a Home Assistant integration or continuing an integration reauthentication flow through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Integration Setup

## Scope

Add integrations that expose a Home Assistant config flow and continue pending integration reauthentication (`reauth`) flows.

Not handled here:

- integration options, reconfigure, subentry, enable/disable, reload, or delete operations
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

Omit `--body-file` on GET and DELETE. Relay-core response data is under `.data.body`.

## Flow

### Resolve the operation

1. For add, list flow handlers and integration manifests. Join manifest `domain` to the handler list, then resolve one exact domain or clear manifest-name match. Ask one blocking question when several match; never guess.
2. For reauthentication:
   - read `config_entries/get` and resolve the exact existing `entry_id`
   - read `config_entries/flow/progress`
   - select an existing flow only when `context.source == "reauth"` and its `handler` plus `context.entry_id` match the resolved entry
   - if no matching pending flow exists, report that Home Assistant is not currently requesting reauthentication; never synthesize a reauth flow
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

## Error Handling

- Relay/upstream failures follow `skills/ha-nova/relay-api.md` → Error Handling.
- `404`: flow expired or handler unavailable — re-read handlers/progress before retrying.
- `405`: wrong method — use POST for the collection start, GET/POST/DELETE for a specific flow.
- `abort` or form errors: surface Home Assistant's reason; do not retry with guessed fields.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

Use the Preview Card for flow start, menu selection, and each non-secret form submit. The menu choice block is its bound confirmation. Results name the integration, operation, config-entry state, and the exact verification scope. UI handoffs state what remains and never display secret fields or values.

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
