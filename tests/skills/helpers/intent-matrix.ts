import { readFileSync } from "node:fs";

import type { IntentDefinition } from "../../../src/skills/contracts/intent-dispatcher.js";

function normalizeBacktickValue(value: string): string {
  if (value.startsWith('"') && value.endsWith('"') && value.length >= 2) {
    return value.slice(1, -1);
  }
  return value;
}

export function parseIntentMatrix(filePath = "skills/ha-nova/core/intents.md"): Map<string, IntentDefinition> {
  const lines = readFileSync(filePath, "utf8").split("\n");
  const matrix = new Map<string, IntentDefinition>();

  let currentIntent: string | null = null;
  let mode: "companions" | "modules" | null = null;

  for (const line of lines) {
    const intentMatch = line.match(/^- `([^`]+)`$/);
    if (intentMatch) {
      currentIntent = intentMatch[1] ?? null;
      if (!currentIntent) {
        continue;
      }
      matrix.set(currentIntent, { companions: [], modules: [] });
      mode = null;
      continue;
    }

    if (!currentIntent) {
      continue;
    }

    if (line.includes("`required_companions[]`:")) {
      mode = "companions";
      if (line.includes("none")) {
        mode = null;
      }
      continue;
    }

    if (line.includes("`modules[]`:")) {
      mode = "modules";
      continue;
    }

    const valueMatch = line.match(/^\s+- `([^`]+)`$/);
    if (!valueMatch || !mode) {
      continue;
    }

    const target = matrix.get(currentIntent);
    if (!target) {
      continue;
    }

    if (mode === "companions") {
      const value = valueMatch[1];
      if (!value) {
        continue;
      }
      target.companions.push(normalizeBacktickValue(value));
    } else {
      const value = valueMatch[1];
      if (!value) {
        continue;
      }
      target.modules.push(normalizeBacktickValue(value));
    }
  }

  return matrix;
}
