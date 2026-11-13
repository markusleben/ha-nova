# Calmer confirmations: fewer asks for everyday changes

Status: active

Date: 2026-11-06. Owner: tier engine (`cli/plan_config.go`, `cli/tier_rules.go`, `cli/plan_call.go`), config apply (`cli/verb_config_apply.go`, `cli/snapshot_store.go`), setup (`cli/claude_apply_rule.go`), skills `write`, `service-call`, `ha-nova` (`test-run.md`, Safety Core). Changes the promise in `docs/reference/safety.md` (first table row) and the Safety Core block of `docs/reference/skill-architecture.md`. Builds on `docs/work/2026-11-02-fewer-asks-spec.md`.

## Today

Measured on the test bed (Home Assistant 2026.11.0) with a headless agent and the skills of this tree: 22 everyday requests from a non-technical user (lights, rooms, climate, covers, locks, scenes, automations, renames, to-dos) in German. 11 ran without any ask. The other 11 raised 13 asks that a user had to answer, plus 3 from an optional test offer and 6 caused by the fixture itself (demo entities without a registry row, a lock that never answered, a tilt-only cover). Of the 13, 2 protected something critical (unlock, open the garage), 1 caught a plausible misunderstanding (a new automation), and 10 asked about explicit, everyday, or reversible changes: edits of an automation the AI had just created, a rename, saving a scene, removing a shopping-list item, deleting the AI's own automation, and two covers without a `device_class` (one of them a pool cover) that are typed as possible doors. Each ask cost about 20–40 s of reading, answering, and waiting for the extra agent turn; the post-write test offer turned one yes into three more asks.

What the asks protect differs by tier:

- **typed** (window click, linked-phone tap, else a one-time code the CLI derives and checks) is the only human gate the software enforces. It covers unlocking, opening a door, gate, or garage, disarming, deletes, code that runs, and anything the engine can't read that could reach those.
- **preview** waits for a natural "yes" in chat. The CLI can't see that yes (`ha-nova apply --plan` of a preview plan needs nothing), so a preview protects against a misunderstanding, not against a misbehaving AI.

So asks can go where a preview catches nothing a user would miss, while the typed set stays exactly as it is.

## Goal and invariant

Fewer and better-placed asks for explicit, everyday changes. Unchanged: every typed-tier action keeps its window, phone, or code on every route (computer, chat apps, the planned Relay gate), and every error, unreadable state, older CLI, or older App ends at today's tier or stricter.

## Decisions (maintainer, 2026-11-06)

1. The promise in `safety.md` and the Safety Core becomes the sentence under Contracts: the CLI checks every change, and its check, not undo, decides what runs directly.
2. Deleting an automation, script, or dashboard stays typed, also for items HA NOVA created in the same conversation.
3. Covers without a `device_class` stay typed; only the reason text changes (item 5). HA NOVA never sets a cover's class.
4. No comfort levels and no owner switch.
5. Test offer: after every create or update, one short closing line offers a test preview; the full Test Plan Card only after yes (item 1).

Implementation: 0.29.

## Design

One rule decides eligibility: **only the CLI rates a plan ordinary.** Skills key on the plan envelope (`status`, `plan.confirm`), never on the kind of change. A newer skill with an older CLI therefore still asks.

### Phase 1: flow and setup (skills, setup, texts)

1. **A short test offer, the card only on request** (maintainer decision, 2026-11-06). After every verified create or update of an automation or script, the receipt ends with one short line offering a test preview. Only after the user's yes does the skill show the full Test Plan Card (`skills/ha-nova/test-run.md`); the run itself follows the card's tier, so a yes to the offer line never runs anything. When the user picks a run, it plans `automation.trigger` (`skip_condition: false`) after the write verified, shows that plan's `say`, and applies only that plan id. Never in a turn with another open preview; a typed member keeps its own ask.
2. **No automatic retry after an unverified typed action.** The result says what is known and unknown; read-only re-checks are fine. No resend and no substitute target; a new attempt is a new plan with its own ask. A code is never reused.
3. **Feature preflight in the CLI.** `plan call` refuses (`UNSUPPORTED_FEATURE`, before any ask) an open, position, or tilt service on a cover whose `supported_features` was read, is non-zero, and lacks the bit, including `homeassistant.turn_on|toggle` mapped onto a cover. Close and stop are never refused; an unknown mask keeps today's tier.
4. **Claude Code allow rules.** Setup offers one rule per read and plan verb next to the apply rule: `Bash(ha-nova find *)`, `get`, `history`, `trace`, `plan`. `call`, `relay`, and `snapshot` stay outside; never `Bash(ha-nova *)`. The rules are recorded for uninstall and named in the setup text. Plan verbs write nothing to Home Assistant.
5. **Texts** (`cli/confirm_strings.*.json`, en and de): the `COVER_CLASS_UNKNOWN` reason says why it asks and that a cover which really is a shade, awning, or window can get that "Show as" class in Home Assistant; it never suggests a class to stop the ask. The phone-link offer appears once with a chat code, not under every code.

