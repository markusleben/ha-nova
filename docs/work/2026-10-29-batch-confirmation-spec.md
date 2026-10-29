# Batch confirmation: one confirmation for up to 20 automation and script changes

Status: active

Date: 2026-10-29. Owner: CLI plans and apply (`cli/plan_batch.go`, `cli/verb_apply_batch.go`, `cli/confirm_batch_text.go`); skills `write` and `ha-nova` (`batch-safety.md`, `grouped-change-set.md`). Target: 0.28.0.

## Today

- Every typed plan gets its own human confirmation (window, linked phone, form, or chat code). Typed: an automation/script write that reaches a typed action, and every automation/script delete (`cli/plan_config.go`). The CLI seals each confirmation to one plan (`cli/plan_store.go`, `cli/confirm_code.go`, `cli/verb_apply.go`), so `skills/ha-nova/batch-safety.md` lists one CLI code per item (at most 10).
- Preview plans share one natural "yes" in a grouped change set (`skills/ha-nova/grouped-change-set.md`, at most 10), then one `apply` each.
- Chat apps also ask per tool call unless the user chose "Always allow". Deleting 12 automations is 12 confirmations and 12 `ha_apply` prompts.

## Decisions (Markus, 2026-10-29; binding)

1. "Both changes AND deletes of automations and scripts can be confirmed together: ONE human confirmation per list, at most 20 items, and the confirmation window shows every item's name (with what happens to it)."
2. "It ships in 0.28.0."

Accepted proposal:

- "The phone shows the count and the most sensitive item."
- "Before sending each item, HA NOVA re-checks it (drift) and takes its snapshot."
- "It stops at the first failure and reports what was done, what failed, and what was not attempted (a ledger; never claimed atomic)."
- "Security: one tap confirms many changes, so each one is named in the window."

## Design

### 1. Commands

```
ha-nova plan write|delete …                    # each item as today: its own preview (diff, consumers)
ha-nova plan batch --plan <id> --plan <id> …   # 2 to 20 of those plans, in the order they run
ha-nova apply --plan <batch id>                # one confirmation, then each item in order
```

`plan batch` only reads Home Assistant (I1): it re-plans every member, stores the batch plan, and removes the member plan files. Exit 3 (needs confirmation), 4 (refused), 2 (a member changed since its preview). `apply` is the same verb as for every plan.

### 2. What a batch holds

Members, an allowlist; anything else is refused (exit 4 `BATCH_UNSUPPORTED`, nothing planned):

- `plan write --domain automation|script` (an update or `--create`; `plan template` makes one too);
- `plan delete automation.x|script.x`.

Refused, each named with its reason: call plans (they act on devices; each keeps its own confirmation), raw plans (already one plan of 1 to 20 items), rooms, revert and restore (they act on the undo stack and the snapshot store, one at a time), dashboard writes and deletes (another snapshot and verify path), and a batch inside a batch.

Rules; each refusal names the member, and the member plans stay as they were:

- 2 to 20 members (`BATCH_SIZE`).
- Every member is a current-schema plan of this server, unexpired and unclaimed (`PLAN_NOT_FOUND`, `SERVER_MISMATCH`, `PLAN_EXPIRED`); a typed member's seal holds (`PLAN_INVALID`).
- Each item at most once: one plan id once, one automation or script once (`BATCH_DUPLICATE`). A second change to the same item would drift as soon as the first is sent.
- Re-planned now from its stored request, as apply does, each member is still the plan whose preview was shown (`stillConfirmed`, same tier). Otherwise exit 2 `PLAN_DRIFT` naming it: plan it again and show its new preview.
- Order: a member whose drift basis reads another member's item (`basis.configs` key `<domain>:<config_id>`, or `basis.entities` key `<entity_id>`) must run before that member (`BATCH_ORDER`: "move “X” before “Y”"; two that read each other go in separate batches). Otherwise it drifts as soon as the earlier one is sent.
- Mixed lists are allowed: creates, changes, and deletes, automations and scripts, in one batch. Decision 1 names both; the window names each item's kind and what happens to it; the order check and the re-check before each send keep dependencies safe. The skills' "one family per manifest" rule existed because each item had its own code.

### 3. The batch plan

