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
  "pass", "pw", "pwd", "key", "keys", "psk", "pin", "code", "codes", "cred", "creds", "llat", "cert", "certs",
  "certificate", "salt", "otp", "totp", "auth", "secret", "secrets", "token", "tokens",
]);

// secretKey: a key whose value may be a credential. Broad on purpose: a
// hidden harmless value costs nothing, a shown password costs the home.
// camelCase splits into words (adminPin → admin, pin); the Supervisor's own
// error_key stays readable.
export function secretKey(key: string): boolean {
  const lower = key.replace(/([a-z0-9])([A-Z])/g, "$1_$2").toLowerCase();
  if (lower === "error_key") {
    return false;
  }
  if (/password|passwd|passphrase|secret|token|credential|apikey|api_key|private|privkey/.test(lower) || /(pass|pw|pwd|pin|key|code)$/.test(lower)) {
    return true;
  }
  return lower.split(/[^a-z0-9]+/).some((word) => SECRET_WORDS.has(word));
}

// A URL with user info (https://user:token@host/…): a repository's
// credentials ride in it.
const URL_USERINFO = /^([a-z][a-z0-9+.-]*:\/\/)[^/?#\s]*@/i; // through the last "@" of the authority

// redactSecretValues hides every value under a secret-like key, at any depth,
// and the user info of any URL; booleans and null stay (a "password set" flag
// is no secret).
export function redactSecretValues(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(redactSecretValues);
  }
  if (typeof value === "string") {
    return value.replace(URL_USERINFO, "$1" + HIDDEN_VALUE + "@");
  }
  if (value === null || typeof value !== "object") {
    return value;
  }
  const out: Record<string, unknown> = {};
  // An environment entry {name: "DB_PASSWORD", value: "…"} keeps its secret
  // under "value".
  const name = (value as { name?: unknown }).name;
  const named = typeof name === "string" && secretKey(name);
  for (const [key, inner] of Object.entries(value as Record<string, unknown>)) {
    out[key] =
      (secretKey(key) || (named && key === "value")) && inner !== null && typeof inner !== "boolean"
        ? HIDDEN_VALUE
        : redactSecretValues(inner);
  }
  return out;
}

// redactAppOptions hides, in an App's info reply, every option the App's own
// schema marks as a password, nested ones included, before the key-based
// redaction runs.
export function redactAppOptions(body: unknown): unknown {
  const data = (body as { data?: unknown } | null)?.data as
    | { options?: unknown; schema?: unknown }
    | undefined;
  if (!data || typeof data !== "object" || !data.options || typeof data.options !== "object") {
    return body;
  }
  return { ...(body as object), data: { ...data, options: hideBySchema(data.options, data.schema) } };
}

// hideBySchema walks the Supervisor's UI schema: a node is {name, type,
// format?, multiple?, schema?}; type "schema" nests the next level (each list
// element when multiple).
function hideBySchema(value: unknown, schema: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map((item) => hideBySchema(item, schema));
  }
  if (!value || typeof value !== "object" || !Array.isArray(schema)) {
    return value;
  }
  const out = { ...(value as Record<string, unknown>) };
  for (const node of schema as Array<{ name?: unknown; format?: unknown; schema?: unknown }>) {
    if (typeof node?.name !== "string" || !(node.name in out) || out[node.name] === null) {
      continue;
    }
    out[node.name] = node.format === "password" ? HIDDEN_VALUE : hideBySchema(out[node.name], node.schema);
  }
  return out;
}

// redactAppInfo: everything an App's info reply hides.
export function redactAppInfo(body: unknown): unknown {
  return redactSecretValues(redactAppOptions(body));
}

// holdsHiddenValue: a write carrying the marker back would store it.
export function holdsHiddenValue(value: unknown): boolean {
  if (typeof value === "string") {
    return value.includes(HIDDEN_VALUE); // a redacted URL carries it inside
  }
  if (value === null || typeof value !== "object") {
    return false;
  }
  return Object.values(value as Record<string, unknown>).some(holdsHiddenValue);
}
