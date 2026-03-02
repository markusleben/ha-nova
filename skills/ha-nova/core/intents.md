# HA NOVA Intent Matrix (Canonical)

This is the single source of truth for intent routing in HA NOVA.

For each intent, define:
- `required_companions[]`: additional skills that must be loaded for this intent.
- `modules[]`: lazy-loaded modules for this intent.

## Intent Contract

- `automation.create`
  - `required_companions[]`:
    - `"$NOVA_REPO_ROOT/skills/ha-automation-best-practices.md"`
    - `"$NOVA_REPO_ROOT/skills/ha-safety.md"`
  - `modules[]`:
    - `"$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/resolve.md"`
    - `"$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/create-update.md"`

- `automation.update`
  - `required_companions[]`:
    - `"$NOVA_REPO_ROOT/skills/ha-automation-best-practices.md"`
    - `"$NOVA_REPO_ROOT/skills/ha-safety.md"`
  - `modules[]`:
    - `"$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/resolve.md"`
    - `"$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/create-update.md"`

- `automation.delete`
  - `required_companions[]`:
    - `"$NOVA_REPO_ROOT/skills/ha-safety.md"`
  - `modules[]`:
    - `"$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/resolve.md"`
    - `"$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/delete.md"`

- `automation.read`
  - `required_companions[]`: none
  - `modules[]`:
    - `"$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/resolve.md"`
    - `"$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/read.md"`

- `automation.list`
  - `required_companions[]`: none
  - `modules[]`:
    - `"$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/read.md"`

- `script.create`
  - `required_companions[]`:
    - `"$NOVA_REPO_ROOT/skills/ha-safety.md"`
  - `modules[]`:
    - `"$NOVA_REPO_ROOT/skills/ha-nova/modules/script/resolve.md"`
    - `"$NOVA_REPO_ROOT/skills/ha-nova/modules/script/create-update.md"`

- `script.update`
  - `required_companions[]`:
    - `"$NOVA_REPO_ROOT/skills/ha-safety.md"`
  - `modules[]`:
    - `"$NOVA_REPO_ROOT/skills/ha-nova/modules/script/resolve.md"`
    - `"$NOVA_REPO_ROOT/skills/ha-nova/modules/script/create-update.md"`

- `script.delete`
  - `required_companions[]`:
    - `"$NOVA_REPO_ROOT/skills/ha-safety.md"`
  - `modules[]`:
    - `"$NOVA_REPO_ROOT/skills/ha-nova/modules/script/resolve.md"`
    - `"$NOVA_REPO_ROOT/skills/ha-nova/modules/script/delete.md"`

- `script.read`
  - `required_companions[]`: none
  - `modules[]`:
    - `"$NOVA_REPO_ROOT/skills/ha-nova/modules/script/resolve.md"`
    - `"$NOVA_REPO_ROOT/skills/ha-nova/modules/script/read.md"`

- `script.list`
  - `required_companions[]`: none
  - `modules[]`:
    - `"$NOVA_REPO_ROOT/skills/ha-nova/modules/script/read.md"`