- Kind `batch`; `batch_members` holds per member its id, hash, tier, and stored config request (copied from the member plan).
- Hash: `planHash("batch", server, [{id, hash}, …] in order, tier, …)`. It covers every member's hash and the order. The batch id and code derive from it as for every plan (nonce, `planIdentity`, `confirmCode`); a typed batch's channel seal covers its id, channel, kind, language, and text. An edited member, hash, or order in the plan file is `PLAN_INVALID` at apply; a member changed in Home Assistant is drift.
- Tier: the highest member tier. All preview: `plan.confirm: natural`, the user's own reply to the list, then one `apply`. Any typed: one typed confirmation on the channel `detectChannel` picks for the batch (window, with the linked phone in parallel; phone; form; chat code).
- Claim: `plan batch` claims each member with the atomic rename apply uses (`<id>.json` → `<id>.batched-<pid>`). A member already claimed (being applied, or in another batch) refuses the batch. Once the batch plan is saved, the member files are deleted: no member can be applied alone or join another batch. A failure before the save renames them back. The batch itself is claimed at apply like every plan.
- Expiry: members must be unexpired when the batch is made. The batch then has its own 15 minutes from `plan batch`; it re-plans every member then and again before each send.
- Language: read once for the batch (sealed when typed); members re-plan in it.
- `planMaxFiles` goes from 20 to 50. With 20, planning the 20th member next to any other open plan prunes the first member.

### 4. Apply

`ha-nova apply --plan <batch id> [--confirm <code>] [--no-snapshot]`:

1. Claim, expiry, server, schema, seal, and channel rules: unchanged (`applyPlan`).
2. Re-plan every member (I3). If any changed: exit 2 `BATCH_DRIFT`, nothing sent, the batch is used up. `next` names the changed items: plan them again, show their new previews, then make a new batch of all of them (the old member plans are gone). No automatic new batch: the changed item's new preview must be seen first.
3. Identity and the one code of the batch (I2), or the window, phone, or form answer, as for every typed plan.
4. Snapshots: the config snapshot of every delete member, all before the first send, unless `--no-snapshot`. If one fails: nothing is sent, exit 1 `SNAPSHOT_FAILED`, and the batch stays valid with `snapshot_failed`, as a single delete does. Only after the user chose it, `--no-snapshot` applies the whole batch without copies. A failed copy never leaves a half-done list, and the re-check before each delete still proves the live config equals the copy (same digest).
5. Each member in order: re-plan it right before its send (the first uses step 2's). Changed: stop (`drift`). Then exactly one write through `executeConfig` (POST or DELETE, reload, read-back, registry and state check, the update's undo checkpoint), never retried. Stop at the first member that is not `verified`.
6. Nothing is rolled back.

`next_step` gives the agent up to 10 minutes for the command (a single plan: 2), since each member costs a re-plan, a write, a reload, and a read-back.

### 5. Exit codes and the ledger

| Exit | `status` | `error.code` | When |
|---|---|---|---|
| 0 | `verified` | none | every member verified |
| 1 | none | `SNAPSHOT_FAILED`, `RELAY_ERROR` | nothing sent; the batch stays valid (snapshot) or is handed back for a retry (Relay unreachable before the first send) |
| 2 | `drift` | `BATCH_DRIFT` | a member changed before anything was sent |
| 4 | as today | as today | claim, expiry, code, form, window, and phone answers, `NO_SNAPSHOT_NOT_OFFERED` |
| 6 | `failed` or `unverified` | `BATCH_STOPPED` | stopped after a member was attempted: refused by Home Assistant, unverified, changed, or a Relay error |
| 7 | `unverified` | `OUTCOME_UNKNOWN` | a member may have been saved or deleted: read it, never apply again |

