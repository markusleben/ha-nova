# Marketplace lifecycle through HA NOVA (0.31, #1099)

Status: active

Follows the 0.30 refusal (#1096, `MARKETPLACE_WARNING`). Maintainer decision D2 (2026-11-10): accepting the Marketplace warning for the system user only as a typed step with a fixed text that names the system user and the lasting effect; check first whether other Apps share that user; install, uninstall, and restart typed, at the Relay's gate too.

## Problem

Home Assistant 2026.11 builds HACS in as the Marketplace. It installs and updates only for a user who accepted its first-run warning, and it stores that acceptance per Home Assistant user. HA NOVA's user can't open the Marketplace panel, so since 2026.11 HA NOVA can't install or update anything from the Marketplace; 0.30 refuses up front with a clear reason.

## Facts from Home Assistant Core 2026.11.1 and the Supervisor

- `marketplace/warning/accept` (WS, admin only, no fields) stores the acceptance for the calling user: `config_entry.data.warning_accepted[user_id] = {version, accepted_at}` (`marketplace/websocket/__init__.py`, `base.py` `async_accept_warning`).
- No command or UI withdraws an acceptance. It ends only when the user is removed (`_async_follow_removed_users`) or Home Assistant raises `WARNING_VERSION` (`const.py`).
- `marketplace/info` reports `warning_accepted` for the calling user only.
- Gated on the caller's acceptance: `marketplace/repository/install`, `version`, `beta`, `repositories/add`, `repositories/detect`, and `update.install` on a Marketplace update entity. `marketplace/repository/uninstall` is not gated; it refuses while an integration still has config entries (`repository_in_use`).
- `update.install` from an automation or script (no user in the context) installs as soon as any user accepted (`update.py` `async_install`).
- **Who HA NOVA is.** On the NOVA App the Relay sends every request with `SUPERVISOR_TOKEN` through the Supervisor's Core proxy, which checks the App's Home Assistant API access and then forwards over its own connection (`supervisor/api/proxy.py`, `supervisor/homeassistant/api.py` `connect_websocket`); Core authenticates that connection as the Supervisor user (`http/auth.py` `async_authenticate_supervisor_unix_socket`). That user is the system-generated administrator **"Supervisor"** (Core `hassio/__init__.py`). **Every App with `homeassistant_api: true` acts as that same user**, and so does the Supervisor. The proxy denies only `supervisor/*` and `hassio/*` WS types, so any such App can already accept the warning for itself today. On a standalone Relay HA NOVA is the account whose long-lived token it holds.

Consequence: accepting for HA NOVA's user on the App accepts for every App with Home Assistant access, with no supported way back. It grants those Apps nothing they can't already do themselves, but it is lasting and shared, so it is typed, asked on its own, and its text says exactly that.

## Design

The Relay is unchanged: it forwards, and its gate worker rates through the CLI's classifiers. All logic is in the CLI and the skills.

1. **Raw allowlist** (`cli/relay_raw_allow.go`) gains three WS writes, plan-only and typed (`cli/plan_raw_tier.go` `rawTypedWS`), each in one exact shape (`cli/marketplace_raw.go` `checkRawMarketplace`):

   | WS type | shape | typed why (`raw.why.*`, en + de) |
   |---|---|---|
   | `marketplace/warning/accept` | `type` only | `marketplace_warning`: HA NOVA's user, the Supervisor system user every App shares, no way to withdraw, what Marketplace code can do |
   | `marketplace/repository/install` | `type`, `repository` | `marketplace_install`: community code Home Assistant doesn't review, with full control |
   | `marketplace/repository/uninstall` | `type`, `repository` | `marketplace_remove`: what uses it stops working |

   No `version` and no `confirm_replace_built_in`: picking a version and replacing a built-in integration stay in the Home Assistant UI. Every other Marketplace write stays unlisted, so refused.
2. **Only the Supervisor user.** Accepting is refused unless HA NOVA's account (`auth/current_user` in `config/auth/list`) is system-generated and named `Supervisor`; read live before planning, before `ha-nova relay`, and at the gate (`rawAccessTarget` → `rawMarketplaceUser`). A failed read sends nothing. A standalone Relay keeps the 0.30 refusal and its way out (sign in as that account and accept it in Settings > Marketplace).
3. **A plan of its own.** A raw plan that holds the acceptance and anything else is refused (`PLAN_INVALID`), so the lasting, shared effect is never approved as a side step of one install. The why fits the phone's 300-rune reason in both languages, so every surface shows it whole.
4. **Gate.** No new path: `gateVerdict` rates raw WS through `rawItemTier` and the live access-target check, so the three writes are typed at the gate (also inside a `collect_events` envelope), and the acceptance for a person's account is refused there. `ha-nova relay ws` refuses all three, naming the plan. `update.install` on a Marketplace entity (also from a script) and `homeassistant.restart` stay typed.
5. **Words.** Plan lines and phone titles name the repository through `marketplace/repositories/list` (`cli/plan_raw_names.go`), via `sayText` only; the acceptance line carries no name. All texts come from `cli/confirm_strings.{en,de}.json`.
6. **update.install preflight** stays. Its `say` offers the acceptance (`say.marketplace_accept`) only when `marketplace/info` says `warning_accepted: false` and HA NOVA is the Supervisor user; a failed read, a missing flag, or a person's account keep the Home Assistant way out (`say.marketplace_update`).
7. **Skills.** `hacs` → Marketplace mode: the lifecycle as plan items, the acceptance asked first and on its own, read back after apply, a restart for an integration as its own typed step. `updates`: a Marketplace update installs via `update.install`; on `MARKETPLACE_WARNING` it relays `say` and hands the acceptance to `hacs` only when `say` offers it. No word budget raised.

## Not in scope

Custom repositories, version pinning and prereleases, GitHub connect, critical notices, replacing a built-in integration, accepting for a standalone Relay's account: these stay in Home Assistant's UI. A way to withdraw the acceptance: Home Assistant has none.

## Reviews

- KISS shaping (gpt-6.1-terra): drop `version` from the install (done), offer the acceptance only on a known `false` (done), reuse the raw planner and tier machinery (done), keep the warning text short enough for the phone (done).
- Astra (gpt-6-astra, medium): PARTLY → addressed. Shared-user facts verified. Its P2s: the acceptance as a plan of its own (enforced in code), standalone scope (restricted to the Supervisor user, the maintainer's literal decision), "for good" reworded to "no way to withdraw", the invariant narrowed to acceptance, install, uninstall, and restart (`update.skip` and `update.clear_skipped` unchanged).

## Proof

- Go: `cli/marketplace_raw_test.go` (tier, words, Supervisor-only, own plan, shapes, gate verdicts, restart and Marketplace update typed, also from a script), `cli/marketplace_update_test.go` (the offer only on a known `false` for the Supervisor), `cli/marketplace_test.go` (`ha-nova relay` refuses the lifecycle writes).
- Bed (throwaway Home Assistant, standalone Relay, offline): the real `marketplace/info` and account reads; the acceptance refused for the bed's person account; the bed's own admin token sends `marketplace/warning/accept` and `marketplace/info` flips, which catches a later rename upstream; install and uninstall plan typed with the repository named; the gate rates them typed. The App's Supervisor user can't be shown on the bed (no Supervisor); that path rests on the sources above and the Go tests.
