import { createHash, randomBytes } from "node:crypto";

import type { Principal } from "../security/principal.js";
import { HttpError } from "./errors.js";
import type { RelayLogger } from "./server.js";
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
const ASKS_PER_HOUR = 10;
const HOUR_MS = 3_600_000;
const PAUSE_AFTER = 3; // asks in a row that end cancelled or unanswered
const PAUSE_MS = 900_000;
const ENDPOINTS: readonly GateEndpoint[] = ["ws", "core", "files", "backups", "supervisor"];

export interface GateRequest {
  endpoint: GateEndpoint;
  body: Buffer;
}

// The worker's /ask answer (cli/gate_ask.go): title, body, and button are the
// phone's; items are the page's, one per request, worded only when typed.
export interface AskItem {
  verdict: "pass" | "typed" | "refuse";
  why?: string;
  text?: string;
  detail?: string;
  entity_ids?: string[];
}

export interface PreparedAsk {
  verdict: "pass" | "typed" | "refuse";
  lang?: string;
  title?: string;
  body?: string;
  button?: string;
  items: AskItem[];
}

// A device's linked phone (§3), on the worker's wire (cli/gate_ask.go).
export interface PhoneLink {
  service: string;
  device_id: string;
  user_id: string;
  platform?: string;
}

export interface PhoneText {
  lang: string;
  title: string;
  body: string;
  button: string;
}

// The worker's /phone answer: approved names the administrator who tapped.
export type PhoneAnswer = { answer: "approved"; user_id: string } | { answer: "declined" | "rejected" | "none" | "unavailable" };

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
  stop: AbortController; // its phone request, if any, hangs up when it ends
  timer: ReturnType<typeof setTimeout>;
}