### Phase 2: edits that undo themselves (CLI)

6. **Updates and reverts of existing UI-managed automations and scripts** become ordinary only when all of these hold; otherwise today's preview:
   - the reasons are exactly `CONFIG_WRITE` or `CONFIG_REVERT`, coverage is complete, and there are no notes (`NO_REGISTRY_ROW`, `SERVICE_NOT_FOUND`, any typed lift keep their tier);
   - no automation or script that calls the item rates typed (`search/related` read; a failed read keeps preview);
   - apply re-checks drift as today, and the checkpoint (`before_config`) is saved before the write; a failed save sends nothing and leaves the previous checkpoint intact;
   - the save neither evicts another target nor replaces an earlier checkpoint of this target from the same conversation, and `before_config` holds no hidden value.
   The receipt shows the CLI's diff and one line from the result: the configuration can be put back with "undo" (`plan revert`); what already ran is not undone. Creating a new automation or script keeps its preview.
7. **The built-in shopping list** (`shopping_list` platform): `todo.remove_item` of up to 5 items named and read first becomes ordinary in `ownedOrdinaryServices`, as an intentional low-risk delete (the receipt lists the items). The `shopping_list_updated` listener lift stays. Other to-do platforms and `remove_completed_items` keep their preview.
8. **Dashboards stay preview.** Card content is not classified today (a card can unlock with one tap); that gap is tracked separately.

### Phase 3 (after phase 2 data)

9. Display names and aliases of entities and devices outside any lock, alarm panel, or access-cover family, with a CLI-recorded registry undo under the conditions of item 6. Entity id renames, areas, and labels stay previewed: they can change what existing automations reach.

## Not in scope, and why

- **Restorable deletes as preview.** The tier is fixed at plan time but the snapshot is taken at apply, a failed snapshot allows `--no-snapshot`, and pruning copies is preview: a "restorable" delete could end irreversible with no typed step. Deletes stay typed.
- **HA NOVA setting a cover's class.** A rename followed by a typed reclassification under the new name could turn a garage door into a "shade" that opens without asking. Text only (item 5).
- **Session grants, comfort levels, remembered approvals, trusted network or phone nearby, remembered codes.** Each is a grant the AI could trigger, or a new setting with nothing left to switch after phase 2. Two unlocks ask twice.
- **Ordinary scenes and scripts whose members all read ordinary.** A candidate, since members are classified on every run; measure on a fully registered fixture first.

## Contracts

- `docs/reference/safety.md` first row and the Safety Core block (byte-identical in every mutation skill) change once: "Every change is checked by the CLI. Everyday actions and edits it can put back run directly and say how to undo them; other changes are previewed. Unlocking, opening a door, gate, or garage, disarming, and deletes need your own confirmation." `tests/skills/ha-safety-contract.test.ts` pins that skills apply without a preview only on `plan.confirm == "none"`.
- No new reason code, tier value, or plan schema version: a plan with only `CONFIG_WRITE` or `CONFIG_REVERT` that meets item 6 is ordinary. `TestTierReasonCodesAreDocumented` and `TestMCPToolTextStable` stay unchanged unless tool text names previews.
- The typed set is unchanged, so the Relay gate spec (`docs/work/2026-11-03-relay-typed-gate-spec.md`) needs no change: ordinary and preview both pass the gate. Chat apps: config plans stay Manage everything only (`chatPlanKinds`).
- An older CLI answers preview (stricter); an older App has no new behavior to lose.

## Tests

- Go: golden and fail-open cases for item 6 (each condition failing alone keeps preview: an extra reason, incomplete coverage, a typed caller, a failed `related` read, a failed or evicting checkpoint save, a hidden value, drift at apply); item 3 (open on a tilt-only cover refused, close never refused, unknown mask unchanged); item 7 (shopping list ordinary up to 5, a listener that unlocks keeps typed, other platforms preview); setup rules written, uninstalled, and never `ha-nova *`.
- Skills: the envelope rule and the test-offer flow in `tests/skills/`; word budgets are ratchets.
- Agent run on the test bed (Claude Code, default permission mode with the setup rules): the measured task set again, asks per task before and after, plus the unlock and garage tasks still asking.

## Known limits

- An ordinary edit lands before the user reads it; an automation can run once before "undo". The receipt says so.
- The undo is one step per target on this computer; another computer or the Home Assistant UI can't see it.
- Measurements came from a small fixture without a linked phone or window (every typed ask showed a chat code) and with the agent in bypass permission mode; client permission prompts were estimated from the commands, not measured.
- Large homes can still turn everyday calls typed through `LIMIT_CUT`; improving enumeration speed is separate work that keeps incomplete scans typed.
