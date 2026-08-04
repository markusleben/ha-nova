import {
  type ConfigEntry,
  compareCodePoint,
  isAttentionEntry,
} from "./_health-availability-support.js";

// Detail selection and row rendering for the availability oracle — the
// executable form of availability-analysis.md → "Grouping and detail
// budgets" (pools, 50-row budget) and "Privacy modes" (identity form).

export type Group = {
  key: string;
  baseLabel: string;
  count: number;
  restored: number;
  classificationCount: number;
  deviceMatchedRows: number;
  deviceIDs: Set<string>;
  entityRows: {
    id: string;
    restored: boolean;
    lowSignal: boolean;
    name: string;
  }[];
  currentTrackerRows: number;
  currentFindingRows: number;
  entry: ConfigEntry | undefined;
  isPlatformOnly: boolean;
};

// Shared base labels disambiguate with hidden-code-point ordinals for
// config-entry groups and a fixed suffix for platform-only groups.
export function buildGroupLabeler(groups: Group[]): (group: Group) => string {
  const groupsByBaseLabel = new Map<string, Group[]>();
  for (const group of groups) {
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
  return (group: Group): string => {
    if (group.isPlatformOnly) {
      return `${group.baseLabel} (no config-entry attribution)`;
    }
    const ordinal = entryOrdinals.get(group.key);
    return ordinal ? `${group.baseLabel} entry ${ordinal}` : group.baseLabel;
  };
}

export const failurePriority = new Map([
  ["setup_error", 0],
  ["setup_retry", 1],
  ["migration_error", 2],
  ["failed_unload", 3],
  ["not_loaded", 4],
]);

export type DetailSelection = {
  selectedAttentionEntries: ConfigEntry[];
  omittedAttentionEntryCount: number;
  detailCandidates: Group[];
  displayed: Group[];
  displayedKeys: Set<string>;
};

export function selectAvailabilityDetail(
  groups: Group[],
  sorted: Group[],
  entries: ConfigEntry[],
): DetailSelection {
  const failedEntries = entries
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
  // Tracker membership comes from the group's member domains, not its label:
  // a mobile_app config-entry group holds device_tracker rows.
  const isCurrentTracker = (group: Group): boolean =>
    group.currentTrackerRows > 0;
  // Pool (b) iterates in the failure-priority order of the chosen attention
  // entries, not catalog order; pool (c) holds only CURRENT groups —
  // restored-only inventory stays summarized in the catalog.
  const detailCandidateKeys = new Set(detailCandidates.map((g) => g.key));
  const groupByEntryID = new Map(
    groups
      .filter((g) => g.entry)
      .map((g) => [g.entry!.entry_id, g] as const),
  );
  const attentionPool = selectedAttentionEntries
    .map((entry) => groupByEntryID.get(entry.entry_id))
    .filter(
      (g): g is Group =>
        g !== undefined &&
        detailCandidateKeys.has(g.key) &&
        !isCurrentTracker(g),
    );
  // Pool (a) orders current tracker groups by their available finding
  // priority (attention state first); pool (c) puts groups with current
  // non-stateless findings before stateless/low-signal-only context. The
  // stable sorts keep catalog order as the tie-break.
  const trackerRank = (group: Group): number =>
    group.entry && isAttentionEntry(group.entry)
      ? (failurePriority.get(group.entry.state) ?? 4)
      : 5;
  const pooled = [
    ...detailCandidates
      .filter((g) => isCurrentTracker(g))
      .sort((left, right) => trackerRank(left) - trackerRank(right)),
    ...attentionPool,
    ...detailCandidates
      .filter(
        (g) =>
          !isCurrentTracker(g) &&
          !isAttentionEntry(g.entry) &&
          g.count > g.restored,
      )
      .sort(
        (left, right) =>
          Number(left.currentFindingRows === 0) -
          Number(right.currentFindingRows === 0),
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
  return {
    selectedAttentionEntries,
    omittedAttentionEntryCount:
      failedEntries.length - selectedAttentionEntries.length,
    detailCandidates,
    displayed,
    displayedKeys: new Set(displayed.map((group) => group.key)),
  };
}

// Every truncation path carries the exact follow-up plus the fresh-live-read
// notice (output-rules.md → Progressive Detail). A selected group renders its
// rows through one shared path for both owners; the DETAIL selection is
// identical in every privacy mode, only the identity form changes.
export function buildExampleRenderer(
  displayed: Group[],
  privacyMode: string,
): (group: Group) => string[] {
  // Finding priority for example rows: current findings first, current
  // stateless/low-signal context second, restored context last, then
  // entity_id — never input order.
  const exampleRank = (row: { restored: boolean; lowSignal: boolean }): number =>
    row.restored ? 2 : row.lowSignal ? 1 : 0;
  const exampleRowsFor = (group: Group) => {
    const prioritized = [...group.entityRows].sort(
      (left, right) =>
        exampleRank(left) - exampleRank(right) ||
        compareCodePoint(left.id, right.id),
    );
    return group.count <= 10 ? prioritized : prioritized.slice(0, 5);
  };
  // Shareable mode keeps the identical detail selection but replaces each
  // identity with a deterministic per-type alias assigned by hidden
  // code-point sort of the underlying IDs.
  const aliasByID = new Map<string, string>();
  if (privacyMode === "shareable") {
    const shownIDs = new Set<string>();
    for (const group of displayed) {
      for (const row of exampleRowsFor(group)) shownIDs.add(row.id);
    }
    const byType = new Map<string, string[]>();
    for (const id of [...shownIDs].sort(compareCodePoint)) {
      const type = id.split(".", 1)[0] ?? "entity";
      const list = byType.get(type) ?? [];
      list.push(id);
      byType.set(type, list);
    }
    for (const [type, ids] of byType) {
      ids.forEach((id, index) => aliasByID.set(id, `${type} ${index + 1}`));
    }
  }
  return (group: Group): string[] => {
    if (privacyMode === "aggregate") return [];
    const out: string[] = [];
    for (const row of exampleRowsFor(group)) {
      // Private detail is the exact ID plus the safely renderable friendly
      // name when one exists; Shareable replaces both with the alias.
      out.push(
        privacyMode === "private"
          ? `  - ${row.id}${row.name ? ` (${row.name})` : ""}`
          : `  - ${aliasByID.get(row.id) ?? "entity"}`,
      );
    }
    if (group.count > 10) {
      out.push(
        `  total ${group.count}, shown 5, omitted ${group.count - 5}. Request this group's full detail for all rows; results may have changed (fresh live read).`,
      );
    }
    return out;
  };
}
