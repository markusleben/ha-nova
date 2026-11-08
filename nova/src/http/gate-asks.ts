import { createHash, randomBytes } from "node:crypto";

import type { Principal } from "../security/principal.js";
import { HttpError } from "./errors.js";
import type { GateClient, GateEndpoint } from "./gate.js";
import type { RouteContext, RouteHandler } from "./router.js";

// Asks and grants (docs/work/2026-11-03-relay-typed-gate-spec.md §3, §4). A
// device asks for exactly the request bytes it will send; the worker rates
// and words them; a person approves on a channel (phone, page: later
// layers); the approval becomes a grant of those bytes' digests, each usable
// once by that device for two minutes. The Relay decides nothing itself, and
// keeps asks and grants in memory only: a restart drops them and nothing runs.

const ASK_MS = 180_000;
const GRANT_MS = 120_000;
const POLL_MS = 25_000;
const MAX_ITEMS = 20;
const ENDPOINTS: readonly GateEndpoint[] = ["ws", "core", "files", "backups", "supervisor"];

export interface GateRequest {
  endpoint: GateEndpoint;
  body: Buffer;
}

// The worker's /ask answer (cli/gate_ask.go): title, body, and button are the
// phone's; items are the page's.
export interface PreparedAsk {
  verdict: "pass" | "typed" | "refuse";
  why?: string;
  lang?: string;
  title?: string;
  body?: string;
  button?: string;
  items: unknown[];
}

export type AskState =
  | { state: "waiting"; expires_at: number }
  | { state: "approved"; grant_id: string; until: number }
  | { state: "cancelled" }
  | { state: "expired" };

interface Ask {
  owner: string;
  digests: string[];
  prepared: PreparedAsk;
  expiresAt: number;
  state: AskState;
  waiters: Set<() => void>;
}

interface Grant {
  owner: string;
  until: number;
  unused: string[]; // one per request, a repeated request once per occurrence
}

export interface GateAsksDeps {
  ask: (device: string, requests: GateRequest[]) => Promise<PreparedAsk | null>;
  prepare: Partial<Record<GateEndpoint, (body: unknown) => unknown>>; // as the gated handler prepares it (gated())
  deviceName: (principal: Principal) => string;
  channels: (principal: Principal) => string[]; // where a person can approve now
  now: () => number;
  pollMs?: number; // tests
}

export interface GateAsks {
  handler: RouteHandler;
  // A channel's approval (a later layer); false when the ask isn't waiting.
  approve(askId: string): boolean;
  // Marks one occurrence used before the request is forwarded (at most once).
  consume(principal: Principal, grantId: string, endpoint: GateEndpoint, body: Buffer): boolean;
}

export function gateDigest(endpoint: GateEndpoint, body: Buffer): string {
  return createHash("sha256").update(endpoint).update(Buffer.from([0])).update(body).digest("hex");
}

// Ids are looked up by their hash, so a lookup's timing says nothing about an id.
const key = (id: string): string => createHash("sha256").update(id).digest("hex");

function ownerOf(principal: Principal): string {
  return principal.kind === "device" ? `device:${principal.deviceId}` : "legacy";
}

