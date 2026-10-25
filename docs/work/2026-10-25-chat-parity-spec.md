# Chat apps can do everything the CLI can (2026-10-25)

Status: active

Target: 0.28 (with the directory-readiness tool texts, so "Always allow" resets once). Parents: `docs/work/2026-10-19-chat-bridge-contract.md`, `docs/work/2026-10-17-confirmation-hardening-spec.md` (follow-ups: "a CLI plan for raw writes (`plan raw`)", the HACS/resource residual), `docs/work/2026-10-24-directory-readiness-spec.md`, `docs/work/2026-10-24-chat-apps-household-spec.md`.

## Problem

A user asked Claude (chat app) to rename a device. Claude said this works only on a computer. On a computer, the organize skill renames it with one allowlisted raw write (`config/device_registry/update`, `cli/relay_raw_allow.go:72`). The bridge has no tool for it, and the skill header tells the model to refuse: "Everything else (helper, dashboard, scene, registry or organize edits, other relay writes, logs, template probes, files on disk) needs a client with a shell and the HA NOVA CLI: say so" (`cli/mcp_skill.go:36-39`).

What the CLI does and chat apps can't (2026-10-25, `main` = `f686de25`):

| CLI (skills use it) | Chat today |
|---|---|
| `relay ws` / `relay core` writes on the raw allowlist: registry, helpers, scenes, areas/floors/labels, energy, backups, HACS, voice, recurring calendar events, blueprints, config flows, users | nothing |
| `relay core` GET, e.g. `/api/error_log`, a flow step's GET (reauth), a camera frame (`--out-binary`) | `ha_read`: 7 path families (`mcpReadablePath`, `cli/mcp_args.go:24`) |
| `relay ws` reads: every type marked read in `rawWSAllowed`, also inside a `collect_events` envelope | `ha_read`: the smaller `readOnlyWSTypes` (`cli/relay_session.go:28`), no envelope |
| `relay jq`, `--jq`, `--jq-file` (about 50 uses) | nothing; whole lists land in the chat |
| `POST /api/template`, `POST …/check_config` (change nothing) | nothing |
| `relay files` (YAML packages, ESPHome) | nothing |
| `relay backups` (config snapshots before a destructive write) | nothing |
| `snapshot show/verify/save`, `diff` (update-revert of helpers, output cards) | nothing |
| `plan write/delete/revert --domain dashboard` (`--base`, `--require-admin`, `--hide-sidebar`), `get --dashboard`, `get --dashboards` | `ha_plan` domain is `automation|script` only; `ha_get` needs an entity |
| `plan rooms` (built from `map --insights`), `plan template` | not in `ha_plan`'s kinds; `ha_map` has no `insights` |

Nothing stops the next CLI feature from opening the same gap.

## Goal

- For the owner (household level "Manage everything"), chat apps do everything the skills do on a computer, with the same allowlist, the same guards, and the same confirmation tiers.
- Only managing HA NOVA itself stays on the computer: `setup`, `pair`, `doctor`, `update`, `check-update`, `version`, `uninstall`, `cloud`, `chat-apps`. `relay health` and the update check are covered by `notices[]` on every result.
- A CI test fails when a CLI command or flag the skills use has no chat route and no listed reason.
- Users learn, where they add HA NOVA to Claude, why Claude asks before each step and what to set.

## Non-goals

- A new tool. The same ten tools gain kinds, endpoints, and arguments (directory spec: no new tool).
- Moving the CLI skills from raw relay writes to `plan raw`. The CLI keeps skill-minted codes for raw deletes until a follow-up.
- Anything below "Manage everything": household levels never reach raw reads, plans other than calls, or files (household spec, Security).

## Design

### One guard per endpoint, shared with `ha-nova relay`

Today the raw checks are spread over `checkRawRelayRequest`, `checkRawAccessTarget`, `rawRelayRead`, and inline steps in `runRelayProxy` (`holdsHiddenValue` refusal and backups body redaction, `cli/relay.go:379-383`; response redaction with nonces on for ws/core, `:469`). They move into one function per endpoint that both `ha-nova relay` and the bridge call: request checks before sending, redaction of every response (planning, apply, each item). The bridge adds no route the CLI refuses and skips no check the CLI runs.

Two guard fixes that hold on the computer too:
- `config/device_registry/update` with `disabled_by` is refused when the device owns a lock, alarm panel, or access cover (today only the entity registry checks this, `cli/relay_raw_guard.go:221`).
- `checkRawAccessTarget` runs again right before each item is sent, against the live registry, so an earlier item in the same plan (a renamed entity id, a finished flow) can't slip past it.

### Reads: `ha_read`

`ha_read` sends through the raw relay path (not `relaySession.read`, whose `checkReadOnlyRequest` is narrower and has no POST).

