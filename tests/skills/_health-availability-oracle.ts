import {
  type Group,
  buildExampleRenderer,
  buildGroupLabeler,
  selectAvailabilityDetail,
} from "./_health-availability-detail.js";
import {
  type AvailabilityFixture,
  compareCodePoint,
  inventoryDomains,
  isAttentionEntry,
  isSafeEntityID,
  isSafeHASlug,
  lowSignalDomains,
  percent,
  safeDisplayName,
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

// Group, selection pools, budget, and privacy rendering live in
// _health-availability-detail.ts; this file owns reconciliation, joins,
// classification, and report assembly.

export function summarizeAvailability(fixture: AvailabilityFixture): string {
  const stateRows = fixture.states.filter(
    (row) => row.state === "unavailable" || row.state === "unknown",
  );
  // Invalid state entity IDs are counted separately in Coverage and never
  // rendered, grouped, or reconciled (availability-analysis.md → Join validity).
  const invalidRowCount = stateRows.filter(
    (row) => !isSafeEntityID(row.entity_id),
  ).length;
  const candidates = stateRows.filter((row) => isSafeEntityID(row.entity_id));
  // One malformed registry row invalidates the whole source (attribution
  // limitation, never per-row salvage) — availability-analysis.md → Join
  // validity.
  const registrySourceValid = (fixture.registry ?? []).every(
    (row) =>
      isSafeEntityID(row.entity_id) &&
      (row.platform === undefined || isSafeHASlug(row.platform)),
  );
  const registryRows = registrySourceValid ? fixture.registry : undefined;
  const registryByEntity = new Map(
    (registryRows ?? []).map((row) => [row.entity_id, row]),
  );
  const entryByID = new Map(
    fixture.entries.map((entry) => [entry.entry_id, entry]),
  );
  // Whole-source invalidation applies to EITHER registry: one malformed
  // device row (missing/empty id) makes the device registry unavailable.
  const devicesSourceValid = (fixture.devices ?? []).every(
    (device) => typeof device.id === "string" && device.id !== "",
  );
  const deviceRows = devicesSourceValid ? fixture.devices : undefined;
  const registeredDeviceIDs = new Set(
    (deviceRows ?? []).map((device) => device.id),
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
    // A config_entry_id with no matching entry attributes nothing and must
    // not invent a group either — only an exact entry join or a platform
    // grants group membership.
    const rawKey =
      registry?.config_entry_id && entry !== undefined
        ? `entry:${registry.config_entry_id}`
        : platform
          ? `platform:${platform}`
          : "";
    const key = baseLabel ? rawKey : "";
    const registeredDeviceID =
      registry?.device_id && registeredDeviceIDs.has(registry.device_id)
        ? registry.device_id
        : "";
    // Attribution requires an EXACT join: a config_entry_id with no matching
    // entry attributes nothing (availability-analysis.md → Join validity).
    const hasAttribution = Boolean(
      registry && (platform || entry !== undefined || registeredDeviceID),
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
      entityRows: [] as {
        id: string;
        restored: boolean;
        lowSignal: boolean;
        name: string;
      }[],
      currentTrackerRows: 0,
      currentFindingRows: 0,
      entry,
      isPlatformOnly: key.startsWith("platform:"),
    };
    // Stateless/low-signal context is the UNKNOWN state in those domains —
    // an unavailable button is a current finding like any other row.
    const isLowSignalContext =
      row.state === "unknown" && lowSignalDomains.has(domain);
    group.entityRows.push({
      id: row.entity_id,
      restored: isRestored,
      lowSignal: isLowSignalContext,
      // Documented precedence: friendly_name → registry name →
      // registry original_name → exact ID.
      name: safeDisplayName(
        row.attributes?.friendly_name ??
          registry?.name ??
          registry?.original_name,
        row.entity_id,
      ),
    });
    if (!isRestored && trackerDomains.has(domain)) {
      group.currentTrackerRows += 1;
    }
    if (!isRestored && !isLowSignalContext) {
      group.currentFindingRows += 1;
    }
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

  const safeLabel = buildGroupLabeler([...groups.values()]);
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

  const {
    selectedAttentionEntries,
    omittedAttentionEntryCount,
    detailCandidates,
    displayed,
    displayedKeys,
  } = selectAvailabilityDetail([...groups.values()], sorted, fixture.entries);
  const displayedCount = displayed.reduce((sum, group) => sum + group.count, 0);
  const privacyMode = fixture.privacyMode ?? "private";
  const renderExampleRows = buildExampleRenderer(displayed, privacyMode);
  const missingSources = [
    ...new Set([
      ...(fixture.unavailableSources ?? []),
      ...(candidates.length > 0 && registryRows === undefined
        ? ["entity registry"]
        : []),
      ...(candidates.length > 0 && deviceRows === undefined
        ? ["device registry"]
        : []),
    ]),
  ];
  const missingSource = missingSources.length > 0;
  const overall = missingSource
    ? "Limited"
    : (fixture.activeRepairs ?? 0) > 0 ||
        selectedAttentionEntries.length > 0 ||
        (fixture.lowBatteries ?? 0) > 0 ||
        (fixture.failedSystemHealth ?? 0) > 0
      ? "Attention"
      : "OK";
  const nextStep =
    (fixture.activeRepairs ?? 0) > 0
      ? "Review active repairs."
      : selectedAttentionEntries.length > 0
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
    `Coverage unavailable: ${missingSources.length > 0 ? missingSources.join(", ") : "none"}.${invalidRowCount > 0 ? ` Invalid rows: ${invalidRowCount} (excluded from reconciliation, never rendered).` : ""}`,
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
      deviceRows === undefined
        ? "Device registry unavailable; device attribution is limited."
        : `${overallDeviceIDs.size} known device-registry records; device attribution ${deviceMatchedRows}/${candidates.length} entity states.`,
    );
  }
  if (candidates.length > 0 && deviceRows !== undefined) {
    const omittedDeviceClusters = deviceClusters.slice(3);
    const omittedDeviceStates = omittedDeviceClusters.reduce(
      (sum, [, count]) => sum + count,
      0,
    );
    lines.push(
      displayedDeviceClusters.length === 0
        ? "No device-attributed subclusters."
        : `Largest device subclusters: ${displayedDeviceClusters.map(([, count]) => count).join(", ")} entity states; top three cover ${displayedDeviceStates}/${deviceMatchedRows} device-attributed states; ${omittedDeviceClusters.length} device clusters omitted (${omittedDeviceStates} entity states). Request the full device-subcluster list for all clusters; results may have changed (fresh live read).`,
    );
  }
  for (const group of displayed.filter(
    (item) => !isAttentionEntry(item.entry),
  )) {
    const entryState = safeEntryState(group.entry);
    const deviceContext =
      deviceRows === undefined
        ? "device registry unavailable"
        : `${group.deviceIDs.size} known device-registry records; device attribution ${group.deviceMatchedRows}/${group.count} entity states`;
    lines.push(
      `${safeLabel(group)}: ${group.count} entity states (${group.restored} restored), ${deviceContext}, ${entryState}`,
    );
    lines.push(...renderExampleRows(group));
  }
  const omittedDetails = detailCandidates.filter(
    (group) => !displayedKeys.has(group.key),
  );
  if (omittedDetails.length > 0) {
    // Every remaining group appears in the catalog with its own counts and
    // an exact, label-addressed detail request — an aggregate line alone
    // gives the user no usable name to ask for. Attention-owned groups keep
    // their whole finding (impact + catalog) under Integrations instead.
    for (const group of omittedDetails.filter(
      (item) => !isAttentionEntry(item.entry),
    )) {
      lines.push(
        `${safeLabel(group)}: total ${group.count}, shown 0, omitted ${group.count}. Request the "${safeLabel(group)}" group's detail for its rows; results may have changed (fresh live read).`,
      );
    }
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
        `${safeLabel(group)}: ${entry.state}${safeReason(entry)}; impact ${group.count} entity states — detail omitted by shared group-detail cap: total ${group.count}, shown 0, omitted ${group.count}. Request the "${safeLabel(group)}" group's detail for its rows; results may have changed (fresh live read).`,
      );
      continue;
    }
    const deviceContext =
      deviceRows === undefined
        ? "device registry unavailable"
        : `${group.deviceIDs.size} known device-registry records; device attribution ${group.deviceMatchedRows}/${group.count} entity states`;
    lines.push(
      `${safeLabel(group)}: ${entry.state}${safeReason(entry)}; impact ${group.count} entity states, ${deviceContext}`,
    );
    lines.push(...renderExampleRows(group));
  }
  if (omittedAttentionEntryCount > 0) {
    lines.push(
      `Integration-entry cap: total ${selectedAttentionEntries.length + omittedAttentionEntryCount}, shown ${selectedAttentionEntries.length}, omitted ${omittedAttentionEntryCount}. Request the full integration-entry list for the rest; results may have changed (fresh live read).`,
    );
  }
  lines.push(`Next step: ${nextStep}`);
  return lines.join("\n");
}
