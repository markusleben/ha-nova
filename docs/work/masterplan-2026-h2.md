# HA NOVA Masterplan 2026-H2

Status: `active`
Scope: program-level plan following the completed [masterplan-2026.md](masterplan-2026.md) (shipped as v0.14.0, since grown to 0.17.0). Each wave breaks down into its own short spec + PRs before implementation; this doc is the SSOT for sequencing and decisions until superseded.

## Context

Seven parallel analyses in July 2026 (end-user UX journey, skill coverage, relay/runtime robustness, product ideation, plus four deep audits across all 28 skills) mapped what remains after the 2026 program. Verdict: the daily-use core (preview → confirm → write → verify → audit → test offer → revert) is the polished, differentiated center of the product. The remaining gaps cluster into six themes:

1. **Safety seams**: `service-call`'s "call ANY service" scope bypasses the stricter gates of owning skills; physically irreversible runtime actions (`lock.unlock`, `alarm_disarm`) sit at the lightest confirmation tier while a retained MQTT publish requires a typed token; the safety matrix omits five skills entirely.
2. **Verification correctness**: the "checks its work" promise itself misfires — immediate post-call re-reads false-alarm on transitioning devices (covers, light fades, climate); several statistics/read paths can produce silently wrong numbers.
3. **Recovery**: full HA Backups (GB-scale, system-level) are the only recovery path for 6+ config families; deletes have no revert; the undo stack is client-local, 5 entries, updates only.
4. **Audit breadth**: the README's "deeper audits are rolling out" promise — checks.md covers only automations/scripts/storage-helpers; scenes, dashboards, template sensors, and cross-item references have no rule catalog.
5. **Coverage**: no skill can add an integration (config flows), write calendar events, or fire events/webhooks.
6. **Onboarding**: the two-token model (relay token + LLAT) is the single largest friction for the non-terminal audience; the wizard's own workarounds (token re-display, disclaimers) are the symptom.

**Maintainer decisions (2026-07-14):** all four analysis dimensions in scope; quick wins separated from bigger bets; committed flagship bet = Pairing Code + Home Base; Config Snapshots as a core workstream; Wave 0 implemented immediately. Non-committed ideas live in the ranked backlog.

---

## Wave 0 — Safety seams (skill/docs-only, immediate)

1. **Close the service-call gate bypass**: deferral table in `skills/service-call/SKILL.md` — `mqtt.publish` → mqtt, `update.install` → updates, `camera.*` → camera, `media_player.*`/`tts.*` → media, `persistent_notification.*` → notify, `logger.set_level` → diagnose — plus dispatch sharpening in `skills/ha-nova/SKILL.md`. A literal reading of "call any service" must no longer execute a retained publish or a core update with weaker gates than the owning skill.
2. **New confirmation rung for high-consequence runtime actions**: `lock.unlock`, `alarm_control_panel.alarm_disarm`, garage/gate covers, `homeassistant.stop` currently run at plain natural confirmation (tier contract `skills/ha-nova/SKILL.md`; `service-call` escalates only `homeassistant.restart`) while retained MQTT requires a typed token. Add an explicit escalation tier for physically irreversible / security-relevant runtime actions (pattern: high-consequence gating in `skills/ha-nova/test-run.md`); name the acoustic blast radius for night TTS/announce in `media`.
3. **Complete the safety matrix**: `admin`, `assist`, `yaml-config`, `mqtt`, `backup` are missing from `write-safety.md` § "Safety-Mechanism Availability by Skill"; `admin` states no in-skill recovery path for zone/person/user deletes. Add rows + in-skill recovery notes.
4. **Backup-gate consistency**: proactive backup offer before irreversible deletes in `dashboard`, `scene`, `organize`, `admin` (pattern from `maintenance`/`updates`). Wording chosen so Wave 2 (Config Snapshots) can replace the offer with auto-snapshots for config-level items without rewriting the gate.
5. **Literal-label drift**: `helper`/`read` Output Format sections still carry hard-coded English bold labels; convert to semantic slots per `skills/ha-nova/output-rules.md`.
6. **Fallback catalog completion**: add rows for integration onboarding (config flows), firing events/webhooks, alarm/lock code management; give the blueprint Relay-Ready row a concrete payload example (`blueprint/list`/`import`).
7. **Doc drift**: stale "Relay 0.3.0" guidance in `skills/ha-nova/relay-api.md`; stale Phase-1 skill list in `PROJECT.md`; inconsistent skill counts; `.codex/ONBOARDING.md` stub. Plus a contract test pinning version claims in skill docs against `version.json` so this class of drift cannot recur.

