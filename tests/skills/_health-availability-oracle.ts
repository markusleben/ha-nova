import {
  type AvailabilityFixture,
  type ConfigEntry,
  compareCodePoint,
  inventoryDomains,
  isAttentionEntry,
  isSafeHASlug,
  lowSignalDomains,
  percent,
  safeEntryState,
  safeReason,
} from "./_health-availability-support.js";

export type {
  AvailabilityFixture,
  ConfigEntry,
  DeviceRow,
  RegistryRow,
  StateRow,
} from "./_health-availability-support.js";

// English-only semantic oracle for deterministic aggregation, ownership,
// ordering, caps, and privacy. Runtime localization belongs to the skill agent
// and is intentionally outside this unit oracle.

type Group = {
  key: string;
  baseLabel: string;
  count: number;
  restored: number;
  classificationCount: number;
  deviceMatchedRows: number;
  deviceIDs: Set<string>;
  entityIDs: string[];
  entry: ConfigEntry | undefined;
  isPlatformOnly: boolean;
};

export function summarizeAvailability(fixture: AvailabilityFixture): string {
  const candidates = fixture.states.filter(
    (row) => row.state === "unavailable" || row.state === "unknown",
  );
  const registryByEntity = new Map(
    (fixture.registry ?? []).map((row) => [row.entity_id, row]),
  );
  const entryByID = new Map(
    fixture.entries.map((entry) => [entry.entry_id, entry]),
  );
  const registeredDeviceIDs = new Set(
    (fixture.devices ?? []).map((device) => device.id),
  );
  const groups = new Map<string, Group>();
  const overallDeviceIDs = new Set<string>();
  const deviceClusterCounts = new Map<string, number>();
  const classificationDeviceClusterCounts = new Map<string, number>();
  let attributed = 0;
  let registryMatched = 0;
  let restored = 0;
  let classificationTotal = 0;
  let classificationAttributed = 0;
  let classificationCurrent = 0;
  let inventoryContext = 0;
  let lowSignal = 0;
  let unavailable = 0;
  let deviceMatchedRows = 0;
  // #440 six-category assignment (every raw row exactly once).
  const categories = {
    integrationFailure: 0,
    restored: 0,
    lowSignalUnknown: 0,
    trackerPresence: 0,
    attributedCurrent: 0,
    unattributed: 0,
  };
  const trackerDomains = new Set(["device_tracker", "person", "geo_location"]);

  for (const row of candidates) {
    const rawDomain = row.entity_id.split(".", 1)[0] ?? "";
    const domain = isSafeHASlug(rawDomain) ? rawDomain : "";
    const isRestored = row.attributes?.restored === true;
    if (row.state === "unavailable") unavailable += 1;
    if (isRestored) restored += 1;
    if (lowSignalDomains.has(domain)) lowSignal += 1;

    const registry = registryByEntity.get(row.entity_id);
    if (registry) registryMatched += 1;
    const entry = registry?.config_entry_id
      ? entryByID.get(registry.config_entry_id)
      : undefined;
    const platform = isSafeHASlug(registry?.platform) ? registry.platform : "";
    const entryDomain = isSafeHASlug(entry?.domain) ? entry.domain : "";
    const baseLabel = platform || entryDomain || domain;
    const rawKey = registry?.config_entry_id
      ? `entry:${registry.config_entry_id}`
      : platform
        ? `platform:${platform}`
        : "";
    const key = baseLabel ? rawKey : "";
    const registeredDeviceID =
      registry?.device_id && registeredDeviceIDs.has(registry.device_id)
        ? registry.device_id
        : "";
    const hasAttribution = Boolean(
      registry && (platform || registry.config_entry_id || registeredDeviceID),
    );
    if (hasAttribution) attributed += 1;

    // Assign exactly one category in precedence order (#440).
    if (isAttentionEntry(entry) && entry !== undefined && !entry.disabled_by) {
      categories.integrationFailure += 1;
    } else if (isRestored) {
      categories.restored += 1;
    } else if (row.state === "unknown" && lowSignalDomains.has(domain)) {
      categories.lowSignalUnknown += 1;
    } else if (trackerDomains.has(domain)) {
      categories.trackerPresence += 1;
    } else if (hasAttribution) {
      categories.attributedCurrent += 1;
    } else {
      categories.unattributed += 1;
    }

    const classificationEligible = !isAttentionEntry(entry);
    if (classificationEligible) {
      classificationTotal += 1;
      if (hasAttribution) classificationAttributed += 1;
      if (!isRestored) classificationCurrent += 1;
      if (isRestored && inventoryDomains.has(domain)) inventoryContext += 1;
      else if (isRestored) inventoryContext += 1;
      if (registeredDeviceID) {
        classificationDeviceClusterCounts.set(
          registeredDeviceID,
          (classificationDeviceClusterCounts.get(registeredDeviceID) ?? 0) + 1,
        );
      }
    }
    if (registeredDeviceID) {
      overallDeviceIDs.add(registeredDeviceID);
      deviceMatchedRows += 1;
      deviceClusterCounts.set(
        registeredDeviceID,
        (deviceClusterCounts.get(registeredDeviceID) ?? 0) + 1,
      );
    }
    if (!key) continue;

    const group = groups.get(key) ?? {
      key,
      baseLabel,
      count: 0,
      restored: 0,
      classificationCount: 0,
      deviceMatchedRows: 0,
      deviceIDs: new Set<string>(),
      entityIDs: [] as string[],
      entry,
      isPlatformOnly: key.startsWith("platform:"),
    };
    group.entityIDs.push(row.entity_id);
    group.count += 1;
    if (compareCodePoint(baseLabel, group.baseLabel) < 0) {
      group.baseLabel = baseLabel;
    }
    if (isRestored) group.restored += 1;
    if (classificationEligible) group.classificationCount += 1;
    if (registeredDeviceID) {
      group.deviceMatchedRows += 1;
      group.deviceIDs.add(registeredDeviceID);
    }
    groups.set(key, group);
  }

  const groupsByBaseLabel = new Map<string, Group[]>();
  for (const group of groups.values()) {
    const siblings = groupsByBaseLabel.get(group.baseLabel) ?? [];
    siblings.push(group);
    groupsByBaseLabel.set(group.baseLabel, siblings);
  }
  const entryOrdinals = new Map<string, number>();
  for (const siblings of groupsByBaseLabel.values()) {
    const entries = siblings
      .filter((group) => !group.isPlatformOnly)
      .sort((left, right) => compareCodePoint(left.key, right.key));
    if (siblings.length > 1) {
      entries.forEach((group, index) =>
        entryOrdinals.set(group.key, index + 1),
      );
    }
  }

  const safeLabel = (group: Group): string => {
    if (group.isPlatformOnly) {
      return `${group.baseLabel} (no config-entry attribution)`;
    }
    const ordinal = entryOrdinals.get(group.key);
    return ordinal ? `${group.baseLabel} entry ${ordinal}` : group.baseLabel;
  };
  const sorted = [...groups.values()].sort(
    (left, right) =>
      right.count - left.count ||
      compareCodePoint(left.baseLabel, right.baseLabel) ||
      compareCodePoint(left.key, right.key),
  );
  const classificationGroups = sorted
    .filter((group) => group.classificationCount > 0)
    .sort(
      (left, right) =>
        right.classificationCount - left.classificationCount ||
        compareCodePoint(left.baseLabel, right.baseLabel) ||
        compareCodePoint(left.key, right.key),
    );
  const integrationTopThreeCount = classificationGroups
    .slice(0, 3)
    .reduce((sum, group) => sum + group.classificationCount, 0);
  const deviceTopThreeCount = [...classificationDeviceClusterCounts.values()]
    .sort((left, right) => right - left)
    .slice(0, 3)
    .reduce((sum, count) => sum + count, 0);
  const topThreeCount = Math.max(integrationTopThreeCount, deviceTopThreeCount);
  const classification =
    classificationTotal > 0 &&
    inventoryContext * 100 >= classificationTotal * 60
      ? "mostly restored or tracker-style inventory"
      : classificationTotal > 0 &&
          classificationAttributed * 100 >= classificationTotal * 80 &&
          topThreeCount * 100 >= classificationTotal * 60
        ? "concentrated integration/device clusters"
        : classificationTotal > 0 &&
            classificationAttributed * 100 >= classificationTotal * 80 &&
            classificationCurrent * 100 >= classificationTotal * 60 &&
            topThreeCount * 100 < classificationTotal * 60
          ? "broad current availability problem"
          : classificationTotal === 0 && candidates.length > 0
            ? "fully explained by integration failures"
            : "insufficient registry evidence";

  const failurePriority = new Map([
    ["setup_error", 0],
    ["setup_retry", 1],
    ["migration_error", 2],
    ["failed_unload", 3],
    ["not_loaded", 4],
  ]);
  const failedEntries = fixture.entries
    .filter(isAttentionEntry)
    .sort(
      (left, right) =>
        (failurePriority.get(left.state) ?? 4) -
          (failurePriority.get(right.state) ?? 4) ||
        compareCodePoint(left.domain, right.domain) ||
        compareCodePoint(left.entry_id, right.entry_id),
    );
  const selectedAttentionEntries = failedEntries.slice(0, 25);
  const selectedAttentionIDs = new Set(
    selectedAttentionEntries.map((entry) => entry.entry_id),
  );
  const detailCandidates = sorted.filter(
    (group) =>
      !isAttentionEntry(group.entry) ||
      selectedAttentionIDs.has(group.entry!.entry_id),
  );
  // #440 Explained selection: pooled order, global 50-row budget,
  // cost = full size for 1-10 groups else 5; skip when over budget.
  const isCurrentTracker = (group: Group): boolean =>
    group.count > group.restored &&
    [...trackerDomains].some((d) => group.baseLabel === d || group.key.includes(`platform:${d}`));
  const pooled = [
    ...detailCandidates.filter((g) => isCurrentTracker(g)),
    ...detailCandidates.filter(
      (g) => !isCurrentTracker(g) && isAttentionEntry(g.entry),
    ),
    ...detailCandidates.filter(
      (g) => !isCurrentTracker(g) && !isAttentionEntry(g.entry),
    ),
  ];
  let budget = 50;
  const displayed: Group[] = [];
  for (const group of pooled) {
    const cost = group.count <= 10 ? group.count : 5;
    if (cost > budget) continue;
    displayed.push(group);
    budget -= cost;
  }
  const displayedKeys = new Set(displayed.map((group) => group.key));
  const displayedCount = displayed.reduce((sum, group) => sum + group.count, 0);
  const missingSources = [
    ...new Set([
      ...(fixture.unavailableSources ?? []),
      ...(candidates.length > 0 && fixture.registry === undefined
        ? ["entity registry"]
        : []),
      ...(candidates.length > 0 && fixture.devices === undefined
        ? ["device registry"]
        : []),
    ]),
  ];
  const missingSource = missingSources.length > 0;
  const overall = missingSource
    ? "Limited"
    : (fixture.activeRepairs ?? 0) > 0 ||
        failedEntries.length > 0 ||
        (fixture.lowBatteries ?? 0) > 0 ||
        (fixture.failedSystemHealth ?? 0) > 0
      ? "Attention"
      : "OK";
  const nextStep =
    (fixture.activeRepairs ?? 0) > 0
      ? "Review active repairs."
      : failedEntries.length > 0
        ? "Review failed integrations."
        : (fixture.lowBatteries ?? 0) > 0
          ? "Review low batteries."
          : (fixture.failedSystemHealth ?? 0) > 0
            ? "Review failed system health."
            : missingSource
              ? "Restore the unavailable source."
              : "No immediate action found.";

  const deviceClusters = [...deviceClusterCounts.entries()].sort(
    ([leftID, leftCount], [rightID, rightCount]) =>
      rightCount - leftCount || compareCodePoint(leftID, rightID),
  );
  const displayedDeviceClusters = deviceClusters.slice(0, 3);
  const displayedDeviceStates = displayedDeviceClusters.reduce(
    (sum, [, count]) => sum + count,
    0,
  );

  const lines = [
    `Status: ${overall}.`,
    `Coverage unavailable: ${missingSources.length > 0 ? missingSources.join(", ") : "none"}.`,
    "Entities:",
  ];
  if (candidates.length === 0) {
    lines.push("0 unavailable, 0 unknown entity states.");
  } else {
    lines.push(
      `${candidates.length} entity states: unavailable ${unavailable}, unknown ${candidates.length - unavailable}; restored ${restored}, current ${candidates.length - restored}. Not a device or independent-problem count.`,
      `Categories: integration failure ${categories.integrationFailure}, restored ${categories.restored}, low-signal unknown ${categories.lowSignalUnknown}, tracker/presence ${categories.trackerPresence}, attributed current ${categories.attributedCurrent}, unattributed ${categories.unattributed}; sum ${categories.integrationFailure + categories.restored + categories.lowSignalUnknown + categories.trackerPresence + categories.attributedCurrent + categories.unattributed}/${candidates.length}.`,
      `Low-signal/stateless: ${lowSignal} entity states, retained in the raw total.`,
      `Classification: ${classification}; population ${classificationTotal}/${candidates.length}. Registry match: ${registryMatched}/${candidates.length}; attribution: ${attributed}/${candidates.length}; unattributed: ${candidates.length - attributed}. Top three cover ${topThreeCount}/${classificationTotal} (${percent(topThreeCount, classificationTotal)}); displayed group details cover ${displayedCount}/${candidates.length} (${percent(displayedCount, candidates.length)}).`,
      fixture.devices === undefined
        ? "Device registry unavailable; device attribution is limited."
        : `${overallDeviceIDs.size} known device-registry records; device attribution ${deviceMatchedRows}/${candidates.length} entity states.`,
    );
  }
  if (candidates.length > 0 && fixture.devices !== undefined) {
    const omittedDeviceClusters = deviceClusters.slice(3);
    const omittedDeviceStates = omittedDeviceClusters.reduce(
      (sum, [, count]) => sum + count,
      0,
    );
    lines.push(
      displayedDeviceClusters.length === 0
        ? "No device-attributed subclusters."
        : `Largest device subclusters: ${displayedDeviceClusters.map(([, count]) => count).join(", ")} entity states; top three cover ${displayedDeviceStates}/${deviceMatchedRows} device-attributed states; ${omittedDeviceClusters.length} device clusters omitted (${omittedDeviceStates} entity states).`,
    );
  }
  for (const group of displayed.filter(
    (item) => !isAttentionEntry(item.entry),
  )) {
    const entryState = safeEntryState(group.entry);
    const deviceContext =
      fixture.devices === undefined
        ? "device registry unavailable"
        : `${group.deviceIDs.size} known device-registry records; device attribution ${group.deviceMatchedRows}/${group.count} entity states`;
    lines.push(
      `${safeLabel(group)}: ${group.count} entity states (${group.restored} restored), ${deviceContext}, ${entryState}`,
    );
    if ((fixture.privacyMode ?? "private") === "private" && group.count <= 10) {
      for (const id of group.entityIDs) {
        lines.push(`  - ${id}`);
      }
    }
  }
  const omittedDetails = detailCandidates.filter(
    (group) => !displayedKeys.has(group.key),
  );
  if (omittedDetails.length > 0) {
    lines.push(
      `${omittedDetails.length} group details omitted (${omittedDetails.reduce((sum, group) => sum + group.count, 0)} entity states).`,
    );
  }
  lines.push("Integrations:");
  for (const entry of selectedAttentionEntries) {
    const group = sorted.find(
      (item) => item.entry?.entry_id === entry.entry_id,
    );
    if (!group) {
      const safeDomain = isSafeHASlug(entry.domain)
        ? entry.domain
        : "unknown integration";
      lines.push(
        `${safeDomain}: ${entry.state}${safeReason(entry)}; impact attribution unavailable`,
      );
      continue;
    }
    if (!displayedKeys.has(group.key)) {
      lines.push(
        `${safeLabel(group)}: ${entry.state}${safeReason(entry)}; joined impact detail omitted by shared group-detail cap`,
      );
      continue;
    }
    const deviceContext =
      fixture.devices === undefined
        ? "device registry unavailable"
        : `${group.deviceIDs.size} known device-registry records; device attribution ${group.deviceMatchedRows}/${group.count} entity states`;
    lines.push(
      `${safeLabel(group)}: ${entry.state}${safeReason(entry)}; impact ${group.count} entity states, ${deviceContext}`,
    );
  }
  if (failedEntries.length > selectedAttentionEntries.length) {
    lines.push(
      `${failedEntries.length - selectedAttentionEntries.length} attention entries omitted by integration-entry cap.`,
    );
  }
  lines.push(`Next step: ${nextStep}`);
  return lines.join("\n");
}
