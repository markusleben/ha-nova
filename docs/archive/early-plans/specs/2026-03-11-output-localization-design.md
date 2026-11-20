# Design: Output Localization & UX Polish

**Date:** 2026-03-11
**Status:** Approved

## Problem

All ha-nova skill output uses hardcoded English section headers (CONFIG_FINDINGS, COLLISION_SCAN, SUGGESTIONS), English severity labels (CRITICAL, HIGH, MEDIUM, LOW), and internal check codes (R-01, S-01, M-03) that are meaningless to end users. The output feels like a machine template, not a polished product. UX is king — output should feel localized and human.

## Solution

1. **Central localization rule in Context Skill** — one instruction that all skills inherit
2. **3 severity levels with emoji** — 🔴 🟠 🟡 (replacing 4 text-only levels)
3. **Descriptive titles instead of codes** — "Template-Fallback fehlt" not "R-01"
4. **Localized section headers** — LLM adapts to user's language naturally
5. **Consistent across all skills** — review, write (post-write review), helper (post-write review)

## Affected Skills

### Full update (structured reports with severity):
- `skills/review/SKILL.md` — Output Format section (7 sections)
- `skills/write/SKILL.md` — Post-Write Review format (Phase 4)
- `skills/helper/SKILL.md` — Post-write review format

### Central rule:
- `skills/ha-nova/SKILL.md` — New "Output Localization" section in Context Skill

### No changes needed:
- `read`, `service-call`, `guide`, `entity-discovery`, `onboarding` — no structured report sections with severity/codes

## Design Details

### Context Skill: New Section "Output Localization"

Add after "Response Format" section:

```
## Output Localization (Critical)

All user-facing output MUST be localized to the user's language:
- Section headings: translate naturally (not literal — idiomatic)
- Severity: 3 levels only — 🔴 (high/critical) 🟠 (medium) 🟡 (low/info)
- Finding titles: use short descriptive title (2-5 words) explaining WHAT the issue is
- Internal check codes (R-01, S-01, H-01, etc.) are for YOUR analysis reference only — NEVER show them to the user
- Keep output structure familiar: same sections, same order, every time
```

### Review Skill: Output Format Rewrite

Replace hardcoded English keys with descriptive, localizable section concepts:

| Current (hardcoded) | New (concept for LLM to localize) |
|---|---|
| `REVIEW_MODE:` | Review target (what was reviewed) |
| `CONFIG_FINDINGS:` | Findings (numbered, with 🔴🟠🟡 + descriptive title) |
| `COLLISION_SCAN:` | Collision check (entity names + short result) |
| `CONFLICTS:` | Conflicts (or "none") |
| `SUGGESTIONS:` | Suggestions |
| `SUMMARY:` | Summary |
| `QUICK_FIX:` | Instant help (or "not needed") |

Finding format change:
- Before: `[HIGH] R-01: float without default — fix: ...`
- After: `🔴 Template-Fallback fehlt — | float | default(21) gibt 0°C zurück... Fix: | float(21)`

### Write & Helper Skills: Post-Write Review Format

Same principles applied to the Post-Write Review block:
- Before: `**Config Findings:** {CRITICAL/HIGH findings...}`
- After: Localized header + 🔴🟠🟡 severity + descriptive titles, no codes

## What does NOT change

- Check catalog in review skill (Step 1) — codes stay as internal LLM reference
- Skill dispatch logic
- Confirmation tiers (natural vs token)
- Any functional behavior
