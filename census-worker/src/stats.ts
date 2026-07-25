import { InstallationStats, LegacyCounterRow } from "./census.js";

export const ACTIVE_DAYS = 21;
export const RETENTION_DAYS = 60;
export const RELAY_APP_SLUG = "2368fcfa_ha_nova_relay";
export const HA_ANALYTICS_URL =
  "https://analytics.home-assistant.io/addons.json";

export interface RelayAppAnalytics {
  status: "available" | "unavailable";
  source: string;
  slug: string;
  total?: number;
  by_version?: Record<string, number>;
  error?: string;
}

export interface PrivateStats {
  schema: 2;
  generated_at: string;
  client_installations: InstallationStats & {
    active_definition: string;
    known_definition: string;
    counting_note: string;
  };
  relay_app_installations: RelayAppAnalytics & {
    counting_note: string;
  };
  legacy_ping_activity: {
    counting_note: string;
    weekly: { iso_week: string; count: number }[];
  };
}

export function privateStatsHeaders(
  api: boolean,
  metadata?: { id: string; tag: string },
): Headers {
  const headers = new Headers({
    "Cache-Control": "private, no-store",
    "Content-Security-Policy":
      "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'",
    "Content-Type": api
      ? "application/json; charset=utf-8"
      : "text/html; charset=utf-8",
    "Referrer-Policy": "no-referrer",
    "X-Content-Type-Options": "nosniff",
  });
  if (metadata !== undefined) {
    headers.set("X-HA-NOVA-Deployment-SHA", metadata.tag);
    headers.set("X-HA-NOVA-Version-ID", metadata.id);
  }
  return headers;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "unknown error";
}

export async function fetchRelayAppAnalytics(
  fetcher: typeof fetch = fetch,
): Promise<RelayAppAnalytics> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 3000);
  try {
    const response = await fetcher(HA_ANALYTICS_URL, {
      headers: { Accept: "application/json" },
      signal: controller.signal,
    });
    if (!response.ok) {
      throw new Error(`Home Assistant Analytics HTTP ${response.status}`);
    }
    const payload = (await response.json()) as Record<string, unknown>;
    const raw = payload[RELAY_APP_SLUG];
    if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
      throw new Error("Relay App slug is absent");
    }
    const record = raw as Record<string, unknown>;
    if (
      typeof record["total"] !== "number" ||
      !Number.isSafeInteger(record["total"]) ||
      record["total"] < 0 ||
      typeof record["versions"] !== "object" ||
      record["versions"] === null ||
      Array.isArray(record["versions"])
    ) {
      throw new Error("Relay App analytics shape is invalid");
    }
    const byVersion: Record<string, number> = {};
    for (const [version, count] of Object.entries(
      record["versions"] as Record<string, unknown>,
    )) {
      if (
        typeof count !== "number" ||
        !Number.isSafeInteger(count) ||
        count < 0
      ) {
        throw new Error("Relay App version count is invalid");
      }
      byVersion[version] = count;
    }
    const versionTotal = Object.values(byVersion).reduce(
      (sum, count) => sum + count,
      0,
    );
    if (
      !Number.isSafeInteger(versionTotal) ||
      versionTotal !== record["total"]
    ) {
      throw new Error("Relay App version counts do not match total");
    }
    return {
      status: "available",
      source: HA_ANALYTICS_URL,
      slug: RELAY_APP_SLUG,
      total: record["total"],
      by_version: byVersion,
    };
  } catch (error) {
    return {
      status: "unavailable",
      source: HA_ANALYTICS_URL,
      slug: RELAY_APP_SLUG,
      error: errorMessage(error),
    };
  } finally {
    clearTimeout(timeout);
  }
}

export function buildPrivateStats(
  client: InstallationStats,
  legacyRows: LegacyCounterRow[],
  relay: RelayAppAnalytics,
  now: Date,
): PrivateStats {
  const weekly = new Map<string, number>();
  for (const row of legacyRows) {
    weekly.set(row.iso_week, (weekly.get(row.iso_week) ?? 0) + row.count);
  }
  return {
    schema: 2,
    generated_at: now.toISOString(),
    client_installations: {
      ...client,
      active_definition: `last report within ${ACTIVE_DAYS} days`,
      known_definition: `last report within ${RETENTION_DAYS} days`,
      counting_note:
        "Voluntary, self-reported participating HA NOVA client installations. Dedicated IDs are hashed before storage. These are not verified people or the complete installed base.",
    },
    relay_app_installations: {
      ...relay,
      counting_note:
        "External metric from opted-in Home Assistant Analytics, not HA NOVA Census data. Resetting HA NOVA Census data does not change it. It must not be added to client installations.",
    },
    legacy_ping_activity: {
      counting_note:
        "Identifier-free schema-1 ping activity retained separately. It cannot be converted into installation counts.",
      weekly: [...weekly.entries()]
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([iso_week, count]) => ({ iso_week, count })),
    },
  };
}

