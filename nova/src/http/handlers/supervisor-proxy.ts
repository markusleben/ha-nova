import { HttpError } from "../errors.js";
import type { RouteContext, RouteHandler } from "../router.js";
import {
  holdsHiddenValue,
  matchSupervisorRoute,
  ownAppSlug,
  HIDDEN_VALUE,
  redactAppInfo,
  redactSecretValues,
  type SupervisorMatch,
} from "./supervisor-allowlist.js";

// POST /supervisor {method, path, data?, lines?}: the one way HA NOVA reaches
// the Supervisor API (docs/work/2026-10-26-supervisor-role-spec.md →
// Decisions). Only listed routes are sent; every JSON reply is redacted here,
// so a stored App credential never leaves the Relay. App mode only: a
// standalone Relay has no Supervisor token and no such route (404).

const SUPERVISOR_BASE = "http://supervisor";
// Below the CLI's 30 s write bound, so a slow uninstall comes back as
// SUPERVISOR_OUTCOME_UNKNOWN (it may still finish), never as a dropped call.
const REQUEST_TIMEOUT_MS = 25_000;
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
    const { data, stored } = await writeBody(match, request.data, upstream);
    const reply = await jsonReply(
      await upstream(request.path, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(data) }),
      match,
    );
    return reply.status < 400 || !stored ? reply : { status: reply.status, body: scrubbed(reply.body, stored) };
  };
}

// scrubbed: a refused options write's reply without the values sent. The
// Supervisor's validation message quotes the whole options object, stored
// credentials included.
function scrubbed(body: unknown, stored: Record<string, unknown>): unknown {
  const values: string[] = [];
  const collect = (v: unknown): void => {
    if (typeof v === "string" && v.length >= 3) {
      values.push(v);
    } else if (v && typeof v === "object") {
      Object.values(v as Record<string, unknown>).forEach(collect);
    }
  };
  collect(stored);
  const raw = body as { result?: unknown; message?: unknown } | null;
  let message = typeof raw?.message === "string" ? raw.message : "the Supervisor refused the settings";
  for (const value of values.sort((a, b) => b.length - a.length)) {
    message = message.split(value).join(HIDDEN_VALUE);
  }
  return { result: raw?.result ?? "error", message };
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
async function writeBody(match: SupervisorMatch, data: Record<string, unknown> | undefined, upstream: Upstream): Promise<{ data: Record<string, unknown>; stored?: Record<string, unknown> }> {
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
      return { data: { background: true } }; // the image pull can take minutes; the skill polls the App's state
    case "uninstall":
      if (keys.some((key) => key !== "remove_config") || (keys.length === 1 && typeof given.remove_config !== "boolean")) {
        throw validation("an uninstall takes only {remove_config: true|false}");
      }
      return { data: given };
  }
  if (keys.length === 1 && keys[0] === "auto_update" && typeof given.auto_update === "boolean") {
    return { data: { auto_update: given.auto_update } };
  }
  const changes = given.options;
  if (keys.length !== 1 || keys[0] !== "options" || !changes || typeof changes !== "object" || Array.isArray(changes)) {
    throw validation("an options change takes exactly {options: {...}} (the keys to change) or {auto_update: true|false}");
  }
  if (JSON.stringify(changes).includes('"!secret')) {
    throw refused("a !secret reference would hand a stored secret to the App; set that option in the App's Configuration tab");
  }
  const info = await upstream("/addons/" + match.slug! + "/info");
  const body = await readJson(info, "the App's stored options");
  const stored = (body as { data?: { options?: unknown } } | null)?.data?.options as Record<string, unknown> | undefined;
  if (!info.ok || !stored || typeof stored !== "object" || Array.isArray(stored)) {
    throw new HttpError(502, "SUPERVISOR_OPTIONS_UNREADABLE", "cannot read the App's stored options, so nothing was sent");
  }
  // A list or object option is replaced whole; one holding a hidden value
  // would silently lose it.
  const shown = ((redactAppInfo(body) as { data: { options: Record<string, unknown> } }).data.options);
  for (const key of Object.keys(changes)) {
    if (stored[key] && typeof stored[key] === "object" && JSON.stringify(shown[key]) !== JSON.stringify(stored[key])) {
      throw refused("the option '" + key + "' holds a hidden value and is replaced whole; change it in the App's Configuration tab");
    }
  }
  return { data: { options: { ...stored, ...(changes as Record<string, unknown>) } }, stored };
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
    if (init?.method === "POST" && error instanceof Error && error.name === "TimeoutError") {
      throw new HttpError(504, "SUPERVISOR_OUTCOME_UNKNOWN", "the Supervisor did not answer in time and may still finish; read the App's state again");
    }
    const message = error instanceof Error && error.message ? error.message : "request failed";
    throw new HttpError(502, "UPSTREAM_SUPERVISOR_ERROR", "Supervisor request failed: " + message);
  }
}

async function jsonReply(response: Response, match: SupervisorMatch): Promise<{ status: number; body: unknown }> {
  if (!response.ok && !(response.headers.get("content-type") ?? "").includes("json")) {
    return { status: response.status, body: (await readText(response, 4096)).text }; // a middleware refusal is plain text
  }
  const body = await readJson(response, "the Supervisor reply");
  return { status: response.status, body: match.route.path === "/addons/{slug}/info" ? redactAppInfo(body) : redactSecretValues(body) };
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
    const json = (response.headers.get("content-type") ?? "").includes("json");
    return { status: response.status, body: json ? redactSecretValues(await readJson(response, "the Supervisor reply")) : (await readText(response, 4096)).text };
  }
  const read = await readText(response, MAX_LOG_BYTES);
  // The Supervisor logs an App's whole options when they fail validation.
  const text = read.text.replace(/(invalid options:).*$/gm, "$1 " + HIDDEN_VALUE);
  return read.truncated ? { status: response.status, body: text, truncated: true } : { status: response.status, body: text };
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