Shipping: 3 PRs (one topic, one branch) — (a) service-call deferrals + confirmation tier + safety matrix, (b) backup gates + labels + fallback catalog, (c) doc drift + version-claim contract test. Full PR merge checklist each; no release (batching rule — collects in the next release-body draft).

---

## Wave 1 — Skill intelligence hardening (skill-only)

Concrete missing checks surfaced by the deep audits, bundled into 3 thematic PRs.

### 1a Verification correctness (the "checks its work" promise itself)
- **Settle window**: immediate post-call re-read false-alarms on transitions (cover `opening`, light `transition` fades, climate ramp, media buffering) — `service-call`, `media`. Add transition awareness / bounded settle-and-re-read guidance.
- **Stateless carve-out**: `button.press`, `scene.turn_on`, `script.*` do not reflect the call in their own state — differentiate verify expectations per domain instead of reporting false discrepancies.
- **`supported_features` gate in service-call** (media already models this).
- **Area/device-target verify**: with area targets, verification silently degrades to a single entity read; verify the expanded set (bounded).
- **`mqtt.publish` gets a verify step**: read the resulting HA entity state after command-topic publishes — the one world-changing MQTT action currently fire-and-forget.
- **Drift check before apply**: nothing re-reads the target between preview and write anywhere; most acute for the dashboard full-document overwrite, and scene explicitly does "last writer wins". Lightweight re-read + abort-on-foreign-change in write/dashboard/scene/helper.

### 1b Missing consequence checks before destructive ops
- `organize`: area delete / entity rename (`new_entity_id`) / disable have no automation-reference check — the most severe single finding; add a `search/related` gate like admin already has for zones.
- `yaml-config`: whole-file replace verifies only the target entity, not survival of sibling sensors in the same file.
- `assist`: risk-weight exposure (exposing lock/alarm/garage grants voice control to anyone in earshot); bulk exposure gets a before/after diff + verification via `expose_entity/list`.
- `maintenance`: quantify purge impact up front (bounded pre-cutoff history read); expand glob/domain selectors to the concrete entity set before confirmation.
- `updates`: surface breaking changes for integrations/Apps too (today core/OS only); check version prerequisites before core updates.
- `todo`: consumer check before list deletion. `backup`: refuse (not just warn) deleting the only/newest backup — it is the recovery net every other skill leans on.
- `helper`: promote H-01-class checks to pre-write (today post-write only); add cross-field constraint validation (`initial` ∈ `options`, min < max, `has_date`/`has_time`).
- `write`: give scripts a best-practice baseline (`bp_status` is automation-only today) — DEFERRED out of Wave 1b: the snapshot cache (`automation-bp-snapshot.json`) is CLI-managed machinery that would need a script corpus; needs its own decision before implementation.

### 1c Read-skill result quality
- `history`: sum-vs-mean trap for `total_increasing` statistics (energy!), statistics unit-mismatch awareness, `statistic_id` ≠ `entity_id`, DST/timezone note.
- `health`: deduplicate repairs by integration + `translation_key`; define the `attention` threshold (a healthy 2000-entity home always has a few unavailable sensors); link cause↔symptom (setup-error integration → its unavailable entity cluster).
- `entity-discovery`: diacritic folding (Küche/Kueche — the German-speaking audience is core), a match-confidence model, honest handling of the 20-result cap.
- `calendar`: exclusive all-day end dates; timezone presentation.
- `media`: make browse bounding real ("page through them" has no mechanism — cap depth/children and ask to narrow); preview the true acoustic blast radius for grouped TTS targets.
- `diagnose`: enforce trace↔log timestamp correlation before claiming causation.
- `external-sources`: unreachable/timeout/DNS error path (the most likely real failure).

---

## Wave 2 — Config Snapshots: targeted, lightweight backups (Relay 0.5.0, committed)

Already specced as "Phase 2" in `docs/reference/bridge-architecture.md` (`POST /backups`), never built. Now grounded in an audited coverage matrix.

**Problem:** full HA Backups (GB, system-level restore) are the only recovery path for 6+ families; deletes have no revert; the undo stack (`cli/snapshot.go`) is client-local, capped at 5, updates only.

**User experience:**
- *"Snapshot my automations before we clean up"* → named, targeted snapshot (KB-sized, seconds).
- **Auto-snapshot before destructive ops** — deletes become recoverable for the first time.
- *"Restore the kitchen automation from yesterday"* → selective per-item restore through the normal diff-preview → confirm → write → verify path.
- *"What snapshots do I have?"* → list with name, age, contents; auto-prune (default 30 days / 100 files, named snapshots exempt).