`result.items[]` is the ledger, one row per member in order: `item`, `plan` (the member's id), `op` (`create`, `update`, `delete`), `domain`, `entity_id`, `config_id`, `name`, `status` (`verified`, `unverified`, `failed`, `unknown`, `drift`, `not_sent`, `not_attempted`), `verification`, `snapshot` and `restore` (deletes), `undo` (updates), `error`. `result.counts`: verified, stopped, not attempted. `say` is one catalog line (§6). `next_step`: tell which went through, what stopped the next one, and what was not sent; the rest is a new batch with a new confirmation; deleted ones come back with each row's `restore`, changed ones with `plan revert` (the newest 5 per server).

### 6. Texts on every surface

Every batch text is a new key in `cli/confirm_strings.en.json`, rendered by `confirmT` in the plan's sealed `Lang`. German goes into `confirm_strings.de.json` when main has it by then, flagged for review. Names go through `sayText` (quoted, defanged, at most 60 characters). The member parts come from the existing builders: each member's typed reason clauses (`sayReason`), never the delete's own "removes it" clause. Nothing comes from the agent.

| Key | English |
|---|---|
| `batch.head` | I'm about to make these {count} changes in Home Assistant, in this order: |
| `batch.goal.create.automation` | Create the automation {name} |
| `batch.goal.create.script`, `….change.automation`, `….change.script`, `….delete.automation`, `….delete.script` | likewise: Create / Change / Delete the automation / script {name} |
| `batch.item` | {n}. {goal}. |
| `batch.needs_you` | One confirmation covers all {count}, so I need you to confirm it. |
| `batch.copies` | A copy of each one deleted is saved first, so it can be restored. |
| `batch.no_copies` | No copies could be saved, so once deleted they can't be restored. |
| `batch.order` | HA NOVA checks each one again right before it is sent and stops at the first one that doesn't go through. |
| `batch.status` | Nothing has changed yet. |
| `batch.ask` | Save all {count}? I can also show the details. |
| `batch.phone.delete.automation` … (9 keys: `delete`, `change`, `either` × `automation`, `script`, `mixed`) | Delete {count} automations? / Delete {count} scripts? / Delete {count} automations and scripts? / Change {count} automations? / … / Change or delete {count} automations and scripts? |
| `batch.phone.most` | Most sensitive: {goal}. |
| `batch.phone.done` | Home Assistant confirms each one. |
| `batch.phone.partly` | {done} of {count} went through. |
| `batch.result.done` | Done: all {count} went through. I checked each one. |
| `batch.result.failed` | Partly done: {done} of {count} went through; {name} didn't. |
| `batch.result.unverified` | Partly done: {done} of {count} went through; {name} was saved, but I couldn't check it. |
| `batch.result.changed` | Partly done: {done} of {count} went through; {name} changed since you saw it, so I stopped. |
| `batch.result.unknown` | Not sure: {name} may have gone through, and I can't tell. I didn't try again. |
| `batch.result.none` | Not done: {name} didn't go through. Nothing changed. |
| `batch.result.rest` | The rest weren't sent. |
| `batch.drift` | Not done: {names} changed since you saw the list, so nothing was sent. |

Keys with `{count}` are plural entries. A create counts as a change in the phone title; the list names it "Create".

**The batch text** (window, form, chat; one builder): `batch.head`; one line per member, `batch.item` with its goal, followed on the same line by that member's typed reason sentences; then `batch.needs_you` (typed batch), `batch.copies` (deletes; `batch.no_copies` after `--no-snapshot`), `batch.order`, `batch.status`. Example:

```
I'm about to make these 3 changes in Home Assistant, in this order:
1. Change the automation “Night mode”. This also unlocks “Front Door” through this automation.
2. Delete the automation “Kitchen motion (old)”.
3. Delete the script “Old arrival”.
One confirmation covers all 3, so I need you to confirm it. A copy of each one deleted is saved first, so it can be restored. HA NOVA checks each one again right before it is sent and stops at the first one that doesn't go through. Nothing has changed yet.
```

- **Window:** the batch text, then the existing lines (the phone asks too; "Requested by your AI assistant …"). Budget: at most 34 lines and 2,400 characters with those lines. The worst 20-member case (60-character names, no reasons: 30 lines, about 2,200 characters) fits. `plan batch` checks it on every OS: over it, the batch is refused (`BATCH_TOO_LONG`, exit 4: split it); the text is never cut. The numbers become final in Phase 2, once the worst case shows uncut in macOS `display dialog` on a 13-inch MacBook, Windows `MessageBoxTimeoutW` at 1366×768 and 125 % (`npm run vm-lane`), and zenity at 1366×768 (Linux container, screenshot). Any clipping lowers them.
- **Phone:** title `batch.phone.<op>.<kind>` (80-character limit; the longest is 44). Body: `batch.phone.most` with the most sensitive member's goal and typed reason sentences, then "Requested by …" and the platform hint, cut at the existing 300-character reason limit with "(more on the computer)" or "(more in the chat)"; "Can't be undone." first after `--no-snapshot`. Most sensitive: an access or disruptive reason (lock, alarm, door, gate, garage, siren, wiping button) before a delete, a delete before another typed reason, typed before preview; the first in order on a tie. Button: "Delete" for a delete-only batch, else "Confirm". Result: the existing prefixes ("Done", "Partly done", "Not done", "Unclear") before the title ("Done: Delete 12 automations"), with `batch.phone.done` or `batch.phone.partly` and where to read why.
- **Form** (MCP elicitation): the batch text plus the existing code line ("Type 1234 to confirm."); after a failed snapshot the form asks again with `batch.no_copies`.
- **Chat code:** one code for the batch, last, as for every chat-code plan.
- **Chat `say`:** the batch text plus the plan's ask: the window, window-and-phone, phone, or code ask as today, or `batch.ask` for a preview batch; the demo prefix in the demo house. After apply: `batch.result.*` (plus `batch.result.rest` when members were not sent) or `batch.drift`.

Shared files get small hooks, coordinated with the localization PRs: `replanStored`/`humanPlan` and `confirmText`/`formPlan` learn kind `batch` (`--no-snapshot` offered after a failed snapshot, as for `raw`); the phone title takes a batch's whole-sentence title; `applyPlan` hands kind `batch` to `applyBatch` after its shared checks; `plan` gets the `batch` subcommand; `planMaxFiles`.

### 7. Chat apps (decision needed)

Finding: the existing `ha_plan`/`ha_apply` arguments can't express a batch. `ha_apply` needs nothing (a batch is one `plan_id`). `ha_plan` has no kind `batch` and no argument for a list of plan ids: `entity_ids` ("exact entity_ids") would misdescribe them, `requests` has the raw item shape, and `id` is one string. A new enum value alone leaves the hash of `TestMCPToolTextStable` unchanged, but it still needs an argument for the list.

Context: 0.28.0 already changes `ha_plan`'s text against 0.27.3 (raw plans #864; dashboards, rooms, templates #865; hash `28de730f…` against `c4861734…` in v0.27.3). Every claude.ai user who updates from 0.27.3 re-allows "Plan a change" once anyway. `ha_apply`'s text is unchanged since 0.27.3 and stays so.

- **A (recommended):** `ha_plan` gets kind `batch` and one new argument, `plans` (2 to 20 plan ids, in the order they run), and its description names the kind. `ha_apply` unchanged. `TestMCPToolTextStable` gets the new hash, stated in the PR. No extra reset for users coming from 0.27.3; RC testers re-allow `ha_plan` once more. Chat apps get the whole feature (the phone in `ha-nova serve`, form or window in `ha-nova mcp`).
- **B:** CLI only in 0.28.0. Chat apps keep one confirmation per typed plan and the grouped "yes" for previews; the `ha_skill` header says so. Chat apps get it with the next deliberate tool-text change, which then resets `ha_plan` for everyone.
- **C:** no text change, deletes only: kind `delete` also takes `entity_ids` (already an `ha_plan` argument, truthfully described) and makes a delete-only batch in one call; batches with changes stay CLI only. A second way to make a batch.

Duration in chat apps: `ha-nova serve` runs a phone-confirmed apply in its background job (the result waits for the next `ha_apply`). A form, window, chat-code, or preview batch in `ha-nova mcp` runs inside one tool call. Phase 2 measures a 20-member apply on the testbed; if it takes more than 45 seconds, I report the number with a proposal before building on.

### 8. Skills

- `skills/ha-nova/batch-safety.md`: the per-item exception goes. A new section, CLI batch: 2 to 20 automation/script writes and deletes, mixed allowed, are one `ha-nova plan batch` with one confirmation; the CLI holds the list, the order, the re-checks, the snapshots, and the ledger; the skill shows each member's preview without its ask or code, then the batch's `say`, and after apply the ledger. "One resource family per manifest" stays for the skills' own manifests and names the CLI batch as the exception. Capability matrix, `write` row.
- `skills/write/SKILL.md`: the Flow's grouped-set sentence and Phase 2's batch-delete bullet become one pointer: several automation/script writes or deletes in one task → plan each, show each preview without its ask, `ha-nova plan batch`, its `say`, one `apply`. Net words ≤ 0.
- `skills/ha-nova/grouped-change-set.md`: automation/script sets go to the CLI batch (matrix `write` row); in the cross-family cleanup, the automation/script items become one batch at their position in the order, instead of keeping their own codes.
- `skills/ha-nova/SKILL.md` (Confirmation Tiers) and `skills/ha-nova/write-safety.md` (multi-target): "one CLI code per item" becomes "one CLI batch".
- `cli/mcp_skill.go` header (a tool result, not tool text): the `plan batch` mapping (A), or "not in chat apps: confirm each plan" (B).
- Tests: `tests/skills/batch-safety-contract.test.ts` (its "automations OR scripts, never mixed" pin becomes the CLI batch pins; "one CLI code per item" appears nowhere), `grouped-change-set-contract.test.ts`, and the routing pins. Word and route budgets are not raised: detail lives in `batch-safety.md`, which only batch flows load.

### 9. The grouped change set

It stays for its other families (helpers, scenes, registry, service calls, to-dos, device swaps, migrate) and for tasks across families. Automation/script creates and updates move to the batch: one apply instead of one per plan, the CLI's re-check and ledger, up to 20. A task with a helper and automations keeps the grouped set for the helper and makes the automations one batch, one operation of the group; a typed batch leaves the group like any typed plan.

## Test plan

Go tests on the fake Relay (no keychain, no Home Assistant), run with an anchored `-run`:

- `plan batch`: each refused kind; 1 and 21 members; duplicate plan id and duplicate item; missing, expired, claimed, other-server, and edited typed members; a member changed since its preview (exit 2, members untouched); the order check; tier and channel; the claim (member files gone, a member applied alone is `PLAN_NOT_FOUND`, a second batch with it is refused, two concurrent `plan batch` runs: one wins); 20 members next to another open plan all kept.
- Seal: an edited member hash, request, order, text, or language in the batch file is `PLAN_INVALID` or drift; nothing sent.
- Apply: a mixed batch (automation create, script update, automation delete, script delete) sends one write per member with every snapshot before the first write, records undo receipts and the ledger, exit 0. Drift before any send: exit 2, zero writes. Drift at member 3: exit 6, two writes. Home Assistant refuses member 2: exit 6. An unverified member stops it. An unknown outcome: exit 7, never retried. A failed snapshot: exit 1, zero writes; `--no-snapshot` then sends all; `--no-snapshot` before a failure is refused. Expiry; two concurrent applies (one sends); a second apply refused; the wrong-code counter; a form plan without its answer refused; a window or phone plan without its answer refused.
- Surfaces, golden in en (and de if the catalog has it): window text and budget (worst case fits; over it `BATCH_TOO_LONG`); phone titles for every op and kind within 80 characters, body ranking, button, Android hint, no-snapshot; result notifications; form message; chat `say` per channel and for a preview batch; the same item lines in window text, form message, and `say`; window and phone racing on a batch (one apply).
- Chat apps (A): `ha_plan` kind `batch` argv and `plans` checks; `ha_apply` of a batch through a form and through `ha-nova serve`'s phone job; `TestMCPToolTextStable` with the new hash. (B: the header says no batch.)
- Skills: the contract tests of §8; budgets unchanged.
- Throwaway testbed (announced first): an agent deletes 12 test automations and changes 3 in one batch; the timing of 20 members.
- Per OS, the worst-case window shows uncut: Windows (vm-lane), Linux (zenity screenshot), macOS (13-inch).

## Known limits

- Not atomic: a stop leaves the earlier members done. Deleted ones come back through each row's `restore`, changed ones through `plan revert`, which keeps the newest 5 updates per server (as six single updates do today). A batch with more than 5 updates leaves only the last 5 revertible, so the skill offers a full backup first.
- Where only the phone asks (no window), the person sees the count and the most sensitive item there; the full list is in the chat or terminal.
- A member that changed before anything was sent ends the whole batch, and its members are used up: the agent plans them again.
- After a stop, the copies of members that were never deleted stay in the snapshot store (one name per item; a later delete replaces it).
- A list whose typed reasons make the window text too long is refused, never shown cut.
- Time grows with the list; over a slow link a 20-member apply can outlast a chat app's tool timeout (measured in Phase 2).

## Choices made here (override if you disagree)

1. Members move into the batch at `plan batch` and their files go away, instead of being referenced and claimed at apply: one file, no state across files, and no member can be applied alone afterwards.
2. The batch's 15 minutes start at `plan batch` (members must be unexpired then), not at the first member's plan: a 20-item task would otherwise expire while the person reads the previews.
3. Every delete snapshot is taken before the first send: a failed copy never leaves a half-done list, and `--no-snapshot` keeps one meaning (the whole batch, after the user chose it).
4. Drift before any send ends the batch without making a new one: the changed item's new preview must be seen first.
5. Mixed lists are allowed, with the order check (§2).
6. A preview batch takes one natural "yes" for up to 20 writes (the grouped set allowed 10): decision 1's cap, and every member's preview is shown first.
7. A create counts as a change in the phone title.

## Open question

Chat apps: A, B, or C (§7). Recommended: A.

## Delivery

One PR, `feat/batch-confirmation`, rebased onto the localization PRs and #891 (they touch the same confirmation files). Estimate: about 750 production lines (new files `plan_batch.go`, `verb_apply_batch.go`, `confirm_batch_text.go`, each under 400 lines; small hooks elsewhere). Over 800, it splits into the CLI (plan, apply, texts) first, then skills and chat apps, stacked. `docs/reference/safety.md` gets the batch row; README items go into the PR body for release prep.
