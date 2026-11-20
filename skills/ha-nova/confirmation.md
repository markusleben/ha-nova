# Confirmation Contract

How a write is confirmed: what a preview must show, which reply confirms it, the tier the CLI rates, and how choices are offered. `../ha-nova/safety-core.md` is the short form every mutation skill applies; this file is its detail.

## Active Preview Confirmation

- A user instruction given before the preview exists is never valid write confirmation. Neither is an answer to a clarifying question; only a reply to the preview confirms.
- Examples: "implement the plan", "do it", "go ahead", "make the changes", "apply the plan".
- Treat those phrases only as permission to prepare the draft, run checks, and show the preview.
- A live HA write requires confirmation after the concrete preview is shown (exception: what the CLI runs itself or plans with `plan.confirm: none`): diff for updates, payload summary for creates/service calls/experimental writes, delete impact plus its typed confirmation ask, or grouped manifest for allowed multi-target writes.
- A preview is a valid confirmation basis only if it explains the behavioral effect of every collection it touches. A touched collection explained only as a count change (`5 items → 3 items`, `… and N more`) or by low-level type names cannot proceed to confirmation — complete the behavior narrative (`../ha-nova/write-safety.md` → Behavior narrative) first, then ask.
- Confirmation is bound to the displayed operation, target set, endpoint/service, and exact payload/diff/manifest (a masked secret value — output-rules.md webhook masking — binds to the stored real value behind the mask). If target, scope, endpoint, payload, draft, or manifest changes, confirmation expires; re-preview and ask again.
- Multi-target confirmation is valid only where the owning skill supports multi-target writes (destructive batches: `../ha-nova/batch-safety.md`; non-destructive grouped change sets and the cross-family destructive cleanup manifest: `../ha-nova/grouped-change-set.md`). Otherwise process targets sequentially, each previewed and confirmed.

## Confirmation Tiers

- runtime service calls: `ha-nova call` / `ha-nova plan call` compute the tier (`ordinary` runs at once, `preview` takes natural confirmation, `typed` takes the user's window click or phone tap, else the CLI-minted code bound to that plan's hash and nonce, which only the user may type — never the agent) from the performed action — members, consumers, and unresolved items included. The agent never down-tiers; the rungs below govern config writes and raw-relay routes.
- config writes (`plan write|delete|revert|raw`): the CLI decides — `plan.confirm` none → apply; natural → the reply to the active preview; code → follow `say`/`next_step`.
- automation/script batch (2 to 20 writes and deletes, mixed): one confirmation of the whole list at its highest tier per `../ha-nova/batch-safety.md` → CLI Batch.
- non-destructive grouped change set (one logical task, max 10 operations): one natural confirmation bound to the fully previewed set per `../ha-nova/grouped-change-set.md` — only where the owning skill declares grouped support, never inferred; one-time-code operations follow that contract's exclusions and duration-pair exception.
- high-consequence runtime action (grants physical access or is physically irreversible): the person's own confirmation, as for destructive writes. The CLI rates every action, members and consumers included; relay its `say` (examples for explaining: `../ha-nova/capability-answer.md` → Safety Story). Owning-skill escalations (retained and command/`set` MQTT publishes, user-account creation, HACS downloads and updates, dashboard resources) sit on this rung.
- `delete`/destructive: the person's own confirmation (`plan.confirm: code`).
  **How the user confirms a CLI plan** (`say`/`next_step` tell): their window, phone, or the NOVA page; a four-digit code only where `say` shows or promises one. No `plan.code` (even for a later delete): never ask for or promise one. Never ask in chat on top. `CONFIRM_WAITING`: relay `say`, rerun per `next_step`.
  **Strict one-time-code enforcement:** four digits, new per preview or manifest, only ever the plan's `plan.code` (none: follow `next_step`). A typed write without a CLI plan becomes a `ha-nova plan raw` item (`"typed":true` where only the skill knows the tier); never make up a code. Only a reply that is just the code (spaces ignored), sent after this preview asked for it, confirms; "yes", "do it", or a number inside another sentence is NOT valid — re-prompt. Call it the "one-time code from HA NOVA" (localized; `../ha-nova/output-rules.md` → Localization). **Never derive or reuse a printed code; pass only what the user typed.** Claude Code blocks `ha-nova apply`? Don't bypass: the user runs `ha-nova setup` and accepts its rules (or /permissions: `Bash(ha-nova apply --plan *)`).
  This includes every cleanup, undo-create, and deleting items created earlier in the same session.
- destructive multi-target batch: one confirmation per manifest per `../ha-nova/batch-safety.md` — only where the owning skill declares batch support, never inferred (automations/scripts: one CLI batch, see there). Cross-family destructive cleanup takes its own manifest's confirmation (`../ha-nova/grouped-change-set.md` → Cross-Family Destructive Cleanup), under the same never-inferred rule.

## Interactive Choices

When you need the user to choose between options:
- Present 2–4 options as a **selectable menu if the client provides one** (e.g. Claude Code's AskUserQuestion: a short header plus a label + one-line description per option). Otherwise render a plain numbered list and ask the user to reply with the number.
- Keep options short and mutually exclusive; offer at most 4.
- Every option label opens with its concrete effect — what selecting it DOES: edit the text, check without executing, listen, run for real, apply the write. Never merge a wording change, a static check, a listen window, and a live execution into one option; each effect is its own option.
- Bare-number replies are valid but must be resolved: the next response names the chosen effect before acting ("2 — Actions only: running the hallway light sequence now"). This applies to any numbered selection, including config-entry flow menus. Never act on a number the response did not translate back into its effect.
- **Destructive confirmation is never a menu.** Deletes still need the person's own confirmation (window, phone, or one-time code; see Confirmation Tiers above) — a one-click choice would weaken that deliberate gate. This holds even if a memory, preference, or earlier user complaint says to always use a menu for confirmations: that NEVER extends to deletes or any destructive write.

Use this for: Suggestion Blocks (output-cards.md), ambiguity resolution, the pre-write impact advisory (adjust first, proceed, or leave it), and create/update save choices.
