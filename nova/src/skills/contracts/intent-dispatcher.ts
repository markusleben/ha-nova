export interface IntentDefinition {
  companions: string[];
  modules: string[];
}

export interface IntentLoadPlan {
  intent: string;
  companions: string[];
  modules: string[];
  orderedLoadList: string[];
}

export function resolveIntentLoadPlan(
  intent: string,
  matrix: Map<string, IntentDefinition>
): IntentLoadPlan {
  const def = matrix.get(intent);
  if (!def) {
    throw new Error(`Unknown intent: ${intent}`);
  }

  const companions = [...def.companions];
  const modules = [...def.modules];

  const orderedLoadList: string[] = [];
  const seen = new Set<string>();
  for (const item of [...companions, ...modules]) {
    if (seen.has(item)) {
      continue;
    }
    seen.add(item);
    orderedLoadList.push(item);
  }

  return {
    intent,
    companions,
    modules,
    orderedLoadList,
  };
}
