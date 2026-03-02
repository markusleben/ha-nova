export interface NormalizedAutomationYamlShape {
  triggers: unknown[];
  conditions: unknown[];
  actions: unknown[];
}

type AutomationYamlLike = Record<string, unknown>;

function toArray(value: unknown): unknown[] {
  if (Array.isArray(value)) {
    return value;
  }
  if (value === undefined || value === null) {
    return [];
  }
  return [value];
}

function collectPluralThenSingular(
  payload: AutomationYamlLike,
  pluralKey: "triggers" | "conditions" | "actions",
  singularKey: "trigger" | "condition" | "action"
): unknown[] {
  return [...toArray(payload[pluralKey]), ...toArray(payload[singularKey])];
}

export function normalizeAutomationYamlShape(payload: AutomationYamlLike): NormalizedAutomationYamlShape {
  return {
    triggers: collectPluralThenSingular(payload, "triggers", "trigger"),
    conditions: collectPluralThenSingular(payload, "conditions", "condition"),
    actions: collectPluralThenSingular(payload, "actions", "action"),
  };
}

export function toCanonicalAutomationYamlShape(payload: AutomationYamlLike): AutomationYamlLike {
  const normalized = normalizeAutomationYamlShape(payload);

  const canonical: AutomationYamlLike = { ...payload };
  delete canonical.trigger;
  delete canonical.condition;
  delete canonical.action;
  canonical.triggers = normalized.triggers;
  canonical.conditions = normalized.conditions;
  canonical.actions = normalized.actions;

  return canonical;
}
