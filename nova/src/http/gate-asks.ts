import { randomInt } from "node:crypto";

import type { Principal } from "../security/principal.js";
import type { CodeMode } from "../security/window-confirm.js";
import type { PageTextSource } from "./page-texts.js";
import type { GateRequest, LinkWords, PageAsk, PhoneAnswer, PhoneLink, PhoneText, PreparedAsk, WindowEnd } from "./gate-ask-types.js";
import { ASK_MS, createAskTable } from "./gate-ask-table.js";
import { HttpError } from "./errors.js";
import { createGateGrants, gateDigest, idKey } from "./gate-grants.js";
import { createOwnerLimits, PAUSED, type Miss } from "./gate-owners.js";
import type { RelayLogger } from "./server.js";
import type { WindowLogEntry } from "../storage/window-log.js";
import type { GateClient, GateEndpoint } from "./gate.js";
import type { RouteHandler } from "./router.js";

export type { AskItem, AskState, GateRequest, LinkWords, PageAsk, PhoneAnswer, PhoneLink, PhoneText, PreparedAsk, WindowEnd } from "./gate-ask-types.js";
export { gateDigest };

// Asks and grants (docs/work/2026-11-03-relay-typed-gate-spec.md §3, §4). A
// device asks for exactly the request bytes it will send; the worker rates
// and words them; a person approves on a channel (phone, page: later
// layers); the approval becomes a grant of those bytes' digests, each usable
// once by that device for two minutes. The Relay decides nothing itself, and
// keeps asks and grants in memory only: a restart drops them and nothing runs.

const MAX_ITEMS = 20;
const PHONE_PATH = "/gate/v1/phone";
const ENDPOINTS: readonly GateEndpoint[] = ["ws", "core", "files", "backups", "supervisor"];

export interface GateAsksDeps {
  ask: (device: string, requests: GateRequest[]) => Promise<PreparedAsk | null>;
  prepare: Partial<Record<GateEndpoint, (body: unknown) => unknown>>; // as the gated handler prepares it (gated())
  deviceName: (principal: Principal) => string;
  channels: (principal: Principal) => string[] | Promise<string[]>; // where a person can approve now, besides the phone
  link: (principal: Principal) => PhoneLink | undefined; // its linked phone, read fresh
  phone: (link: PhoneLink, text: PhoneText | LinkWords, waitMs: number, stop: AbortSignal, onSent?: () => void) => Promise<PhoneAnswer | null>;
  phoneEnd?: (end: WindowEnd) => Promise<boolean>;
  // Sets the device's link while it is active and still has current; none: no link route.
  setLink?: (principal: Principal, link: PhoneLink, current: PhoneLink | undefined) => boolean;
  linkable?: (principal: Principal) => boolean; // whose link setLink keeps (default: a device's)
  window?: (principal: Principal) => Promise<boolean>;
  windowNow?: (principal: Principal) => boolean; // the record still lets its window confirm, read with no wait before a grant
  windowOff?: (principal: Principal) => boolean;
  code?: (principal: Principal) => { mode: CodeMode; rev: number; userId: string }; // its chat code, read fresh (window-confirm.ts)
  codeOwner?: (userId: string) => Promise<boolean>; // still the owner; throws when unread
  legacyCleanupComplete?: (principal: Principal) => boolean; // the caller's own Mac cleanup report (spec §5.3)
  windowLog?: (entry: WindowLogEntry) => void; // a window win, for the NOVA page's recent list
  now: () => number;
  logger?: Pick<RelayLogger, "info" | "warn">;
  pollMs?: number; // tests
}

export interface GateAsks {
  handler: RouteHandler;
  // The page (§3, item 2): the asks waiting for a person, each by its ref (the
  // id's hash: it can approve or cancel, never wait), and the devices whose
  // phone asks are paused. approve and cancel: false when the ask isn't waiting.
  waiting(): PageAsk[];
  pausedDevices(): PausedDevice[];
  resume(deviceId: string): boolean; // its phone asks again at once; false: not paused
  approve(ref: string): boolean;
  cancel(ref: string): boolean;
  endCode(deviceId: string): void; // its waiting chat-code asks end (the owner turned the code off)
  // Marks one occurrence used before the request is forwarded (at most once).
  consume(principal: Principal, grantId: string, endpoint: GateEndpoint, body: Buffer): boolean;
}