- `endpoint: core`: a GET that passes `checkRawCore`, `readablePath` (no proxies or streams), and `rawRelayRead` (no flow step GET: it can finish a flow). Plus two POSTs that change nothing: `/api/template`, `/api/config/core/check_config`.
- `endpoint: camera`: `entity_id`; returns the frame as MCP image content (same 8 MiB bound). On a computer the agent sees the frame too.
- `endpoint: ws`: every type marked read in `rawWSAllowed`, with the per-type rules of `checkRawWS`, also as a `{message, collect_events}` envelope. Except `blueprint/import` (Home Assistant fetches any URL), `camera/stream`, and `media_source/resolve_media` (signed live URLs): those go through `ha_plan kind raw`, so the user sees them first.
- `endpoint: files`: `list_dir`, `read_file`. The Relay's file switch still decides (default off).
- `endpoint: backups`: `list`, `load`.
- `endpoint: snapshot`: `show` (list, target, domain), `verify` (`against` inline).
- `endpoint: diff`: `before`, `after` inline; same output as `ha-nova diff`.
- `jq`: optional filter, compiled by `compileJQFilter` (`cli/jq.go:69`), as `relay jq` and `--jq` on a computer. A result over 64 KiB is refused with "filter with jq" instead of filling the chat.

### Writes: `ha_plan kind raw`, then `ha_apply`

- `requests`: 1 to 20 items, run in order, each `{endpoint: core|ws|files|backups, payload}`. 20 is the snapshot batch limit (`skills/ha-nova/config-snapshots.md:34`); a skill's own lower limit (grouped change set: 10) still applies. `checkArguments` learns arrays of objects and checks the nested fields.
- `storedPlan` gets `RawRequests`; `applyPlan` gets a raw branch with its own validation (`cli/verb_apply.go:183-187`). Planning refuses an item the guard refuses; reads belong in `ha_read`, except the reads listed above that only plans may send and a flow step GET.
- Say and confirmation prompt: one line per item, in plain words (what it changes and on which names), so the window, the phone, and the form show what the user confirms (`confirmationPrompt` today returns "" for such a plan, `cli/confirm_say.go:69`).
- Tier per item, taken from the skills' rungs on a computer; the plan takes the highest:
  - `typed`: every delete or remove (WS `*/delete`, `*/remove`, `recorder/clear_statistics`, core `DELETE …/entry/…` and `…/scene/config/…`, `files delete_file`, `backups delete`); `config/auth/create` (`skills/admin/SKILL.md:83`); `energy/save_prefs` that empties a non-empty list or drops more than one of its entries, compared by value with `energy/get_prefs` at planning (`skills/energy/SKILL.md:54,96`); `hacs/repository/download` and `lovelace/resources/create|update` (third-party code that runs in every signed-in browser; Decision 3); `config/entity_registry/update` carrying a new id, area, or label onto a lock, alarm panel, or access cover, and `person/update` of its trackers (`skills/device/SKILL.md` → Access Check); any item with `typed: true`, which the skill sets where it takes the code for a step the plan would preview (escalate only, in the hash).
  - `preview`: everything else, flow cancel and `backups prune` included (`config-snapshots.md:18`: natural confirmation).
- Apply re-runs the guard per item, sends the items in order, stops at the first failure, and returns each item's redacted response (a flow's next form, a created id). A sent item with no answer is `OUTCOME_UNKNOWN`; later items are not sent.
- A config snapshot goes first in the same plan (`backups save`, then the delete), so the user confirms once, as on a computer. If that save fails, apply sets `SnapshotFailed`, and the user may go on without a copy under the same confirmation (`no_snapshot`), as `config-snapshots.md:89-95` allows.
- Update-revert for helpers: when apply sends a `<storage collection>/update`, it reads the item before and after through `<collection>/list` and saves the checkpoint itself (the record of `skills/ha-nova/update-revert.md` → Manual capture). The CLI keeps the manual capture.
- Drift: the hash covers the items exactly; any change is a new plan.

How many questions the user sees: one preview reply per step, as on a computer. A flow menu choice is its own confirmation (`skills/integration-setup/SKILL.md:72`): the model plans and applies it right after the user picks. A rename is one plan (metadata snapshot, then the update).

### Dashboards, rooms, templates

