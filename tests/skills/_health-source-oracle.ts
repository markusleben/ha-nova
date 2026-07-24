import {
  isSafeEntityID,
  isSafeHASlug,
} from "./_health-availability-support.js";

type SourceName =
  | "config"
  | "components"
  | "states"
  | "repairs"
  | "integrations"
  | "entity registry"
  | "device registry"
  | "system health";

export type RawHealthSources = Partial<Record<SourceName, unknown>>;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function dataRecord(envelope: unknown): Record<string, unknown> | undefined {
  if (!isRecord(envelope) || envelope.ok !== true || !isRecord(envelope.data)) {
    return undefined;
  }
  return envelope.data;
}

function restBody(envelope: unknown): unknown {
  const data = dataRecord(envelope);
  if (
    !data ||
    typeof data.status !== "number" ||
    !Number.isInteger(data.status) ||
    data.status < 200 ||
    data.status >= 300
  ) {
    return undefined;
  }
  return data.body;
}

function wsData(envelope: unknown): unknown {
  if (!isRecord(envelope) || envelope.ok !== true) return undefined;
  return envelope.data;
}

function everyRecord(
  value: unknown,
  predicate: (row: Record<string, unknown>) => boolean,
): value is Record<string, unknown>[] {
  return (
    Array.isArray(value) &&
    value.every((row) => isRecord(row) && predicate(row))
  );
}

function optionalString(value: unknown): boolean {
  return value === undefined || value === null || typeof value === "string";
}

function optionalSafeSlug(value: unknown): boolean {
  return value === undefined || value === null || isSafeHASlug(value);
}

export function unavailableHealthSources(
  sources: RawHealthSources,
): SourceName[] {
  const unavailable: SourceName[] = [];
  const config = restBody(sources.config);
  const components = restBody(sources.components);
  const states = restBody(sources.states);
  const repairs = dataRecord(sources.repairs)?.issues;
  const integrations = wsData(sources.integrations);
  const systemEvents = dataRecord(sources["system health"])?.events;

  if (!isRecord(config)) unavailable.push("config");
  if (!Array.isArray(components) || !components.every(isSafeHASlug)) {
    unavailable.push("components");
  }
  const validStates = everyRecord(
    states,
    (row) =>
      isSafeEntityID(row.entity_id) &&
      typeof row.state === "string" &&
      (row.attributes === undefined || isRecord(row.attributes)),
  );
  if (!validStates) unavailable.push("states");
  if (
    !everyRecord(
      repairs,
      (row) => typeof row.issue_id === "string" && isSafeHASlug(row.domain),
    )
  ) {
    unavailable.push("repairs");
  }
  if (
    !everyRecord(
      integrations,
      (row) =>
        typeof row.entry_id === "string" &&
        isSafeHASlug(row.domain) &&
        typeof row.state === "string" &&
        optionalString(row.disabled_by),
    )
  ) {
    unavailable.push("integrations");
  }
  if (!everyRecord(systemEvents, (row) => typeof row.type === "string")) {
    unavailable.push("system health");
  }

  const hasAvailabilityRows =
    validStates &&
    states.some(
      (row) => row.state === "unavailable" || row.state === "unknown",
    );
  if (hasAvailabilityRows) {
    if (
      !everyRecord(
        wsData(sources["entity registry"]),
        (row) =>
          isSafeEntityID(row.entity_id) &&
          optionalString(row.config_entry_id) &&
          optionalString(row.device_id) &&
          optionalSafeSlug(row.platform),
      )
    ) {
      unavailable.push("entity registry");
    }
    if (
      !everyRecord(
        wsData(sources["device registry"]),
        (row) => typeof row.id === "string",
      )
    ) {
      unavailable.push("device registry");
    }
  }
  return unavailable;
}