// The page also reads its words from the worker (page-texts.ts).
export type GatePage = Pick<GateAsks, "waiting" | "pausedDevices" | "approve" | "cancel" | "resume"> & Partial<Pick<GateAsks, "endCode">> & { texts: PageTextSource; recent?: () => WindowLogEntry[]; devices?: () => string[] };

// A device whose phone asks are paused: until when, and the asks that paused it.
export interface PausedDevice {
  deviceId: string;
  until: number;
  misses: Miss[];
}

export const askRef = idKey; // an ask's ref on the page

function ownerOf(principal: Principal): string {
  return principal.kind === "device" ? `device:${principal.deviceId}` : "shared";
}

const sameLink = (a: PhoneLink | undefined, b: PhoneLink): boolean =>
  a !== undefined && a.service === b.service && a.device_id === b.device_id && a.user_id === b.user_id;

export function createGateAsks(deps: GateAsksDeps): GateAsks {
  const grants = createGateGrants(deps.now);
  const limits = createOwnerLimits(deps.now);
  const { prune, settle, start, grant, find, get, entries, replaceOpen, wait } = createAskTable(deps.now, grants, limits, deps.pollMs);
  const preparing = new Set<string>(); // owners whose ask the worker is rating now
  let windowSeq = 0;

  const limit = (owner: string): void => {
    prune();
    limits.limit(owner);
  };
  const paused = (owner: string): boolean => limits.paused(owner);

  // A phone over its hourly cap is paused like an unanswered run: callers then go on with the other channels.
  const phoneUnavailable = (owner: string): HttpError => new HttpError(429, "GATE_PAUSED", paused(owner) ? PAUSED
    : "Phone asks from this device are paused: its phone was asked ten times in the last hour; nothing was sent");

  const open = async (principal: Principal, raw: unknown, presence: unknown, acceptCode: boolean): Promise<unknown> => {
    if (presence !== undefined && presence !== "present" && presence !== "away" && presence !== "unknown") {
      throw new HttpError(400, "VALIDATION_ERROR", "Presence must be present, away, or unknown");
    }
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
    limit(owner);
    if (preparing.has(owner)) {
      throw new HttpError(429, "GATE_ASK_BUSY", "An ask from this device is still being prepared; nothing was asked or sent");
    }
    preparing.add(owner);
    limits.count(owner);
    const device = deps.deviceName(principal);
    const before = acceptCode && principal.kind === "device" ? deps.code?.(principal) : undefined; // only a CLI that saw /health code: 1 asks for one
    const [prepared, others, window, owns] = await Promise.all([
      deps.ask(device, requests), deps.channels(principal),
      Promise.resolve().then(() => deps.window?.(principal)).catch(() => false),
      before?.mode === "on" ? deps.codeOwner!(before.userId).catch(() => false) : false,
    ]).finally(() => preparing.delete(owner));
    if (prepared === null) {
      throw new HttpError(503, "GATE_UNAVAILABLE", "HA NOVA's safety check is not available; nothing was asked or sent");
    }
    if (prepared.verdict === "refuse") {
      throw new HttpError(422, "GATE_REFUSED", "HA NOVA's safety check refuses one of these requests; nothing was asked or sent");
    }
    if (prepared.verdict === "pass") {
      return { needed: false };
    }
    replaceOpen(owner);
    const link = deps.link(principal);
    const now = before === undefined ? undefined : deps.code?.(principal);
    const code = owns === true && now?.mode === "on" && now.rev === before!.rev && now.userId === before!.userId
      ? { digits: String(randomInt(10_000)).padStart(4, "0"), rev: now.rev, userId: now.userId, wrong: 0 } : undefined;
    const channels = [...(link !== undefined && limits.phoneAvailable(owner) ? ["phone"] : []), ...others, ...(principal.kind === "device" && window === true ? ["window"] : []), ...(code ? ["code"] : [])];
    if (channels.length === 0 && link !== undefined) {
      throw phoneUnavailable(owner);
    }
    if (channels.length === 0) {
      throw new HttpError(409, "GATE_NO_CHANNEL", "No one can confirm this now: no linked administrator's phone answers, and no approval page is available here; nothing was sent");
    }
    const digests = requests.filter((_, i) => prepared.items[i]?.verdict === "typed").map((r) => gateDigest(r.endpoint, r.body));
    const { askId, ask } = start(owner, digests, prepared);
    if (code !== undefined) ask.code = code;
    if (link !== undefined && channels.includes("phone")) {
      let sent = false;
      ask.sendPhone = () => {
        prune();
        if (sent || ask.state.state !== "waiting") return;
        sent = true;
        clearTimeout(ask.phoneTimer);
        const release = limits.reservePhone(owner);
        if (release === undefined) return;
        const text = { ask_ref: idKey(askId), lang: prepared.lang ?? "", title: prepared.title ?? "", body: prepared.body ?? "", button: prepared.button ?? "" };
        void deps.phone(link, text, ask.expiresAt - deps.now(), ask.stop.signal, () => {
          if (ask.state.state === "waiting") ask.phoneAt = deps.now();
        }).then((answer) => {
          if (answer?.not_asked === true) release();
          prune(); // an answer past the deadline finds the ask expired
          if (ask.state.state !== "waiting" || !sameLink(deps.link(principal), link)) {
            return; // ended, or the link changed or went with its device
          }
          if (answer?.answer === "approved" && answer.user_id === link.user_id) {
            grant(ask);
            deps.logger?.info?.("A typed request was approved on the linked phone", { user_id: answer.user_id });
          } else if (answer?.answer === "declined" || answer?.answer === "rejected") {
            settle(ask, { state: "cancelled" }, true);
          } else if ((answer === null || answer.answer === "unavailable") && others.length === 0 && !channels.includes("window")) {
            settle(ask, { state: "unavailable" }); // no weaker channel: nobody can approve it now
          }
        });
      };
      const delay = !channels.includes("window") || presence === undefined || presence === "away" ? 0 : presence === "present" ? 60_000 : 20_000; // no presence: no window shown here
      if (delay === 0) ask.sendPhone();
      else {
        ask.phoneTimer = setTimeout(ask.sendPhone, delay);
        ask.phoneTimer.unref?.();
      }
    }
    return { needed: true, ask_id: askId, expires_at: ask.expiresAt, channels, ...(code ? { code: code.digits } : {}) }; // the code's only answer
  };

  // A link ask (§3): the new phone's tap, after the tap of the phone linked
  // now, sets the device's link; a link changed meanwhile sets nothing.
  const openLink = (principal: Principal, raw: unknown): unknown => {
    if (deps.setLink === undefined || !(deps.linkable?.(principal) ?? principal.kind === "device")) {
      throw new HttpError(404, "NOT_FOUND", "Not found");
    }
    const { service, device_id, user_id, platform } = (typeof raw === "object" && raw !== null ? raw : {}) as Record<string, unknown>;
    const id = (v: unknown): v is string => typeof v === "string" && v !== "" && v.length <= 128; // as the registry keeps it
    if (!id(service) || !/^mobile_app_[a-z0-9_]+$/.test(service) || !id(device_id) || !id(user_id) || (platform !== undefined && platform !== "ios" && platform !== "android")) {
      throw new HttpError(400, "VALIDATION_ERROR", "Send link with service (mobile_app_…), device_id, user_id, and optionally platform (ios or android)");
    }
    const link: PhoneLink = { service, device_id, user_id, ...(platform !== undefined ? { platform } : {}) };
    const current = deps.link(principal);
    if (sameLink(current, link)) {
      return { needed: false };
    }
    const owner = ownerOf(principal);
    limit(owner);
    if (preparing.has(owner)) {
      throw new HttpError(429, "GATE_ASK_BUSY", "An ask from this device is still being prepared; nothing was asked or sent");
    }
    limits.count(owner);
    replaceOpen(owner);
    if (!limits.phoneAvailable(owner)) throw phoneUnavailable(owner);
    const device = deps.deviceName(principal);
    const { askId, ask } = start(owner, []);
    const words: LinkWords = { words: "link", device, ...(current !== undefined ? { current } : {}) };
    const release = limits.reservePhone(owner)!;
    void deps.phone(link, words, ASK_MS, ask.stop.signal, () => {
      if (ask.state.state === "waiting") ask.phoneAt = deps.now();
    }).then((answer) => {
      if (answer?.not_asked === true) release();
      prune();
      if (ask.state.state !== "waiting") {
        return;
      }
      if (answer?.answer === "approved" && answer.user_id === link.user_id) {
        let linked = false;
        try {
          linked = deps.setLink?.(principal, link, current) === true;
        } catch (error) {
          deps.logger?.warn?.("A phone link could not be saved; nothing was set", { error: String(error) }); // the disk, not the person: it stays unlinked
        }
        settle(ask, { state: linked ? "linked" : "cancelled" }, true);
        deps.logger?.info?.(linked ? "A device's phone link was set on the phone" : "A phone link changed meanwhile; nothing was set", { user_id: answer.user_id });
      } else if (answer?.answer === "declined" || answer?.answer === "rejected") {
        settle(ask, { state: "cancelled" }, true);
      } else if (answer === null || answer.answer === "unavailable") {
        settle(ask, { state: "unavailable" });
      }
    });
    return { needed: true, ask_id: askId, expires_at: ask.expiresAt, channels: ["phone"] };
  };

  return {
    async handler(context) {
      const { principal } = context;
      if (principal === undefined || (principal.kind === "device" && principal.internal !== undefined)) {
        throw new HttpError(404, "NOT_FOUND", "Not found");
      }
      const body = (context.body ?? {}) as { requests?: unknown; ask_id?: unknown; op?: unknown; link?: unknown; presence?: unknown; accept_code?: unknown; code?: unknown };
      if (context.path === PHONE_PATH) {
        return openLink(principal, body.link);
      }
      if (body.requests !== undefined) {
        return await open(principal, body.requests, body.presence, body.accept_code === true);
      }
      if (body.op === "window_off") {
        if (principal.kind !== "device" || deps.windowOff?.(principal) !== true) {
          throw new HttpError(404, "NOT_FOUND", "No such paired computer");
        }
        return { window: false };
      }
      if (body.op === "legacy_cleanup_complete") {
        if (principal.kind !== "device" || deps.legacyCleanupComplete?.(principal) !== true) {
          throw new HttpError(404, "NOT_FOUND", "No such paired computer");
        }
        return { legacy_cleanup: "complete" };
      }
      const ask = find(ownerOf(principal), body.ask_id);
      if (body.op === "wait") {
        return await wait(ask, context);
      }
      if (body.op === "cancel") {
        if (ask.state.state === "waiting") {
          settle(ask, { state: "cancelled" });
        }
        return ask.state;
      }
      if (body.op === "away") {
        ask.sendPhone?.();
        return ask.state;
      }
      if (body.op === "window" || body.op === "code") { // the chat code (MAR-244) shares the window's checks, grant, and log
        const viaCode = body.op === "code";
        const off = viaCode ? "GATE_CODE_OFF" : "GATE_WINDOW_OFF";
        if (principal.kind !== "device" || ask.prepared === undefined) {
          throw new HttpError(409, off, "A phone link must be confirmed on the phone");
        }
        if (ask.state.state !== "waiting") return ask.state;
        const code = ask.code;
        if (viaCode && code === undefined) throw new HttpError(409, off, "This ask takes no chat code; nothing was approved");
        if (viaCode && body.code !== code!.digits) {
          if (++code!.wrong >= 3) settle(ask, { state: "cancelled" });
          throw new HttpError(409, "GATE_CODE_WRONG", "That code is wrong; nothing was approved");
        }
        let allowed = false;
        let unavailable = false;
        try {
          allowed = await (viaCode ? deps.codeOwner?.(code!.userId) : deps.window?.(principal)) === true;
        } catch {
          unavailable = true;
        }
        prune();
        if (ask.state.state !== "waiting") return ask.state;
        if (unavailable) {
          throw new HttpError(503, "GATE_UNAVAILABLE", "The Home Assistant owner could not be checked; nothing was approved");
        }
        const fresh = viaCode ? deps.code?.(principal) : undefined; // no wait from here to the grant
        if (!allowed || (viaCode ? fresh?.mode !== "on" || fresh.rev !== code!.rev || fresh.userId !== code!.userId : deps.windowNow?.(principal) === false)) {
          throw new HttpError(409, off, viaCode ? "This computer no longer confirms with a chat code; nothing was approved" : "This computer cannot confirm in its window; use your phone or the NOVA page");
        }
        if (grant(ask, true)) {
          const seq = ++windowSeq;
          try {
            deps.windowLog?.({ atMs: deps.now(), computer: { id: principal.deviceId, name: deps.deviceName(principal) }, title: ask.prepared.title ?? "", ...(viaCode ? { code: true as const } : {}) });
          } catch (error) {
            deps.logger?.warn?.("A window confirmation could not be logged; approval stands", { error: String(error) });
          }
          if (deps.phoneEnd === undefined) ask.stop.abort();
          else void Promise.resolve().then(async () => {
            const link = deps.link(principal);
            if (link === undefined) return;
            const recorded = await deps.phoneEnd!({ ask_ref: idKey(String(body.ask_id)), seq, outcome: "window", link,
              lang: ask.prepared!.lang ?? "", title: ask.prepared!.title ?? "", computer: { id: principal.deviceId, name: deps.deviceName(principal) } });
            if (!recorded) deps.logger?.warn?.("A window confirmation could not be recorded on the phone; approval stands");
          }).catch((error: unknown) => {
            deps.logger?.warn?.("A window confirmation could not be recorded on the phone; approval stands", { error: String(error) });
          }).finally(() => ask.stop.abort());
        }
        return ask.state;
      }
      throw new HttpError(400, "VALIDATION_ERROR", "Send requests, or ask_id with op wait or cancel");
    },
    waiting() {
      prune();
      return entries().flatMap(([ref, ask]) => (ask.prepared === undefined || ask.state.state !== "waiting" ? []
        : [{ ref, body: ask.prepared.body ?? "", from: ask.prepared.from ?? "", items: ask.prepared.items.filter((item) => item.verdict === "typed"), expiresAt: ask.expiresAt }]));
    },
    pausedDevices() {
      return limits.pauses().flatMap(({ owner, until, misses }) => (owner.startsWith("device:") ? [{ deviceId: owner.slice("device:".length), until, misses }] : []));
    },
    resume(deviceId) {
      return limits.resume(`device:${deviceId}`);
    },
    endCode(deviceId) {
      for (const [, ask] of entries()) if (ask.owner === `device:${deviceId}` && ask.code !== undefined) settle(ask, { state: "cancelled" });
    },
    approve(ref) {
      prune();
      const ask = get(ref);
      if (ask?.prepared === undefined || ask.state.state !== "waiting") {
        return false; // a link ask is set only on the phones (§3)
      }
      grant(ask);
      return true;
    },
    cancel(ref) {
      prune();
      const ask = get(ref);
      if (ask?.prepared === undefined || ask.state.state !== "waiting") {
        return false;
      }
      settle(ask, { state: "cancelled" }, true); // declined on the NOVA page
      return true;
    },
    consume(principal, grantId, endpoint, body) {
      prune();
      return grants.consume(ownerOf(principal), grantId, endpoint, body);
    },
  };
}

// gateWithAsks: the gate that forwards a typed request only on a grant, and
// the ask route (POST /gate/v1/ask, and POST /gate/v1/phone for a link) that leads to one.
export function gateWithAsks(gate: GateClient, deps: Omit<GateAsksDeps, "ask" | "phone" | "phoneEnd">): { gate: GateClient; ask: RouteHandler; asks: GateAsks } {
  const asks = createGateAsks({
    ...deps,
    ask: async (device, requests) => (await gate.ask?.(device, requests)) ?? null,
    phone: async (...a) => (await gate.phone?.(...a)) ?? null,
    ...(gate.phoneEnd === undefined ? {} : { phoneEnd: (end: WindowEnd) => gate.phoneEnd!(end) }),
  });
  return { gate: { check: (...a) => gate.check(...a), redact: (...a) => gate.redact(...a), consume: asks.consume }, ask: asks.handler, asks };
}
