import { HttpError } from "../errors.js";
import type { RouteContext, RouteHandler } from "../router.js";
import {
  holdsHiddenValue,
  matchSupervisorRoute,
  ownAppSlug,
  redactAppOptions,
  redactSecretValues,
  type SupervisorMatch,
} from "./supervisor-allowlist.js";

// POST /supervisor {method, path, data?, lines?}: the one way HA NOVA reaches
// the Supervisor API (docs/work/2026-10-26-supervisor-role-spec.md →
// Decisions). Only listed routes are sent; every JSON reply is redacted here,
// so a stored App credential never leaves the Relay. App mode only: a
// standalone Relay has no Supervisor token and no such route (404).

const SUPERVISOR_BASE = "http://supervisor";
const REQUEST_TIMEOUT_MS = 60_000; // an uninstall waits for the container
const DEFAULT_LOG_LINES = 100;
const MAX_LOG_LINES = 1000;
const MAX_LOG_BYTES = 512 * 1024;
const MAX_JSON_BYTES = 4 * 1024 * 1024;

export interface SupervisorProxyHandlerOptions {
  token: string;
  base?: string;
}

interface SupervisorRequest {
  method: string;
  path: string;
  data?: Record<string, unknown>;
  lines?: number;
}

type Upstream = (path: string, init?: RequestInit) => Promise<Response>;

export function createSupervisorProxyHandler(options: SupervisorProxyHandlerOptions): RouteHandler {
  const base = options.base ?? SUPERVISOR_BASE;
  const token = options.token;
  return async ({ body }: RouteContext) => {
    const request = parseRequest(body);
    const match = matchSupervisorRoute(request.method, request.path);
    if (!match) {
      throw refused(`${request.method} ${request.path} is not on HA NOVA's Supervisor list`);
    }
    const upstream: Upstream = (path, init) => send(base, token, path, init);
    switch (match.route.kind) {
      case "read":
        if (request.data !== undefined || request.lines !== undefined) {
          throw validation("a read takes no data or lines");
        }
        return await jsonReply(await upstream(request.path), match);
      case "logs":
        if (request.data !== undefined) {
          throw validation("a log read takes lines, not data");
        }
        return await logsReply(await upstream(`${request.path}?lines=${request.lines ?? DEFAULT_LOG_LINES}&no_colors`, { headers: { accept: "text/plain" } }));
    }
    if (request.lines !== undefined) {
      throw validation("lines applies to log reads");
    }
    await rejectOwnApp(match.slug!, upstream);
    const data = await writeBody(match, request.data, upstream);
    return await jsonReply(
      await upstream(request.path, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(data) }),
      match,
    );
  };
}

function parseRequest(body: unknown): SupervisorRequest {
  if (!body || typeof body !== "object" || Array.isArray(body)) {
    throw validation("the body must be {method, path, data?, lines?}");
  }
  const raw = body as Record<string, unknown>;
  for (const key of Object.keys(raw)) {
    if (!["method", "path", "data", "lines"].includes(key)) {
      throw validation("unknown field '" + key + "'");
    }
  }
  if ((raw.method !== "GET" && raw.method !== "POST") || typeof raw.path !== "string") {
    throw validation("method must be GET or POST and path a string");
  }
  if (raw.data !== undefined && (raw.data === null || typeof raw.data !== "object" || Array.isArray(raw.data))) {
    throw validation("data must be an object");
  }
  if (raw.lines !== undefined && (!Number.isInteger(raw.lines) || (raw.lines as number) < 1 || (raw.lines as number) > MAX_LOG_LINES)) {
    throw validation("lines must be an integer from 1 to " + MAX_LOG_LINES);
  }
  return raw as unknown as SupervisorRequest;
}

// rejectOwnApp: HA NOVA never installs, removes, or reconfigures the App it
// runs in; the slug Supervisor reports for it is read live and fails closed.
async function rejectOwnApp(slug: string, upstream: Upstream): Promise<void> {
  if (ownAppSlug(slug)) {
    throw refused("HA NOVA does not install, remove, or reconfigure its own App; do that in Home Assistant (Settings > Apps)");
  }
  const self = await upstream("/addons/self/info");
  const own = ((await readJson(self, "the Relay's own App info")) as { data?: { slug?: unknown } } | null)?.data?.slug;
  if (!self.ok || typeof own !== "string" || own === "") {
    throw new HttpError(502, "SUPERVISOR_SELF_UNREADABLE", "cannot read which App this Relay runs in, so nothing was sent");
  }
  if (slug === own) {
    throw refused("HA NOVA does not install, remove, or reconfigure its own App; do that in Home Assistant (Settings > Apps)");
  }
}

