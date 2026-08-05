import {
  type AvailabilityFixture,
  type ConfigEntry,
  type RegistryRow,
  type StateRow,
} from "./_health-availability-oracle.js";

// Shared adversarial fixture builder for the availability test files.
export type GroupInput = {
  platform: string;
  count: number;
  state: string;
  disabled_by?: string | null;
  error_reason?: string;
  error_reason_translation_key?: string;
};

export function groupFixture(groups: GroupInput[]): AvailabilityFixture {
  const states: StateRow[] = [];
  const registry: RegistryRow[] = [];
  const entries: ConfigEntry[] = [];
  for (const [groupIndex, group] of groups.entries()) {
    const entryID = `private-entry-${groupIndex}`;
    entries.push({
      entry_id: entryID,
      domain: group.platform,
      state: group.state,
      title: `Private ${groupIndex}`,
      ...(group.disabled_by !== undefined
        ? { disabled_by: group.disabled_by }
        : {}),
      ...(group.error_reason !== undefined
        ? { error_reason: group.error_reason }
        : {}),
      ...(group.error_reason_translation_key !== undefined
        ? {
            error_reason_translation_key: group.error_reason_translation_key,
          }
        : {}),
    });
    for (let rowIndex = 0; rowIndex < group.count; rowIndex += 1) {
      const entityID = `sensor.private_${groupIndex}_${rowIndex}`;
      states.push({ entity_id: entityID, state: "unavailable" });
      registry.push({
        entity_id: entityID,
        config_entry_id: entryID,
        platform: group.platform,
      });
    }
  }
  return { states, registry, entries, devices: [] };
}