- `ha_plan`: `domain` adds `dashboard` for write, delete, and revert, with `id` (url_path), `base` (config digest), `create`, `title`, `icon`, `require_admin`, `hide_sidebar`; kinds `rooms` and `template` (`name` from the CLI's list); revert domain `device_area`.
- `ha_get`: `dashboard` (url_path, `config`) and `dashboards`; `entity_id` is no longer required when one of them is set.
- `ha_map`: `insights`.

### Skill header

The header (`cli/mcp_skill.go`) maps every CLI command and flag to its tool: `relay ws|core` writes and flow step GETs → `ha_plan kind raw`; `relay` reads, `files`, `backups`, `snapshot`, `diff`, `--out-binary` → `ha_read`; `relay jq`, `--jq`, `--jq-file` → `ha_read`'s `jq`. The "Everything else … needs a client with a shell" line goes. For the computer-only list it says: "this sets up HA NOVA on a computer; tell the user to run it there".

### Parity test (`cli/mcp_parity_test.go`)

1. Every `ha-nova <command> [<sub>]` and every flag after it in `skills/**/*.md`, and every flag in the CLI usage strings of the verbs the skills use, is in one table: its chat route (tool + kind/endpoint/argument), or the computer-only list with a reason. A new command or flag without an entry fails.
2. Every CLI `plan` kind and `--domain` value has an `ha_plan` route.
3. Every `rawWSAllowed` write and `rawCoreWrites` entry plans through `kind raw` (fake relay) with the tier from the table above; every `rawWSAllowed` read reads through `ha_read` or is on the plan-only read list.
4. `ha_read` and `ha-nova relay` refuse the same requests (table test over the cases in `relay_raw_guard_test.go`).
5. The header calls nothing on the computer-only list doable in chat, and nothing chat-routed computer-only.

### "Always allow" where users add HA NOVA

On a computer, setup lets Claude Code run `ha-nova apply --plan` without asking, but keeps Claude's own question when the model passes a chat code (`cli/claude_apply_rule.go:13-19`). Chat apps can't split one tool by its arguments, so the advice depends on the phone:

- Phone linked (typed plans are decided on the phone or in the window, never by a code in chat): set **all** HA NOVA tools to "Always allow". Same as on a computer.
- No phone: set all tools but "Run a confirmed change" to "Always allow". A chat code is visible to the AI, so Claude's question before `ha_apply` stays the only check the AI can't answer itself.

Where it shows:
- Setup page, after chat apps are on (`cli/setupweb/strings.en.json` `setup.page.home.note_chat`), and the NOVA page's Chat apps section (`nova/src/http/handlers/nova-page-view.ts`): a short "Next" block under "Add to Claude", in the variant that fits (both know whether a phone is linked).
- In chat: the model answers "why does Claude ask every time?" with the same steps (`skills/ha-nova/capability-answer.md`, mapped in the header). Tool descriptions don't carry it: they reset approvals, and the directory review wants no behavior text there.
- `docs/reference/chat-apps.md`: the same two variants. `README.md` follows in the 0.28 release-prep PR (README edits only there).

## Docs and copy

Every text that says chat apps can't do something, or gives the old "Always allow" advice, changes in the PR that makes it true (sweep 2026-10-25, `main` = `f686de25`):

| Where | Today | Change | PR |
|---|---|---|---|
| `cli/mcp_skill.go:35` header | "Everything else … needs a client with a shell" | computer-only list only | 5 |
| `cli/mcp_args.go:170` (`ha_read` refusal), `:347` (skill without chat writes) | "needs a client with a shell" | the refusal names the route, or the computer-only reason | 2, 3 |
| `docs/reference/chat-apps.md:37-44` | read-only + "Control a device" | the two variants by phone | 5 |
| `docs/reference/chat-apps.md:102` | "helpers, dashboards, and files still need HA NOVA on a computer" | removed; computer-only list (setting up HA NOVA itself) | 5 |
| `cli/setupweb/strings.en.json` (`setup.page.home.note_chat`), `nova/src/http/handlers/nova-page-view.ts` (Chat apps) | no advice | "Next" block, variant by phone | 5 |
| `skills/ha-nova/capability-answer.md` | no answer for "why does Claude ask" | the steps, both variants | 5 |
| `nova/DOCS.md` (Chat apps) | no advice | one line, link to chat-apps.md | 5 |
| `nova/CHANGELOG.md`, `docs/work/next-release-body.md` | — | new entry: chat apps can do what the computer can; new advice | 5 |
| `README.md:175`, `:182` | old advice; "Helpers, dashboards, and files still need HA NOVA on a computer" | same as chat-apps.md | 0.28 release-prep (README edits only there), listed in `next-release-body.md` |
| hanova.app `index.html:478`, `demo/index.html:88` | old advice | site PR after the 0.28 tag, from the README (site rule); the demo has no phone, so its copy is the no-phone variant | after release |
| `docs/reference/` (`client-integration.md`, `ha-api-matrix.md`, `safety.md`, `skill-architecture.md`) and `docs/work/2026-10-19-chat-bridge-contract.md` | tool and route lists | `ha_read` endpoints, `ha_plan kind raw`, the shared guard | 1-5, each with its change |

Guard against regressions: `tests/docs/banned-wording.json` gets rules (scope all) for "still need(s) HA NOVA on a computer" and "client with a shell", read by `tests/docs/canonical-wording.test.ts` and by hanova.app's `check-sync.sh`. `README.md` is an allowed exception in the test until the release-prep PR, which drops it. The parity test (item 5) covers the bridge's own texts.

## Security

- Same allowlist, guards, and redaction as the CLI, in one function per endpoint; the two guard fixes above hold on both.
- Every raw write in chat is a plan; on a computer, the skill does the preview. Typed raw plans use HA NOVA's window, phone, or form, else a chat code. A chat code is visible to the AI (hardening spec), so it is no stronger than a skill-minted code; that limit is unchanged, and the "Always allow" advice keeps Claude's question for it.
- Reads that leave the house or hand out live URLs (`blueprint/import`, `camera/stream`, `media_source/resolve_media`) are plan-only, so "Always allow" on `ha_read` never sends them unseen.
- Files: reads and writes reach `/config` only when file access is on in the Relay App (default off); the Relay denies `.storage` and `secrets.yaml`. A file's content is not redacted inside (it is one string): a key written inline in a YAML file reaches the chat, as it reaches the agent on a computer. Accepted for "Manage everything" and said in `docs/reference/chat-apps.md`.
- A YAML file can run commands after a restart; the restart is a typed call, as today.
- Household: below "Manage everything", the bridge refuses raw reads, plans other than calls, and files, on the vouched user id (household spec).
- Prompt injection from Home Assistant data can make the model plan a raw write. A preview plan then runs when the model believes the user agreed, as on a computer; a typed plan needs the window, the phone, the form, or a code the user types.

## UX copy (draft)

Setup page and NOVA page, under "Add to Claude", phone linked:

> **Next: stop Claude from asking every time.** At first, Claude asks before each step. That's Claude, not HA NOVA. In Claude, open Customize → Connectors → HA NOVA and set every tool to "Always allow". HA NOVA still shows you a preview before bigger changes, and your phone asks before unlocks, the alarm, and deletes. Claude may ask again after an update.

No phone:

> **Next: stop Claude from asking every time.** At first, Claude asks before each step. That's Claude, not HA NOVA. In Claude, open Customize → Connectors → HA NOVA. Set every tool to "Always allow", except "Run a confirmed change": leave it on "Needs approval". Then Claude still asks before a change you confirm with a code. Link a phone, and you can allow that one too. Claude may ask again after an update.

Computer-only reply (header): "That sets up HA NOVA itself, so it runs on your computer: `ha-nova <command>`."

## Test plan

- Go: the parity test; tiers per item type; apply order, stop at first failure, outcome unknown, `SnapshotFailed` from a leading save; the access-target re-check between items (rename to a lock id, then remove); device disable refused for a device with a lock; helper checkpoint after a verified update; redaction of plan, apply, and item responses; `ha_read` jq and size cap; camera frame as image content; `TestMCPToolTextStable` updated once, with the directory texts.
- Test bed agent run over the bridge, German prompts: rename a device, create a helper, add a dashboard card, delete a scene (typed), set up an integration through a flow, read the error log, edit a YAML package with file access on, look at a camera.
- Maintainer on claude.ai: the rename from the report works; the "Always allow" block reads right on the setup page and the NOVA page, in both variants.

## Delivery

Release-bound (Go, App). PRs, each with reviewers per `AGENTS.md`:

1. Shared guard per endpoint + the two guard fixes (also hardens the CLI).
2. Reads: `ha_read` endpoints, `jq`, size cap, camera.
3. Writes: `plan raw` + `ha_plan kind raw` + apply + say/prompt + helper checkpoint.
4. Dashboards, rooms, templates, `ha_get` dashboards, `ha_map` insights.
5. Header, parity test, "Always allow" copy (setup page, NOVA page, skill, docs).

Tool text changes ship in the same release as the directory-readiness texts, so "Always allow" resets once.

## Decisions (maintainer, 2026-10-25)

1. Chat apps reach CLI parity before the 0.28 release; no quick 0.27.4.
2. "Always allow" advice by phone, as above: all tools with a linked phone; without one, "Run a confirmed change" stays on "Needs approval".
3. HACS downloads and dashboard resources are typed everywhere (option A): in `plan raw`, HACS component updates through `update.install` too (the call tier `HACS_INSTALL`), and the CLI skills (`skills/hacs/SKILL.md`, `skills/dashboard/SKILL.md`, `skills/updates/SKILL.md`) take the one-time code for the same steps on a computer. A behavior change on the computer, called out in the PR.
