// The connector address: Home Assistant Link's while its remote access is on
// (cloud/status), else Home Assistant's external URL when it is public HTTPS
// (get_config); null when neither. Never http://, an IP, or a local-only
// name. Same rules as cli/chat_apps_connector.go chatAppsBase (shared vectors:
// tests/fixtures/chat-connector-base.json). Add to Claude is Claude's
// documented install link: claude.ai only, the address only, never a token.
export function chatConnectorUrl(status: unknown, config: unknown): string | null {
  type Cert = { common_name?: unknown; alternative_names?: unknown };
  const s = status as { logged_in?: unknown; active_subscription?: unknown; remote_connected?: unknown; remote_domain?: unknown; remote_certificate_status?: unknown; remote_certificate?: Cert | null; prefs?: { remote_enabled?: unknown } } | null;
  const cert = [s?.remote_certificate?.common_name, ...(Array.isArray(s?.remote_certificate?.alternative_names) ? s.remote_certificate.alternative_names : [])].map((n) => String(n).trim().toLowerCase().replace(/\.$/, ""));
  const link = s?.logged_in === true && s.active_subscription === true && s.remote_connected === true && s.prefs?.remote_enabled === true && s.remote_certificate_status === "ready" &&
    typeof s.remote_domain === "string" && cert.includes(s.remote_domain.trim().toLowerCase().replace(/\.$/, "")); // as cli/cloud_ha_api.go ValidateForOrigin
  const external = /^https:\/\/([^/?#@\s\\:]+)(?::([1-9]\d{0,4}))?\/?$/i.exec(String((config as { external_url?: unknown } | null)?.external_url ?? "").trim());
  const name = (link ? String(s.remote_domain).trim() : external?.[1] ?? "").toLowerCase().replace(/\.$/, "");
  const port = !link && external?.[2] !== undefined ? Number(external[2]) : 0;
  const ok = /^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?$/.test(name) && port <= 65535 &&
    !/\.(local|lan|home|internal|localdomain|localhost|home\.arpa|ts\.net)$/.test(name) && (link || !name.endsWith(".nabu.casa"));
  return ok ? `https://${name}${port ? `:${port}` : ""}/api/ha_nova/mcp` : null;
}
// Team and Enterprise members can't add one; an owner adds it under
// organization settings (admin = true), with the same parameters.
export const claudeConnectorLink = (url: string, admin = false): string =>
  `https://claude.ai/${admin ? "admin-settings" : "customize"}/connectors?modal=add-custom-connector&connectorName=HA%20NOVA&connectorUrl=${encodeURIComponent(url)}`;

// Link first (cloud/status fails without the cloud integration); get_config
// only without it, so a hiccup there never hides a working Link address.
export const readConnectorUrl = async (send: (type: string) => Promise<unknown>): Promise<string | null> =>
  chatConnectorUrl(await send("cloud/status").catch(() => null), null) ?? chatConnectorUrl(null, await send("get_config"));
