# Issue #274 — Write/Review Preview & Verification Quality

Status: active
Issue: #274 (12 observed problems). Delivered as two PRs — different subsystems, one logical topic:

## PR A — `ha-nova diff`: aligned per-item rendering (problems 1–3 root cause)

Today `diffArrays` collapses any length change to `Actions: N → M items` ("do not
flood"). That single line can misrepresent a change (nesting actions into one
`if` block *reduces* the count) and makes the confirmed preview incomprehensible.

Change (deterministic, no LLM involvement — the diff stays a stable artifact):
- Keep the count line as the group header.
- Align common prefix/suffix (via `valuesEqual`), render the middle window as
  per-item `added (…)` / `removed (was …)` lines using a compact one-line item
  summary (`alias` > `action`/`service` > trigger `platform`/`trigger` >
  `condition` > `delay`/`wait_*` > block types `if`/`choose`/`repeat`/`parallel`
  with nested action counts > compact JSON fallback).
- Cap at 8 rendered items per side, then one honest `… and N more` line —
  no silent truncation.
- Notification-copy recursion (`diff_notification.go`) unchanged.
- Pure reorders show as symmetric removed+added summaries — accepted (KISS; no
  LCS matching).

## PR B — skill texts: narrative, honesty, semantic checks (problems 2–12)

1. **Behavior narrative (write-safety Pre-Write Diff):** the preview-summary
   sentence must state the behavioral effect in plain language; when the diff
   still contains a count-only/`… and N more` line, the summary MUST name what
   was added/removed/nested. The narrative is agent-authored interpretation and
   lives in the summary slot — the Changes slot stays verbatim CLI output
   (resolves the problem-3 tension without giving up determinism).
2. **Verification honesty (write SKILL Phase 4 + write-safety):** ban the bare
   word "verified"; the post-write result names what was proven (saved,
   read-back matched, reloaded, entity present) and states that behavior was
   not exercised; offer an optional consent-gated behavior test (manual
   trigger via `ha-nova:service-call`) where meaningful. Covers problems 5, 9.
3. **Semantic-narrowness review check (review/checks.md):** exact-state
   equality against open state sets (person/device_tracker zone states,
   media_player, vacuum, climate…) that is narrower than the stated intent —
   e.g. `state == "home"` vs "when nobody is home" (`!= "home"` matches zone
   states too). New check, also referenced by write pre-check. Covers 4, 6.
4. **Timing advisory:** fixed `delay` standing in for completion of an
   asynchronous operation (child script start, device action) → advise
   `wait_for_trigger`/`wait_template`. Covers 10.
5. **Startup advisory:** `homeassistant` start trigger + immediate state
   checks on integration entities → advise availability guard. Covers 11.
6. **Multi-target logical changes (write SKILL/write-safety):** when one
   logical change spans multiple automations/scripts: present a change plan
   (all targets, combined behavior, apply order) BEFORE the first per-target
   preview; state revert coverage honestly (N=1 — only the last update stays
   auto-revertible) and offer a safety backup proportionally; per-target flow
   after plan confirmation; combined final summary. Covers 7, 8 (honesty), 12.

## Out of scope (documented follow-ups)

- Multi-snapshot revert stack (N>1) — CLI storage change; would fully close
  problem 8. Candidate follow-up issue.
- Automated behavioral testing — actuates physical devices; stays a
  consent-gated manual offer.
