## Hermes `skill_view` Mismatch Spec

Date: 2026-04-21
Status: implemented

### Problem

Hermes shows HA NOVA sub-skills in `skills_list(category="ha-nova")`, but `skill_view("<listed-name>")` fails for names such as `ha-nova-history` and `ha-nova-review`.

### Root Cause

Before the fix, HA NOVA installed Hermes sub-skills under bare directory names like:

- `~/.hermes/skills/ha-nova/history/SKILL.md`
- `~/.hermes/skills/ha-nova/review/SKILL.md`

but rewrites the frontmatter names to:

- `name: ha-nova-history`
- `name: ha-nova-review`

This creates two competing identifiers for the same installed Hermes skill:

- directory name: `history`
- frontmatter name: `ha-nova-history`

Hermes discovery and view lookup did not consistently tolerate that mismatch.

### Goal

Make Hermes install and status logic use one canonical installed identifier per sub-skill so list and view agree.

### Implementation

1. Hermes sub-skills now install into directories whose names match their rewritten frontmatter names.
2. The context skill stays stable as `ha-nova`.
3. Hermes-facing markdown keeps the `ha-nova-*` dispatch references.
4. Go installer, shell installer, tests, and docs now match the canonical Hermes layout.

### Exit Criteria

- Hermes sub-skills install under `~/.hermes/skills/ha-nova/ha-nova-*/`.
- Installed Hermes sub-skill directories match their `name:` frontmatter.
- `ha-nova setup hermes` and `install-local-skills.sh hermes` follow the same layout.
- Targeted Go and Vitest coverage passes for the Hermes install contract.