**Audited coverage matrix (defines what the feature honestly promises):**
- **FULL (identity-preserving restore):** automations, scripts, scenes (upsert by `unique_id`/id), dashboard *content* (`lovelace/config/save` by `url_path` — the full-document overwrite is the single most valuable capture point), energy prefs (id-stable whole-doc; the flow already holds the pre-read), YAML files (path-stable; also fixes the one-deep `.bak` clobber and the unprotected `delete_file`), tags (physical `tag_id` round-trips), assist exposure map, entity/device registry metadata (in-place).
- **PARTIAL (item restores, identity/graph does not):** storage-helper delete (recreate mints a new id → inbound references break), Lovelace resources, persons, zones, pipelines, to-do lists (items yes, entity new), dashboard delete.
- **NOT covered (excluded honestly):** config-entry helpers (flow-based, new `entry_id`), areas/floors/labels/categories (id change breaks the reference graph — the Wave-1b consequence checks are the real protection there), users (credentials/tokens unrecoverable — a snapshot would give false confidence), recorder data (purges are true data deletion), update installs, backup archives.
- **Design rules:** a snapshot restores the *item*, never the *reference graph* — the restore preview names orphaned references; restore prefers in-place writes, never delete+recreate; per-family honesty labels in the output. Naming: avoid "config snapshot" wording collisions with the bulk-review prohibition (`skills/review/SKILL.md`).

**Architecture (charter-clean):**
- **Relay**: `POST /backups` = generic blob store (save/load/list/prune/delete, gzip, size/count caps; pattern `files.ts`; zero HA semantics). Storage in the App data directory → survives client machines and is swept into full HA Backups automatically.
- **Skills** carry all intelligence: what to capture, diff, restore rules, identity honesty.
- **CLI**: `ha-nova relay backups` passthrough (pattern: `files`).
- Short spec in `docs/work/` before implementation: snapshot format, category layout, restore identity rules, caps, relation to the existing update-revert stack (which stays as the quick-undo layer).

---

## Wave 3 — Review expansion: deliver "deeper audits rolling out" (skill-only)

The README already promises it; `skills/review/checks.md` covers only automations/scripts/storage-helpers. New rule families (candidates from the audit, IDs in checks.md style):

- **SC-01…07 scenes**: dead entity refs, mixed color attributes, group capture, read-only domains captured, orphaned scenes.
- **D-01…07 dashboards**: broken card entity refs, missing custom-card resource (the `lovelace/config` + `resources` join already exists in the skill), duplicate view paths, orphaned resources.
- **TS-01…07 template/REST/command-line sensors** (yaml-config): entity typos, `float` without `default`, missing `availability`, aggressive `scan_interval`, secret leakage, unit/state_class mismatches.
- **HX-01…05 cross-item**: automation references a deleted scene/script/helper/area/zone/person — the first true graph-consistency audit; dashboard → scene/script refs.
- **H-11…15, F-09**: dead config-entry helper sources, dead group members, orphaned scripts.
- **Test-offer expansion**: the `test-run.md` card is consumed only by `write` today. Extend to the natural "run it once" actions: scene apply-test, notify send-test, assist utterance re-test after pipeline/exposure changes.

---

## Wave 4 — Coverage gaps (skill-only)

- **NEW `integration-setup` skill**: add/re-auth integrations via `config_entries/flow` (reuse the proven flow-loop machinery from `helper` Family 2). The largest remaining domain gap — exactly the moment non-technical users need help.
- **Calendar writes**: `calendar.create_event`/update/delete with preview/confirm (calendar is read-only today).
- **Events/webhooks**: decide ownership (`service-call` vs `fallback`) + payload examples for `POST /api/events/{type}` and `/api/webhook/{id}`.
- **Alarm/lock guidance** in `service-call`: arm modes, code handling (codes never in chat) — builds on the Wave-0 high-consequence tier.

---

## Wave 5 — Relay diagnosability & robustness (bundled with Wave 2 into Relay 0.5.0)

- `/health`: WS-disconnect reason (auth vs network), `file_access` mode, HA version — so the onboarding skill can classify failures instead of prematurely sending users into `ha-nova setup`.
- Wire up `LOG_LEVEL` (validated but never consumed today); log the cause on the 500 path (`nova/src/http/errors.ts` swallows it); log 401s; fix the `constantTimeEqual` length leak (`nova/src/security/auth.ts`); server request/headers timeouts; make the 256 MiB response ceiling configurable for constrained hosts.
- Test holes: the 413 path, concurrency/reconnect integration.

---

## Wave 6 — Onboarding flagship: Pairing Code + Home Base (Relay 0.6.0, committed)

