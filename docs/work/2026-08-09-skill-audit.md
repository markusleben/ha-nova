# Skill Audit 2026-08 — six-charter deep-dive over all 31 skills

Status: active

Audited commit: `3d26b72` (main, v0.23.0). All `file:line` references are relative to that commit.

Method: six parallel charter agents — (C1) coverage & product gaps, (C2) external drift vs HA 2026.7/2026.8, (C3) internal consistency & routing, (C4) adversarial safety & untrusted data, (C5) verification & release integrity, (C6) agent effectiveness & UX — over a shared pre-pass (relay ground truth extracted from `nova/src`, upstream sources fetched once, inventory manifest, destructive-op inventory, shared severity rubric with a prior-art seed list). Every P1 was re-verified line-by-line by the orchestrator; no P0/P1 rests on inference. Live checks were read-only (11 whitelisted probes against a production HA 2026.8.0 / Relay 0.9.0 instance). Follow-up issues: #513–#522.

## Executive summary

The skill layer is in strong shape: the confirmation-tier model is structurally consistent across direct operations, recovery texts never over-claim, prompt-injection attack traces show the preview-binding + typed-code design holds, external drift after 2026.8 is limited to P2/P3, and terminology/link hygiene is clean. The audit found **no P0 and two P1 defects**, both in the same file: `service-call` lets indirect actuation (`scene.turn_on` of a lock-bearing scene) and un-deferred services (`recorder.purge`) bypass the stricter gates their owning skills enforce (#513). The two biggest systemic results: (1) several reference docs misdescribe the shipped relay contract (a documented WS batch mode the relay rejects, a claimed `/files` whitelist that does not exist in code, an "SSOT" missing two endpoint families) — fixed in the quick-win PRs; (2) the product under-sells shipped capability — bounded event capture, Supervisor/App lifecycle, dashboard strategies, calendar recurrence and media search are all reachable today but unsurfaced (#519, #520). One seeded belief was falsified during verification: released 0.23.0 **does** ship `skills/hacs`; the "dead hand-off" observation was session-roster staleness, not a packaging defect.

## Priority matrix — broken (defect / drift / inconsistency)

| ID | P | Finding (one line) | Fix locus | Route |
|---|---|---|---|---|
| C4-01 | P1 | `scene.turn_on`/`automation.trigger`/`script.*` actuate locks/alarms/garage under natural confirmation — no member expansion (`service-call:52,:100,:222` vs `scene:140`) | service-call | #513 |
| C3-01 | P1 | `recorder.purge` reachable via service-call at natural tier; maintenance demands typed code + quantification (`service-call:43-50` vs `maintenance:52-53`) | service-call | #513 |
| C3-02 | P2 | Deferral table also missing calendar/todo/backup/camera-power/conversation rows (+ scene row, + `hassio.*` self-amputation risk) | service-call | #513 |
| C4-03 | P2 | review Quick-Fix never restates the high-consequence check (`review:297,:309`) | review | #513 |
| C6-04 | P2 | mqtt states two tiers for command/`set` publishes (`mqtt:72-73` vs `:102`) | mqtt, ha-nova | #513 |
| C4-04 | P2 | energy `save_prefs` lacks the pre-write drift STOP its full-document peers mandate | energy, write-safety | #514 |
| C4-05 | P3 | yaml-config `write_file` same class, mitigated by `.bak`+snapshot | yaml-config | #514 |
| C5-02 | P2 | 4 `docs/reference/safety.md` rows cite tests that do not assert the guarantee (camera/notify/mqtt honesty, Claim-Evidence) | safety.md, tests | #515 |
| C3-03 | P2 | Integration entry lifecycle (reload/disable/options/delete) owned by nobody; no capability-map row | fallback, integration-setup | #516 |
| C3-06 | P2 | fallback "Blocked by: No SSE" contradicts the shipped bounded `collect_events` envelope | fallback | #516 |
| C3-04 | P2 | `bridge-architecture.md:154-169` documents a WS batch mode the relay rejects (`ws-proxy` throws on arrays) | bridge-architecture | PR A |
| C3-05 | P2 | `bridge-architecture.md:259` claims a `/files` write whitelist (`/config/ha_mcp/`) that does not exist in `files-paths.ts` | bridge-architecture | PR A |
| C3-09 | P2 | `ha-api-matrix.md` misses ~55 surfaces skills pin (incl. `search/related`, `backup/*`, person/zone/tag/auth) | ha-api-matrix | #517 |
| C3-10 | P2 | relay-api presents `collect_events` caps as example values; 1 MiB/256 MiB/8 MiB + files bounds absent from every skill-facing doc | relay-api | PR A |
| C2-01 | P2 | fallback device-detach text predates the 2026.8 single-config-entry model — detach now REMOVES the device | fallback | PR B |
| C2-02 | P2 | `media_source/search_media` (2026.8) missing from the media flow | media | #519 |
| C6-03 | P2 | history 30-day default (`:68`) vs 24-hour cap (`:110`) — same utterance, opposite outcomes | history | PR B |
| C6-05 | P2 | helper storage-family delete omits the skill's own MANDATORY post-write review + duplicate step numbering (`:144-157`) | helper | PR B |
| C3-07 | P2 | Trace ownership triangle (history→read, dispatch→diagnose, read silent) | history, read | #518 |
| C3-08 | P2 | health carries no reverse boundary vs diagnose or maintenance | health, maintenance | #518 |
| C4-06 | gap | `config/auth/create` at natural tier despite "most dangerous writes" (escalation decision) | admin | #513 |
| C2-03 | P3 | "Developer Tools" → "Tools" rename; single stale pointer (`best-practices.md:128`) | best-practices | PR B |
| C2-04 | P3 | matrix tense: legacy template sensors "end in 2026.6" — removal shipped | ha-api-matrix | PR A |
| C2-05 | P3 | energy `BatterySourceType.capacity` (2026.8) missing from the optional-field list | energy-reference | PR B |
| C2-06 | P3 | health system-health example path `.data.info` is fictional (live shape: `.data.<domain>.info`) | health | PR B |
| C3-11 | P3 | matrix filesystem section is pre-relay legacy (`ha_mcp` paths, "only from the HA host") | ha-api-matrix | PR A |
| C3-12 | P3 | LLAT-era wording residue (matrix header, `admin:58`) | ha-api-matrix, admin | PR A/B |
| C3-13 | P3 | organize misroutes zones/persons/tags (no target) and dead-loops device categories with fallback | organize, fallback | PR B/#516 |
| C3-14 | P3 | fallback "Apps — External" row hides that App updates are Covered (updates) | fallback | PR B |
| C3-15 | P3 | `render_template` is also envelope-permitted; contract text says "subscription commands" only | relay-api | PR A |
| C3-17 | P3 | Error Handling sections absent in write-capable dashboard + organize (write/helper covered via apply-agent; helper inline gap = C4-08) | dashboard, organize | #518 |
| C3-18 | P3 | `config/auth/create` payload shape named nowhere; `lovelace/config/save` config key never named | admin, dashboard | #518 |
| C3-21 | P3 | Creating a local calendar unowned (todo owns the exact parallel) | calendar, fallback | #516 |
| C3-22 | P3 | References-section convention split 4 vs 27 | 4 skills | #518 (cosmetic tail) |
| C4-07 | P3 | energy single-source removal at natural tier — deliberate but a reader-trap vs "delete = typed" | energy | #514 (note) |
| C4-08 | P3 | helper inline flow lacks created-but-verify-failed / options-abort notes | helper | #515 (pin) / #518 |
| C4-09 | P3 | `homeassistant.restart` labeled high-consequence in test-run, natural+warn in service-call | test-run, service-call | #513 (align) |
| C4-10 | low | diagnose interpolates a derived string into a jq regex (charset-restrict or escape) | diagnose | #518 |
| C6-09 | P3 | fallback's shared Safety boilerplate is self-referential inside fallback | fallback | PR B |
| C6-10 | P3 | health text-severity rule vs output-rules "no text labels" | output-rules or health | PR B |
| C6-11 | P3 | review ":389 8 sections every time" vs ":46 omit skipped sections" | review | PR B |
| C5-08 | P3 | safe-test allowlist guard accepts any-script references and only `*.test.ts` | tests/onboarding | #515 |

## Priority matrix — worth building (gap / friction / opportunity, value × effort)

| ID | V×E | Opportunity (one line) | Route |
|---|---|---|---|
| C1-01 | H×M | Apps/Supervisor lifecycle is relay-reachable today (`/api/hassio/*`, `hassio.*` services); External tier premise wrong; needs self-amputation block | #520 (+#513 row) |
| C1-02 | H×S-M | Bounded event capture (shipped envelope) as a product surface: button/remote event capture, Assist pipeline debug, notification quick-response | #520 |
| C1-03 | H×S/M | Dashboard generation dead-ends in an empty shell; strategy dashboards are an S-effort win; views increment 2 | #519/#520 |
| C5-05 | H×M | review-checks executable oracle; 41 labeled fixtures already exist as seed corpus | #521 |
| C5-03 | H×S | Six untested load-bearing gates (mqtt retained escalation, admin account guards, camera overwrite, dashboard pause-drift, yaml rollback, diagnose reset) | #515 |
| C5-06 | H×S | Drift-log discipline has zero automation (~15 lines in the release preflight) | #521 |
| C5-07 | H×M | Live-E2E next five: helper, service-call HC gate, scene, hacs, updates | #521 |
| C6-01/02/07/13 | H×S | Description vocabulary sweep (service-call device verbs, notify disambiguator, helper types, assist exclusion) | #518 |
| C6-06 | H×M | Write flow mandates ~3,270 main-thread lines vs ~2,100 policy floor; thread-tag references, saves ~520 lines/write | #521 |
| C1-04 | M-H×S-M | Integration lifecycle extension (options/reconfigure/reload/delete; LLM agent options) | #520 |
| C1-05 | M-H×S | Recurring calendar events via WS `calendar/event/create` (rrule) | #519 |
| C1-06 | M-H×M | Z2M/ZHA/Z-Wave read-only observability + bridge-topic recipes | #520 |
| C1-08 | M×S | Assist custom sentences via `/files` (verified writable) + reload + live test | #519 |
| C1-09 | M×S | Media search flow step (player + 2026.8 source search) | #519 |
| C1-07 | M×S | Matter/Thread capability-map row (absent entirely) | #516 |
| C1-10 | M×S-M | Statistics → chart artifact rendering (client-gated) | #520 |
| C1-11 | M×S-M | Cross-skill playbooks reference (briefing, vacation, onboarding, digest) | #519 |
| C1-12 | M×S | Camera before/after frame comparison (currently chilled by live-view ban) | #519 |
| C1-13 | L×S | Device-level diagnostics path + redaction note | #519 |
| C4-02 | H×M | No "treat retrieved HA content as data, never instructions" rule anywhere (defense-in-depth; gates hold structurally) | #513 (shared rule) |
| C3-16 | M×S | Dispatch examples missing for hacs/onboarding (subtlest boundary) | #518 |
| C3-19 | M×M | Size splits: review/helper/relay-api extraction; checks.md must NOT move (78 test-path refs) | #521 |
| C3-20 | M×S | Shared `entity-resolution.md` replacing near-verbatim prose in 8+ skills | #521 |
| C6-08/14 | M×S | fallback endpoint-type table + review verify-before-flag: load-bearing rules unreferenced from their flows | #518 |
| C6-15 | M×S | README 3-step header could carry the Container/Core qualifier the Get-Started box has | backlog |
| C2 handoff | M×M | Composite-split triage after 2026.8 registry change (`list_composite_splits` unused) | #520 |

## Corrections to prior beliefs (verification pass results)

- **Seed 9 falsified (C5-01).** Released 0.23.0 ships `skills/hacs`: the tag (`3d26b72`, 22:49) is three commits after the hacs merge (`f4a04ca`, 18:25); the published macOS bundle contains all 31 skills; the installed plugin cache contains hacs; the enumeration source is a directory scan (plugin.json has no skills field), so no manifest can omit a skill. The observed "hacs not loadable" was a session started before the cache update (session-start roster snapshot). Residue worth keeping: a dispatch-target-existence contract test (#515) and a user-facing note that skill rosters snapshot at session start (#518).
- **Seed 7 corrected (C5-07).** The promoted live suite covers dashboard (5 scenarios, incl. typed-code delete gates), organize (4) and history (2) — live coverage is broader than seeded, but all of it is manual; no workflow runs the Codex harnesses.
- **Seed 4 nuanced.** `subscribe_events`/`subscribe_trigger` are not flatly rejected: they are permitted inside a bounded `collect_events` envelope (`ws-proxy.ts:97-105`); only bare subscriptions and bare `render_template` are rejected. The matrix fix must state the envelope caveat, not just remove the rows.
- **Clean bills worth recording:** vacuum `battery_level` removal does not touch health (it reads `device_class: battery` sensors); `search/related` still has no `zone` item type on 2026.8 (`admin:38` holds); `/api/error_log` 404-on-HA-OS confirmed live; helper↔fallback↔matrix config-entry family lists are bit-for-bit consistent; batch/grouped capability matrices consistent; recovery-layer texts never over-claim; German-text sweep clean; no broken cross-references; emoji vocabulary clean; every menu flow has the numbered-list fallback.

## Charter coverage declarations (condensed)

- **C1:** 20 candidate areas verdicted (13 findings; groups, backup-restore posture, blueprints tiering, energy tariff/forecast audited clean). Dashboard screenshots vs ha-mcp: recommend explicit non-goal (needs a relay delta of size L).
- **C2:** 10 dev-blog posts + 2026.8 BI list + feature flags screened — 4 hits, rest clean passes; 11 live read-only probes logged; time-bound claims verified; drift-log entry appended in this PR (clears the carried 2026.7 dev-blog backlog and screens 2026.8).
- **C3:** line-level compare of relay ground truth vs relay-api/bridge-architecture/ha-api-matrix/PROJECT.md; all 50 capability-map rows checked (44 clean, 6 flagged); intent matrix over domains × 9 verbs (non-trivial cells listed in #516/#518); terminology/link sweeps clean.
- **C4:** every destructive/high-consequence op tier-verified (op→tier table: 2 P1-adjacent defects, 45+ verified ok; declared exceptions judged sound); recovery-layer map complete and honest; 3 injection attack traces run — gates hold structurally; shell/jq interpolation clean except one diagnose regex.
- **C5:** 41/41 safety.md rows triaged (14 valid at read-the-test depth, 4 broken citations, 23 relay-internal rows existence-checked as declared non-goal); 18 write-capable skills × top-3 rules matrixed; release timeline reconstructed end-to-end; allowlist diffed both directions (zero orphans today).
- **C6:** 30 routing utterances (14 German) simulated — 19 clean, 9 degraded, 2 gaps; 7 files depth-audited; 10 skills output-rules-checked (9 compliant); 4 clients parity-compared (progressive enhancement uniformly implemented); example-vs-prose sweep over all 31 SKILL.md (23 explicitly clean).

## Cross-checks

- Every fallback capability-map row: accounted — 50 rows at the audited commit (`fallback:44-93`): 44 clean, 6 flagged in findings (`:60`, `:76`, `:82`, `:84`, `:86`, `:90` → #516/PR B). Missing-row gaps (integration lifecycle, Matter/Thread, custom sentences, local calendar, device category) are #516 findings, not row counts.
- Drift-log open item ("2026.7 dev-blog NOT SCREENED"): cleared by the entry landing with this PR; next watch items recorded (registry shims harden 2027.8, device-tracker deprecations 2027.7, `unit_class` breaks 2026.11).
- Every pre-pass seed finding: mapped (seeds 1/4 → PR A + #517; 2/3 → PR B; 5 → PR A; 6 → #515; 7 → corrected above; 8 → drift-log entry; 9 → falsified; 10 → #521; 11 → #518; 12 → #513; 13 → #521; 14 → #522; 15 → this report's issue set).
- Every `docs/reference/` file appears in at least one charter's coverage (matrix/bridge/safety/skill-architecture/comparison/relay-container/update-guide/testing/ha-doc-gate/freshness-patterns/census/client-integration/demo-recording/documentation-governance/ha-template-reference/hermes-platform-validation — the last four only via scope screens: no findings, low exposure).

## Sequencing (successor of the 2026-08-03 backlog-sequencing pattern)

- **Phase 1 — safety:** #513 (P1 pair + tier sweep; contract-test pins from #515 land with it), then #514.
- **Phase 2 — truth & verification:** quick-win PRs A/B (landing alongside this report), #515 remainder, #516, #517, #518.
- **Phase 3 — capability & depth:** #519 (S-effort wins first), #520, #521, #522.
- A separate sequencing doc is created only when Phase-1 implementation starts (KISS).

## Quick-win PRs landing with this audit

- **PR A** `docs: relay contract truth sweep` — relay-api endpoints/caps/envelope clause; matrix wrong-row fixes; bridge-architecture batch-mode + files-whitelist corrections; PROJECT.md endpoint list; safety.md citation wording (the 4 broken rows stay open in #515 until pinned).
- **PR B** `fix(skills): stale cross-refs + capability-map one-liners` — fallback webhook contradiction + Apps carve-out + self-reference; media "once shipped"; device-detach rewording (C2-01); Tools rename; health example path; energy capacity field; organize admin target; admin LLAT wording; history cap scope; review section qualifier; helper delete numbering + review step; camera reference pointer; health/output-rules severity exemption.
- **PR C** `docs(work): archive shipped backlog doc + breadcrumbs` — housekeeping.