// writeBody: the exact body each write may send. An options change is merged
// onto the stored options (read here with the Relay's token), so a hidden
// credential keeps its value without ever leaving the Relay.
async function writeBody(match: SupervisorMatch, data: Record<string, unknown> | undefined, upstream: Upstream): Promise<Record<string, unknown>> {
  const given = data ?? {};
  const keys = Object.keys(given);
  if (holdsHiddenValue(given)) {
    throw refused("the request carries a value HA NOVA hid; leave that option out and it keeps its stored value");
  }
  switch (match.route.kind) {
    case "install":
      if (keys.length !== 0) {
        throw validation("an install takes no data");
      }
      return { background: true }; // the image pull can take minutes; the skill polls the App's state
    case "uninstall":
      if (keys.some((key) => key !== "remove_config") || (keys.length === 1 && typeof given.remove_config !== "boolean")) {
        throw validation("an uninstall takes only {remove_config: true|false}");
      }
      return given;
  }
  if (keys.length === 1 && keys[0] === "auto_update" && typeof given.auto_update === "boolean") {
    return { auto_update: given.auto_update };
  }
  const changes = given.options;
  if (keys.length !== 1 || keys[0] !== "options" || !changes || typeof changes !== "object" || Array.isArray(changes)) {
    throw validation("an options change takes exactly {options: {...}} (the keys to change) or {auto_update: true|false}");
  }
  const info = await upstream("/addons/" + match.slug! + "/info");
  const stored = ((await readJson(info, "the App's stored options")) as { data?: { options?: unknown } } | null)?.data?.options;
  if (!info.ok || !stored || typeof stored !== "object" || Array.isArray(stored)) {
    throw new HttpError(502, "SUPERVISOR_OPTIONS_UNREADABLE", "cannot read the App's stored options, so nothing was sent");
  }
  return { options: { ...(stored as Record<string, unknown>), ...(changes as Record<string, unknown>) } };
}

async function send(base: string, token: string, path: string, init?: RequestInit): Promise<Response> {
  try {
    return await fetch(base + path, {
      ...init,
      headers: { ...(init?.headers as Record<string, string> | undefined), authorization: "Bearer " + token },
      redirect: "error", // the Supervisor token never follows a redirect
      signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
    });
  } catch (error) {
    const message = error instanceof Error && error.message ? error.message : "request failed";
    throw new HttpError(502, "UPSTREAM_SUPERVISOR_ERROR", "Supervisor request failed: " + message);
  }
}

async function jsonReply(response: Response, match: SupervisorMatch): Promise<{ status: number; body: unknown }> {
  let body = await readJson(response, "the Supervisor reply");
  if (match.route.path === "/addons/{slug}/info") {
    body = redactAppOptions(body);
  }
  return { status: response.status, body: redactSecretValues(body) };
}

async function readJson(response: Response, what: string): Promise<unknown> {
  const { text, truncated } = await readText(response, MAX_JSON_BYTES);
  if (truncated) {
    throw new HttpError(502, "UPSTREAM_SUPERVISOR_ERROR", what + " is too large");
  }
  if (text === "") {
    return null;
  }
  try {
    return JSON.parse(text);
  } catch {
    throw new HttpError(502, "UPSTREAM_SUPERVISOR_ERROR", what + " is not JSON");
  }
}

async function logsReply(response: Response): Promise<{ status: number; body: unknown; truncated?: true }> {
  if (!response.ok) {
    return { status: response.status, body: redactSecretValues(await readJson(response, "the Supervisor reply")) };
  }
  const { text, truncated } = await readText(response, MAX_LOG_BYTES);
  return truncated ? { status: response.status, body: text, truncated: true } : { status: response.status, body: text };
}

// readText reads at most limit bytes and stops the stream there; a character
// cut in half at the limit is dropped.
async function readText(response: Response, limit: number): Promise<{ text: string; truncated: boolean }> {
  if (!response.body) {
    return { text: "", truncated: false };
  }
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  let truncated = false;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) {
      break;
    }
    if (size + value.byteLength > limit) {
      chunks.push(value.subarray(0, limit - size));
      truncated = true;
      await reader.cancel();
      break;
    }
    chunks.push(value);
    size += value.byteLength;
  }
  const text = new TextDecoder().decode(Buffer.concat(chunks));
  return { text: truncated ? text.replace(/\uFFFD+$/, "") : text, truncated };
}

function validation(message: string): HttpError {
  return new HttpError(400, "VALIDATION_ERROR", "Supervisor request: " + message);
}

function refused(message: string): HttpError {
  return new HttpError(403, "SUPERVISOR_REQUEST_REFUSED", message + "; nothing was sent");
}