export function createGateAsks(deps: GateAsksDeps): GateAsks {
  const asks = new Map<string, Ask>();
  const grants = new Map<string, Grant>();

  const settle = (ask: Ask, state: AskState): void => {
    ask.state = state;
    for (const wake of ask.waiters) {
      wake();
    }
    ask.waiters.clear();
  };
  const prune = (): void => {
    const now = deps.now();
    for (const [id, ask] of asks) {
      if (ask.state.state === "waiting" && now >= ask.expiresAt) {
        settle(ask, { state: "expired" });
      }
      if (ask.state.state !== "waiting" && now >= ask.expiresAt + GRANT_MS) {
        asks.delete(id);
      }
    }
    for (const [id, grant] of grants) {
      if (now > grant.until) {
        grants.delete(id);
      }
    }
  };
  const find = (principal: Principal, askId: unknown): Ask => {
    prune();
    const ask = typeof askId === "string" ? asks.get(key(askId)) : undefined;
    if (ask === undefined || ask.owner !== ownerOf(principal)) {
      throw new HttpError(404, "GATE_ASK_UNKNOWN", "No such ask from this device");
    }
    return ask;
  };

  const open = async (principal: Principal, raw: unknown): Promise<unknown> => {
    if (!Array.isArray(raw) || raw.length === 0 || raw.length > MAX_ITEMS) {
      throw new HttpError(400, "VALIDATION_ERROR", `An ask holds 1 to ${MAX_ITEMS} requests`);
    }
    const requests = raw.map((item): GateRequest => {
      const endpoint = (item as { endpoint?: unknown } | null)?.endpoint;
      const encoded = (item as { body_b64?: unknown } | null)?.body_b64;
      if (!ENDPOINTS.includes(endpoint as GateEndpoint) || typeof encoded !== "string") {
        throw new HttpError(400, "VALIDATION_ERROR", "Each request needs endpoint and body_b64");
      }
      let body: unknown;
      try {
        body = JSON.parse(Buffer.from(encoded, "base64").toString("utf8"));
      } catch {
        throw new HttpError(400, "VALIDATION_ERROR", "A request body is not JSON");
      }
      const prepare = deps.prepare[endpoint as GateEndpoint] ?? ((value: unknown) => value);
      return { endpoint: endpoint as GateEndpoint, body: Buffer.from(JSON.stringify(prepare(body) ?? null)) };
    });
    const prepared = await deps.ask(deps.deviceName(principal), requests);
    if (prepared === null) {
      throw new HttpError(503, "GATE_UNAVAILABLE", "HA NOVA's safety check is not available; nothing was asked or sent");
    }
    if (prepared.verdict === "refuse") {
      throw new HttpError(422, "GATE_REFUSED", "HA NOVA's safety check refuses one of these requests; nothing was asked or sent");
    }
    if (prepared.verdict === "pass") {
      return { needed: false };
    }
    const channels = deps.channels(principal);
    if (channels.length === 0) {
      throw new HttpError(409, "GATE_NO_CHANNEL", "No one can confirm this now: no linked phone answers and the approval page is not available; nothing was sent");
    }
    prune();
    const owner = ownerOf(principal);
    for (const ask of asks.values()) {
      if (ask.owner === owner && ask.state.state === "waiting") {
        settle(ask, { state: "cancelled" }); // one open ask per device: a new one replaces it
      }
    }
    const askId = randomBytes(32).toString("base64url");
    const expiresAt = deps.now() + ASK_MS;
    asks.set(key(askId), { owner, digests: requests.map((r) => gateDigest(r.endpoint, r.body)), prepared, expiresAt, state: { state: "waiting", expires_at: expiresAt }, waiters: new Set() });
    return { needed: true, ask_id: askId, expires_at: expiresAt, channels };
  };

  // Long-polls up to POLL_MS (or the ask's end) for a change from waiting.
  const wait = async (ask: Ask, context: RouteContext): Promise<AskState> => {
    if (ask.state.state === "waiting") {
      await new Promise<void>((resolve) => {
        const done = (): void => {
          clearTimeout(timer);
          ask.waiters.delete(done);
          context.request.off("close", done);
          resolve();
        };
        const timer = setTimeout(done, Math.max(0, Math.min(deps.pollMs ?? POLL_MS, ask.expiresAt - deps.now())));
        ask.waiters.add(done);
        context.request.once("close", done);
      });
      prune();
    }
    return ask.state;
  };

  return {
    async handler(context) {
      const { principal } = context;
      if (principal === undefined || (principal.kind === "device" && principal.internal !== undefined)) {
        throw new HttpError(404, "NOT_FOUND", "Not found");
      }
      const body = (context.body ?? {}) as { requests?: unknown; ask_id?: unknown; op?: unknown };
      if (body.requests !== undefined) {
        return await open(principal, body.requests);
      }
      const ask = find(principal, body.ask_id);
      if (body.op === "wait") {
        return await wait(ask, context);
      }
      if (body.op === "cancel") {
        if (ask.state.state === "waiting") {
          settle(ask, { state: "cancelled" });
        }
        return ask.state;
      }
      throw new HttpError(400, "VALIDATION_ERROR", "Send requests, or ask_id with op wait or cancel");
    },
    approve(askId) {
      prune();
      const ask = asks.get(key(askId));
      if (ask === undefined || ask.state.state !== "waiting") {
        return false;
      }
      const grantId = randomBytes(32).toString("base64url");
      const until = deps.now() + GRANT_MS;
      grants.set(key(grantId), { owner: ask.owner, until, unused: [...ask.digests] });
      settle(ask, { state: "approved", grant_id: grantId, until });
      return true;
    },
    consume(principal, grantId, endpoint, body) {
      prune();
      const id = key(grantId);
      const grant = grants.get(id);
      if (grant === undefined || grant.owner !== ownerOf(principal) || deps.now() > grant.until) {
        return false;
      }
      const at = grant.unused.indexOf(gateDigest(endpoint, body));
      if (at === -1) {
        return false;
      }
      grant.unused.splice(at, 1);
      if (grant.unused.length === 0) {
        grants.delete(id);
      }
      return true;
    },
  };
}

// gateWithAsks: the gate that forwards a typed request only on a grant, and
// the ask route (POST /gate/v1/ask) that leads to one.
export function gateWithAsks(gate: GateClient, deps: Omit<GateAsksDeps, "ask">): { gate: GateClient; ask: RouteHandler; asks: GateAsks } {
  const asks = createGateAsks({ ...deps, ask: async (device, requests) => (await gate.ask?.(device, requests)) ?? null });
  return { gate: { check: (...a) => gate.check(...a), redact: (...a) => gate.redact(...a), consume: asks.consume }, ask: asks.handler, asks };
}