- **`/pair` endpoint**: a 6-digit short-lived pairing code (visible in the App log/panel) exchanges for the relay token over LAN. The wizard asks for exactly one thing: the code. The two-token model disappears from the user's world (the relay token becomes invisible; the LLAT stays in the App's own config UI where it belongs). Charter-clean: pure auth handshake, rate-limited, zero HA semantics. Natural future home for token rotation.
- **Home Base**: a minimal ingress status page in the HA sidebar (one HTML file rendering `/health` data): connection status, version floor, pairing code, install one-liner. When something breaks, non-terminal users look *here* — inside the UI they know — instead of running `ha-nova doctor`. A charter test pins that no HA business logic creeps in.
- Flanking CLI work: multi-instance discovery pick list (`cli/setup_discovery.go` already collects candidates, shows one), token-revoked recovery deep link (`haProfileSecurityURL`), non-TTY install guidance, Windows prerequisite preflight.

---

## Wave 7 — Update UX parity

- "Start a new session" nudge after updates (today an easily-missed printed instruction); update-notice parity for non-Claude clients; embed the install one-liner directly in the README (release-prep PR — README gate).

---

## Backlog (ranked, not committed)

From the ideation pass; each needs its own decision before entering a wave:
1. **Sentinel** — "watch this for me" compiled into labeled HA automations (proactive capability without event streaming; pure skill).
2. **Move-In Interview** — guided first conversation after setup (rename cryptic entities, surface repairs, first automation).
3. **Household Ledger** — every AI change visible inside HA (logbook entry per verified write) for non-operator family members.
4. **Home Handbook** — living plain-language documentation of how the house works, as a dashboard.
5. **Voice Trainer** — test → fix → verify loop for Assist utterances.
6. **Blueprint Bridge** — import community blueprints conversationally; generalize own automations into shareable blueprints.
7. **Morning Briefing** — scheduled headless agent run delivering a weekly home report as an HA notification (needs a CLI scheduler decision).
8. **Flight Recorder** — record expectations in automation descriptions, compare against traces over time ("your porch light hasn't fired in 12 days despite 12 sunsets").
9. **Two-Key Actions** — out-of-band phone confirmation for locks/alarm (needs request-tunable envelope timeout).
10. **Time Machine Review** — "this would have fired 11 times last week, including 3 AM Tuesday" counterfactual previews (approximation honesty required).
11. **Community-skills certification** — `ha-nova skill add <repo>` with the existing linter as a machine-verified trust gate (largest item; needs the CLI to port linter checks).

## Deliberately NOT
- SSE streaming as a headline feature (Sentinel + bounded windows cover the user-visible value; streaming invites relay session state).
- HACS management parity (checklist-chasing; concierge-style coverage is higher leverage).
- A hosted skills registry (charter: no cloud).
- Snapshots for recorder data or user credentials (false confidence).

---

## Release sequencing

All waves land on `main` first; releases batch per the release-worthiness rule. Relay changes bundle into exactly two releases: **0.5.0** (`/backups` + diagnosability) and **0.6.0** (`/pair` + Home Base). Skill-only waves (0, 1, 3, 4) batch freely between them.

| Wave | Theme | Relay release | Status |
|------|-------|---------------|--------|
| 0 | Safety seams (3 PRs) | — | **DONE** — #340, #341, #342 |
| 1 | Skill intelligence hardening (3 PRs) | — | **DONE** — #344, #345, #346 |
| 2 | Config Snapshots | 0.5.0 | spec: `2026-07-14-config-snapshots-spec.md` |
| 3 | Review expansion (SC/D/TS/HX families, test-offer) | — | planned |
| 4 | Coverage (integration-setup, calendar writes, events, alarm/lock) | — | planned |
| 5 | Relay diagnosability | 0.5.0 (with Wave 2) | planned |
| 6 | Pairing Code + Home Base | 0.6.0 | planned (spec first) |
| 7 | Update UX parity | — | planned |

## Opinionated defaults (documented instead of asked)
- Waves 2 and 6 get short specs in `docs/work/` before implementation; the masterplan stays the sequencing SSOT.
- Wave ordering favors trust-compounding: harden what exists (0–1) before shipping new recovery machinery (2), before widening surface (3–4).
- The update-revert stack stays as the quick-undo layer; snapshots are the durable, named layer on top — no migration of the existing mechanism in this program.
- Backlog items enter a wave only through an explicit maintainer decision recorded here.

## Known risks
- Wave 1's settle-window and drift-check changes touch the hottest skill paths — each needs targeted live verification (`npm run dev:sync` + fresh session) before PR.
- The snapshot matrix's PARTIAL families need per-family honesty wording; overpromising restore fidelity would damage exactly the trust the feature is meant to build.
- `/pair` changes the auth surface — needs the same adversarial review depth as the original dual-token design and a rate-limit test.
- 29+ skills after Wave 4: dispatch sharpness pressure keeps rising; the linter and context-skill examples remain the backstop.
