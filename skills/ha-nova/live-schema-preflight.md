# Live-Schema Preflight (supported config/options flows)

Shared orchestration contract for Home Assistant config-entry and options
flows. It applies ONLY to explicitly supported flow families: the flow-backed
helper domains owned by `ha-nova:helper` (see
`skills/ha-nova/helper-flow-schemas.md`) and exactly those
`ha-nova:integration-setup` paths that skill DECLARES supported with
documented non-persisting navigation — the owning skill's declaration is the
allowlist, never this file; a flow family the owning skill excludes (an
options `next_flow`, for example) stays out of scope here too. This contract never authorizes probing unfamiliar endpoints or
arbitrary config flows — the fail-closed rule from #493
(`skills/ha-nova/relay-api.md` → Write-Probing Asymmetry) remains the
boundary, and everything outside the supported families stays with
`ha-nova:fallback`.

No new Relay endpoint exists for this: the Relay stays dumb, and all
orchestration below is skill-side behavior over the existing
`/api/config/config_entries/flow` and `.../options/flow` paths.

## Preflight rules

1. Start only a known, allowlisted flow whose inspection and navigation
   behavior is documented for its family.
2. Read the LIVE response of every step: `data_schema`, suggested/default
   values (`description.suggested_value`), `step_id`, and `last_step`.
3. Mutation previews are assembled from the live form, never from previously
   observed schemas. Observed field inventories plan the flow; the live
   `data_schema` decides what is shown and submitted. The preview labels which
   fields came from the live running flow and which remain unavailable (no
   live value exposed) — unavailable fields are named, never guessed.
4. Pre-confirmation navigation may advance ONLY through steps proven
   non-persisting and side-effect-free for that family (menu steps, non-final
   form steps of a documented multi-step flow). If a step's persistence
   behavior is not documented for the family, stop before submitting it.
5. STOP before the terminal submit. One-step flows (`last_step: true` on the
   first form) therefore stop before their only submit; multi-step flows stop
   after the last non-persisting step. The terminal boundary is exact in both
   shapes: nothing terminal is sent before the bound confirmation.
6. Confirmation binds to the live schema plus the exact terminal payload. Any
   change to either expires it.
7. On cancel or expiry, abandon the transient flow without submitting the
   terminal step (DELETE the unfinished flow when the owning skill's cleanup
   rule applies).

## Explicit stops (never guess-or-retry)

- Validation errors: show the returned field errors and stop.
- Schema drift between preview and submit (fields, defaults, enum options,
  step order, or terminal boundary changed): stop and re-preview against the
  new live schema; the old confirmation is void.
- Unknown outcomes (timeout, transport error, ambiguous terminal response):
  reconcile the FLOW first — `GET .../flow/<flow_id>` shows the current
  `step_id`, so it proves whether the uncertain submit landed (the progress
  LIST omits agent-started flows; the by-id GET does not). A 404 there is
  ambiguous after a TERMINAL submit: a completed flow disappears exactly like
  an expired one — reconcile by FLOW TYPE first. A create flow checks
  `config_entries/get` for the new entry; an options/update/reauth flow
  creates NO entry, so compare the ENTRY itself (options/config changed, or
  the reauth success shape: flow gone with the same `entry_id` alive). Only
  when that evidence shows nothing landed treat the 404 as expiry and restart
  with a fresh preview; anything ambiguous stops and reports the state.
  Never retry blind — a blind retry can double-create or double-apply.

## Response envelope and terminal-result normalization

Relay `/core` responses carry the upstream payload under `.data.body`
(`.data.status` holds the HTTP status); `/ws` responses carry it under
`.data` directly. A flow step response therefore lives at `.data.body`.

Terminal `create_entry` identity extraction checks the documented nested
shape: the created config-entry identity is at `.data.body.result.entry_id`
(the flow's `result` object), NOT at `.data.body.entry_id` — reading the flat
path returns null even though the entry was created. When the terminal result
exposes no identity there, the constrained before/after `config_entries/get`
diff is the
fallback: it passes only when exactly one new `entry_id` appeared and its
metadata is consistent with the request; empty, plural, or inconsistent diffs
fail loud as ambiguous verification.
