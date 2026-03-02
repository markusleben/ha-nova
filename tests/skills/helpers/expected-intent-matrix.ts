import type { IntentDefinition } from "../../../src/skills/contracts/intent-dispatcher.js";

export const expectedIntentMatrix = new Map<string, IntentDefinition>([
  [
    "automation.create",
    {
      companions: [
        "$NOVA_REPO_ROOT/skills/ha-automation-best-practices.md",
        "$NOVA_REPO_ROOT/skills/ha-safety.md",
      ],
      modules: [
        "$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/resolve.md",
        "$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/create-update.md",
      ],
    },
  ],
  [
    "automation.update",
    {
      companions: [
        "$NOVA_REPO_ROOT/skills/ha-automation-best-practices.md",
        "$NOVA_REPO_ROOT/skills/ha-safety.md",
      ],
      modules: [
        "$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/resolve.md",
        "$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/create-update.md",
      ],
    },
  ],
  [
    "automation.delete",
    {
      companions: ["$NOVA_REPO_ROOT/skills/ha-safety.md"],
      modules: [
        "$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/resolve.md",
        "$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/delete.md",
      ],
    },
  ],
  [
    "automation.read",
    {
      companions: [],
      modules: [
        "$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/resolve.md",
        "$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/read.md",
      ],
    },
  ],
  [
    "automation.list",
    {
      companions: [],
      modules: ["$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/read.md"],
    },
  ],
  [
    "script.create",
    {
      companions: ["$NOVA_REPO_ROOT/skills/ha-safety.md"],
      modules: [
        "$NOVA_REPO_ROOT/skills/ha-nova/modules/script/resolve.md",
        "$NOVA_REPO_ROOT/skills/ha-nova/modules/script/create-update.md",
      ],
    },
  ],
  [
    "script.update",
    {
      companions: ["$NOVA_REPO_ROOT/skills/ha-safety.md"],
      modules: [
        "$NOVA_REPO_ROOT/skills/ha-nova/modules/script/resolve.md",
        "$NOVA_REPO_ROOT/skills/ha-nova/modules/script/create-update.md",
      ],
    },
  ],
  [
    "script.delete",
    {
      companions: ["$NOVA_REPO_ROOT/skills/ha-safety.md"],
      modules: [
        "$NOVA_REPO_ROOT/skills/ha-nova/modules/script/resolve.md",
        "$NOVA_REPO_ROOT/skills/ha-nova/modules/script/delete.md",
      ],
    },
  ],
  [
    "script.read",
    {
      companions: [],
      modules: [
        "$NOVA_REPO_ROOT/skills/ha-nova/modules/script/resolve.md",
        "$NOVA_REPO_ROOT/skills/ha-nova/modules/script/read.md",
      ],
    },
  ],
  [
    "script.list",
    {
      companions: [],
      modules: ["$NOVA_REPO_ROOT/skills/ha-nova/modules/script/read.md"],
    },
  ],
]);