function escapeHTML(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function rows(values: Record<string, number>): string {
  const entries = Object.entries(values).sort(([left], [right]) =>
    left.localeCompare(right),
  );
  if (entries.length === 0) {
    return '<tr><td colspan="2">No data</td></tr>';
  }
  return entries
    .map(
      ([label, count]) =>
        `<tr><td>${escapeHTML(label)}</td><td>${count}</td></tr>`,
    )
    .join("");
}

function legacyRows(values: { iso_week: string; count: number }[]): string {
  if (values.length === 0) {
    return '<tr><td colspan="2">No data</td></tr>';
  }
  return values
    .map(
      ({ iso_week, count }) =>
        `<tr><td>${escapeHTML(iso_week)}</td><td>${count}</td></tr>`,
    )
    .join("");
}

export function renderDashboard(stats: PrivateStats): string {
  const relayTotal =
    stats.relay_app_installations.status === "available"
      ? String(stats.relay_app_installations.total)
      : "Unavailable";
  const relayVersions =
    stats.relay_app_installations.status === "available"
      ? rows(stats.relay_app_installations.by_version ?? {})
      : `<tr><td colspan="2">${escapeHTML(stats.relay_app_installations.error ?? "Unavailable")}</td></tr>`;
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>HA NOVA Census</title>
<style>
body{font:16px/1.45 system-ui,sans-serif;max-width:980px;margin:40px auto;padding:0 20px;color:#17202a;background:#f7f9fb}
h1,h2{line-height:1.2}.cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:16px}
.card,section{background:white;border:1px solid #dfe5ea;border-radius:10px;padding:18px;margin:16px 0}
.value{font-size:2rem;font-weight:700}table{border-collapse:collapse;width:100%}td,th{padding:8px;border-bottom:1px solid #e8ecef;text-align:left}
.source-guide{background:#eef7ff;border-color:#8ebce5}.source-label{display:block;color:#3d5366;font-size:.75rem;font-weight:700;letter-spacing:.04em;margin-bottom:8px;text-transform:uppercase}
.external{border-color:#8ebce5}.note{color:#53606b;font-size:.92rem}a{overflow-wrap:anywhere}code{background:#eef2f5;padding:2px 5px;border-radius:4px}
</style>
</head>
<body>
<h1>HA NOVA Census</h1>
<p class="note">Private maintainer dashboard · generated ${escapeHTML(stats.generated_at)}</p>
<section class="source-guide"><h2>Two independent sources</h2><p><strong>HA NOVA Census</strong> numbers come from voluntary client reports stored by HA NOVA. <strong>Relay App installations</strong> come from external, opted-in Home Assistant Analytics.</p><p><strong>Never add these numbers.</strong> They measure different populations.</p></section>
<div class="cards">
<div class="card"><span class="source-label">HA NOVA Census</span><div class="value">${stats.client_installations.active_21_days}</div><strong>Active client installations</strong><div class="note">${escapeHTML(stats.client_installations.active_definition)}</div></div>
<div class="card"><span class="source-label">HA NOVA Census</span><div class="value">${stats.client_installations.known_60_days}</div><strong>Known client installations</strong><div class="note">${escapeHTML(stats.client_installations.known_definition)}</div></div>
<div class="card external"><span class="source-label">External · Home Assistant Analytics</span><div class="value">${relayTotal}</div><strong>Relay App installations</strong><div class="note">Opt-in Home Assistant metric · not HA NOVA Census data</div></div>
<div class="card"><span class="source-label">HA NOVA Census</span><div class="value">${stats.client_installations.new_installation_rejections_today}</div><strong>New-client rejects today</strong><div class="note">Admission protection; investigate any non-zero value</div></div>
</div>
<section><span class="source-label">HA NOVA Census</span><h2>Client versions · active 21 days</h2><table><tr><th>Version</th><th>Installations</th></tr>${rows(stats.client_installations.by_version)}</table></section>
<section><span class="source-label">HA NOVA Census</span><h2>Client operating systems · active 21 days</h2><table><tr><th>OS</th><th>Installations</th></tr>${rows(stats.client_installations.by_os)}</table></section>
<section><span class="source-label">HA NOVA Census</span><h2>Recently observed Relay versions</h2><p><strong>${stats.client_installations.relay_not_recently_observed}</strong> active clients did not report a Relay version observed within the previous 14 days.</p><table><tr><th>Relay version</th><th>Client installations</th></tr>${rows(stats.client_installations.relay_versions)}</table></section>
<section class="external"><span class="source-label">External source</span><h2>Relay App installations · Home Assistant Analytics</h2><p>${escapeHTML(stats.relay_app_installations.counting_note)}</p><p class="note">Dataset: <a href="${HA_ANALYTICS_URL}">${HA_ANALYTICS_URL}</a><br>Relay App slug: <code>${escapeHTML(stats.relay_app_installations.slug)}</code></p><table><tr><th>Version</th><th>Installations</th></tr>${relayVersions}</table></section>
<section><span class="source-label">HA NOVA Census · legacy</span><h2>Legacy ping activity</h2><p class="note">${escapeHTML(stats.legacy_ping_activity.counting_note)}</p><table><tr><th>ISO week</th><th>Accepted pings</th></tr>${legacyRows(stats.legacy_ping_activity.weekly)}</table></section>
<section><h2>Interpretation</h2><p>${escapeHTML(stats.client_installations.counting_note)}</p><p>${escapeHTML(stats.legacy_ping_activity.counting_note)}</p><p>Machine-readable data: <a href="/stats/api">/stats/api</a>.</p></section>
</body>
</html>`;
}