interface Owner {
  opened: number[]; // when its asks of the last hour opened
  misses: number; // its asks in a row that ended cancelled or unanswered
  pausedUntil: number; // its phone asks
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
  channels: (principal: Principal) => string[]; // where a person can approve now, besides the phone
  link: (principal: Principal) => PhoneLink | undefined; // its linked phone, read fresh
  phone: (link: PhoneLink, text: PhoneText, waitMs: number, stop: AbortSignal) => Promise<PhoneAnswer | null>;
  now: () => number;
  logger?: Pick<RelayLogger, "info">;
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

const sameLink = (a: PhoneLink | undefined, b: PhoneLink): boolean =>
  a !== undefined && a.service === b.service && a.device_id === b.device_id && a.user_id === b.user_id;

export function createGateAsks(deps: GateAsksDeps): GateAsks {
  const asks = new Map<string, Ask>();
  const grants = new Map<string, Grant>();
  const preparing = new Set<string>(); // owners whose ask the worker is rating now
  const owners = new Map<string, Owner>();
  const ownerState = (owner: string): Owner => {
    let state = owners.get(owner);
    if (state === undefined) {
      state = { opened: [], misses: 0, pausedUntil: 0 };
      owners.set(owner, state);
    }
    return state;
  };

  // An ask ends once: its phone hangs up, and its end counts toward a pause.
  const settle = (ask: Ask, state: AskState): void => {
    if (ask.state.state !== "waiting") {
      return;
    }
    ask.state = state;
    clearTimeout(ask.timer);
    ask.stop.abort();
    const owner = ownerState(ask.owner);
    if (state.state === "approved") {
      owner.misses = 0;
    } else if (++owner.misses >= PAUSE_AFTER) {
      owner.misses = 0;
      owner.pausedUntil = deps.now() + PAUSE_MS;
    }
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
    for (const [id, owner] of owners) {
      owner.opened = owner.opened.filter((at) => now - at < HOUR_MS);
      if (owner.opened.length === 0 && owner.misses === 0 && now >= owner.pausedUntil) {
        owners.delete(id);
      }
    }
  };
  const grant = (ask: Ask): void => {
    const grantId = randomBytes(32).toString("base64url");
    const until = deps.now() + GRANT_MS;
    grants.set(key(grantId), { owner: ask.owner, until, unused: [...ask.digests] });
    settle(ask, { state: "approved", grant_id: grantId, until });
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
    const owner = ownerOf(principal);
    prune();
    if ((owners.get(owner)?.opened.length ?? 0) >= ASKS_PER_HOUR) {
      throw new HttpError(429, "GATE_ASK_LIMIT", `This device asked ${ASKS_PER_HOUR} times in the last hour; try again later. Nothing was asked or sent`);
    }
    if (preparing.has(owner)) {
      throw new HttpError(429, "GATE_ASK_BUSY", "An ask from this device is still being prepared; nothing was asked or sent");
    }
    preparing.add(owner);
    const prepared = await deps.ask(deps.deviceName(principal), requests).finally(() => preparing.delete(owner));
    if (prepared === null) {
      throw new HttpError(503, "GATE_UNAVAILABLE", "HA NOVA's safety check is not available; nothing was asked or sent");
    }
    if (prepared.verdict === "refuse") {
      throw new HttpError(422, "GATE_REFUSED", "HA NOVA's safety check refuses one of these requests; nothing was asked or sent");
    }
    if (prepared.verdict === "pass") {
      return { needed: false };
    }
    prune();
    for (const ask of asks.values()) {
      if (ask.owner === owner && ask.state.state === "waiting") {
        settle(ask, { state: "cancelled" }); // one open ask per device: a new one replaces it (and may pause the phone)
      }
    }
    const link = deps.link(principal);
    const paused = deps.now() < (owners.get(owner)?.pausedUntil ?? 0);
    const channels = [...(link !== undefined && !paused ? ["phone"] : []), ...deps.channels(principal)];
    if (channels.length === 0 && link !== undefined) {
      throw new HttpError(429, "GATE_PAUSED", "Phone asks from this device are paused for 15 minutes after three in a row went unanswered or were cancelled; nothing was sent");
    }
    if (channels.length === 0) {
      throw new HttpError(409, "GATE_NO_CHANNEL", "No one can confirm this now: no linked phone answers and the approval page is not available; nothing was sent");
    }
    const askId = randomBytes(32).toString("base64url");
    const expiresAt = deps.now() + ASK_MS;
    const ask: Ask = {
      owner,
      digests: requests.filter((_, i) => prepared.items[i]?.verdict === "typed").map((r) => gateDigest(r.endpoint, r.body)),
      prepared,
      expiresAt,
      state: { state: "waiting", expires_at: expiresAt },
      waiters: new Set(),
      stop: new AbortController(),
      timer: setTimeout(() => settle(ask, { state: "expired" }), ASK_MS),
    };
    ask.timer.unref?.();
    asks.set(key(askId), ask);
    ownerState(owner).opened.push(deps.now());
    if (link !== undefined && channels.includes("phone")) {
      const text = { lang: prepared.lang ?? "", title: prepared.title ?? "", body: prepared.body ?? "", button: prepared.button ?? "" };
      void deps.phone(link, text, ASK_MS, ask.stop.signal).then((answer) => {
        if (ask.state.state !== "waiting" || !sameLink(deps.link(principal), link)) {
          return; // ended, or the link changed or went with its device
        }
        if (answer?.answer === "approved" && answer.user_id === link.user_id) {
          grant(ask);
          deps.logger?.info?.("A typed request was approved on the linked phone", { user_id: answer.user_id });
        } else if (answer?.answer === "declined" || answer?.answer === "rejected") {
          settle(ask, { state: "cancelled" });
        }
      });
    }
    return { needed: true, ask_id: askId, expires_at: expiresAt, channels };
  };

  // Long-polls up to POLL_MS (or the ask's end) for a change from waiting.
  // A caller that hangs up ends its poll (the response's close, not the request's).
  const wait = async (ask: Ask, { response }: RouteContext): Promise<AskState> => {
    if (ask.state.state === "waiting" && !response.destroyed) {
      await new Promise<void>((resolve) => {
        const done = (): void => {
          clearTimeout(timer);
          ask.waiters.delete(done);
          response.off("close", done);
          resolve();
        };
        const timer = setTimeout(done, Math.max(0, Math.min(deps.pollMs ?? POLL_MS, ask.expiresAt - deps.now())));
        ask.waiters.add(done);
        response.once("close", done);
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
      grant(ask);
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
export function gateWithAsks(gate: GateClient, deps: Omit<GateAsksDeps, "ask" | "phone">): { gate: GateClient; ask: RouteHandler; asks: GateAsks } {
  const asks = createGateAsks({
    ...deps,
    ask: async (device, requests) => (await gate.ask?.(device, requests)) ?? null,
    phone: async (...a) => (await gate.phone?.(...a)) ?? null,
  });
  return { gate: { check: (...a) => gate.check(...a), redact: (...a) => gate.redact(...a), consume: asks.consume }, ask: asks.handler, asks };
}
