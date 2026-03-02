# HA NOVA Discovery Map (Lazy Loading)

Load only what is needed for the detected intent.
Canonical intent mapping (companions + modules) is defined in:
- `"$NOVA_REPO_ROOT/skills/ha-nova/core/intents.md"`

This file is module-loading guidance only. If this file conflicts with `core/intents.md`, `core/intents.md` wins.

## Loading Rule

For resolved intent `X`:
1. Load `required_companions[]` from `core/intents.md`.
2. Load `modules[]` from `core/intents.md`.
3. Do not load any additional modules in normal path.

## Progressive Loading Rules

1. Load router + `core/contracts.md` first.
2. Resolve current intent (`automation|script` + `create|update|delete|read|list`).
3. Load only `required_companions[]` + `modules[]` from `core/intents.md`.
4. Load diagnostics guidance only when a real failure occurs.
5. Do not preload all modules in normal flow.
