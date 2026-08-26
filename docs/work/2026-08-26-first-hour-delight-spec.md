# First-Hour & Delight Pack — Spec (#528)

Status: active

Minimal spec for the 9 P3 items. All skills-only; no relay change. Target
files per item; contract pins land in `tests/skills/first-hour-contract.test.ts`.

| Item | What | Files |
|---|---|---|
| P3-01 Capability answer | Dispatch row + "Capability Answer" section grouped by everyday jobs, grounded via one registry aggregate | `skills/ha-nova/SKILL.md` (dispatch), `skills/ha-nova/capability-answer.md` (new, shared — no word budget) |
| P3-02 Starter proposals | Bounded inventory → evidence-gated pattern table → max 5 Suggestion-Block items → each accepted hands to write individually | `skills/ha-nova/starter-proposals.md` (new), dispatch row |
| P3-03 Safety story | 5-line on-request block rendering the enforced guarantees user-facing | `skills/ha-nova/capability-answer.md` (same surface), dispatch example |
| P3-04 Explain mode | "explain this automation" route: read renders the behavior narrative instead of force-dumping YAML | `skills/read/SKILL.md` Output Format (budgeted — compact), dispatch example |
| P3-05 Home overview | Aggregate-counts recipe (List-Frame-legal), feeds P3-01 | `skills/ha-nova/capability-answer.md` |
| P3-06 Replication closing line | ONE evidence-gated closing line after a verified create + replicate-across-rooms pattern + dispatch example | `skills/ha-nova/output-rules.md` (named exception), `skills/write/SKILL.md` (budgeted), `skills/ha-nova/automation-patterns.md` (pattern) |
| P3-07 Structure check | Read-only audit (no-area count, integration-default names) + legal Next-step invitation; fixes hand to organize | `skills/ha-nova/capability-answer.md` (organize had zero budget headroom) |
| P3-08 Session recap honesty | Answer from this conversation's verified writes only; name the coverage limit | `skills/ha-nova/output-rules.md` |
| P3-09 Suggestion budget fuel | Candidates for scene/dashboard + notify-on-failure in the catalog | `skills/scene/SKILL.md`, `skills/dashboard/SKILL.md`, `skills/write/SKILL.md` or shared catalog (budgeted — ~2 lines each) |

Precedent to copy: energy:42 (unconfigured → offer initial setup).
Constraints: 100% English skill files; budgeted sub-skills need simultaneous
compression; every new promise gets a contract pin; post-write suggestion cap
stays 2 — P3-06 is a NARROW named exception, not a loosening.
