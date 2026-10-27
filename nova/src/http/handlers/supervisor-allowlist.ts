// The Supervisor allowlist (docs/work/2026-10-26-supervisor-role-spec.md →
// Decisions). The App holds hassio_role: manager, which reaches far more than
// HA NOVA uses; the Relay forwards a request to the Supervisor only when its
// exact method and path are listed here. cli/relay_raw_supervisor.go mirrors
// this table (tests/http/supervisor-allowlist-parity.test.ts keeps them equal).

export type SupervisorRouteKind = "read" | "logs" | "install" | "uninstall" | "options";

export interface SupervisorRoute {
  method: "GET" | "POST";
  path: string;
  kind: SupervisorRouteKind;
}

export const SUPERVISOR_ROUTES: readonly SupervisorRoute[] = [
  { method: "GET", path: "/info", kind: "read" },
  { method: "GET", path: "/os/info", kind: "read" },
  { method: "GET", path: "/host/info", kind: "read" },
  { method: "GET", path: "/network/info", kind: "read" },
  { method: "GET", path: "/hardware/info", kind: "read" },
  { method: "GET", path: "/resolution/info", kind: "read" },
  { method: "GET", path: "/core/info", kind: "read" },
  { method: "GET", path: "/supervisor/info", kind: "read" },
  { method: "GET", path: "/addons", kind: "read" },
  { method: "GET", path: "/addons/{slug}/info", kind: "read" },
  { method: "GET", path: "/store", kind: "read" },
  { method: "GET", path: "/store/addons", kind: "read" },
  { method: "GET", path: "/store/addons/{slug}", kind: "read" },
  { method: "GET", path: "/host/disks/default/usage", kind: "read" },
  { method: "GET", path: "/os/datadisk/list", kind: "read" },
  { method: "GET", path: "/host/logs/boots", kind: "read" },
  { method: "GET", path: "/host/logs/identifiers", kind: "read" },
  { method: "GET", path: "/supervisor/logs", kind: "logs" },
  { method: "GET", path: "/core/logs", kind: "logs" },
  { method: "GET", path: "/host/logs", kind: "logs" },
  { method: "GET", path: "/host/logs/identifiers/{identifier}", kind: "logs" },
  { method: "GET", path: "/addons/{slug}/logs", kind: "logs" },
  { method: "POST", path: "/store/addons/{slug}/install", kind: "install" },
  { method: "POST", path: "/addons/{slug}/uninstall", kind: "uninstall" },
  { method: "POST", path: "/addons/{slug}/options", kind: "options" },
];

const PLACEHOLDERS: Record<string, string> = {
  "{slug}": "(?<slug>[A-Za-z0-9][A-Za-z0-9_-]{0,63})",
  "{identifier}": "(?<identifier>[A-Za-z0-9][A-Za-z0-9_.@-]{0,63})",
};

const COMPILED = SUPERVISOR_ROUTES.map((route) => ({
  route,
  pattern: new RegExp(
    "^" + route.path.replace(/\{slug\}|\{identifier\}/g, (name) => PLACEHOLDERS[name]!) + "$",
  ),
}));

export interface SupervisorMatch {
  route: SupervisorRoute;
  slug?: string;
}

// matchSupervisorRoute: the listed route for the exact method and raw path,
// else null. The path is matched as sent (no decoding), so a query, a
// %-escape, a dot segment, or a doubled or trailing slash never matches.
export function matchSupervisorRoute(method: string, path: string): SupervisorMatch | null {
  for (const { route, pattern } of COMPILED) {
    const match = route.method === method ? pattern.exec(path) : null;
    if (!match) {
      continue;
    }
    const slug = match.groups?.slug;
    if (slug !== undefined && slug.toLowerCase() === "self") {
      return null;
    }
    return slug === undefined ? { route } : { route, slug };
  }
  return null;
}

// ownAppSlug: HA NOVA's own App (store or local build). The handler also
// compares against the slug Supervisor reports for this App.
export function ownAppSlug(slug: string): boolean {
  return slug.toLowerCase().includes("ha_nova_relay");
}

// The CLI's own marker (cli/relay_raw_redact.go secretHidden), so its
// hidden-value check refuses a write that carries one back.
export const HIDDEN_VALUE = "(hidden: a stored code, password, or key)";

const SECRET_WORDS = new Set([
  "pass", "pw", "pwd", "key", "keys", "psk", "pin", "llat", "cert", "certs",
  "certificate", "salt", "otp", "totp", "auth", "secret", "secrets", "token", "tokens",
]);

// secretKey: a key whose value may be a credential. Broad on purpose: a
// hidden harmless value costs nothing, a shown password costs the home.
export function secretKey(key: string): boolean {
  const lower = key.toLowerCase();
  if (/password|passwd|passphrase|secret|token|credential|apikey|api_key|private|privkey/.test(lower)) {
    return true;
  }
  return lower.split(/[^a-z0-9]+/).some((word) => SECRET_WORDS.has(word));
}

// redactSecretValues hides every value under a secret-like key, at any depth;
// booleans and null stay (a "password set" flag is no secret).
export function redactSecretValues(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(redactSecretValues);
  }
  if (value === null || typeof value !== "object") {
    return value;
  }
  const out: Record<string, unknown> = {};
  for (const [key, inner] of Object.entries(value as Record<string, unknown>)) {
    out[key] =
      secretKey(key) && inner !== null && typeof inner !== "boolean"
        ? HIDDEN_VALUE
        : redactSecretValues(inner);
  }
  return out;
}

// redactAppOptions hides, in an App's info reply, every option the App's own
// schema marks as a password, before the key-based redaction runs.
export function redactAppOptions(body: unknown): unknown {
  const data = (body as { data?: unknown } | null)?.data as
    | { options?: unknown; schema?: unknown }
    | undefined;
  if (!data || typeof data !== "object" || !data.options || typeof data.options !== "object") {
    return body;
  }
  const options = { ...(data.options as Record<string, unknown>) };
  for (const node of Array.isArray(data.schema) ? data.schema : []) {
    const name = (node as { name?: unknown }).name;
    if ((node as { format?: unknown }).format === "password" && typeof name === "string" && name in options && options[name] !== null) {
      options[name] = HIDDEN_VALUE;
    }
  }
  return { ...(body as object), data: { ...data, options } };
}

// holdsHiddenValue: a write carrying the marker back would store it.
export function holdsHiddenValue(value: unknown): boolean {
  if (value === HIDDEN_VALUE) {
    return true;
  }
  if (value === null || typeof value !== "object") {
    return false;
  }
  return Object.values(value as Record<string, unknown>).some(holdsHiddenValue);
}
