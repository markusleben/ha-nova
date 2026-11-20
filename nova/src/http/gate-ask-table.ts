import { randomBytes } from "node:crypto";

import type { AskState, PreparedAsk } from "./gate-ask-types.js";
import { HttpError } from "./errors.js";
import { GRANT_MS, idKey, type GateGrants } from "./gate-grants.js";
import type { OwnerLimits } from "./gate-owners.js";
import type { RouteContext } from "./router.js";

// The open asks (docs/work/2026-11-03-relay-typed-gate-spec.md §3): each ends
// once, and is kept a grant's lifetime past its deadline so a late wait still
// reads how it ended. Memory only: a restart drops them and nothing runs.

export const ASK_MS = 180_000;
const POLL_MS = 25_000;

export interface Ask {
  owner: string;
  digests: string[];
  prepared?: PreparedAsk; // none: a link ask
  expiresAt: number;
  state: AskState;
  waiters: Set<() => void>;
  stop: AbortController; // its phone request, if any, hangs up when it ends
  timer: ReturnType<typeof setTimeout>;
  phoneAt?: number;
  phoneTimer?: ReturnType<typeof setTimeout>;
  sendPhone?: () => void;
  code?: { digits: string; rev: number; userId: string; wrong: number }; // the chat code (MAR-244), never sent again
}

export interface AskTable {
  start(owner: string, digests: string[], prepared?: PreparedAsk): { askId: string; ask: Ask };
  settle(ask: Ask, state: AskState, answered?: boolean, hold?: boolean): boolean; // answered: a person ended it (gate-owners.ts)
  grant(ask: Ask, hold?: boolean): boolean;
  find(owner: string, askId: unknown): Ask; // 404 unless this owner's
  get(ref: string): Ask | undefined;
  entries(): [string, Ask][];
  replaceOpen(owner: string): void;
  wait(ask: Ask, context: RouteContext): Promise<AskState>;
  prune(): void; // asks, grants, and owner limits past their time
}

export function createAskTable(now: () => number, grants: GateGrants, limits: OwnerLimits, pollMs = POLL_MS): AskTable {
  const asks = new Map<string, Ask>();

  // An ask ends once: its phone hangs up, and its end may count toward a pause.
  const settle = (ask: Ask, state: AskState, answered = false, hold = false): boolean => {
    if (ask.state.state !== "waiting") {
      return false;
    }
    ask.state = state;
    delete ask.code;
    clearTimeout(ask.timer);
    clearTimeout(ask.phoneTimer);
    if (!hold) ask.stop.abort();
    limits.ended(ask.owner, state.state, answered, ask.prepared?.items.find((item) => item.verdict === "typed")?.text ?? "", ask.phoneAt === undefined ? 0 : now() - ask.phoneAt);
    for (const wake of ask.waiters) {
      wake();
    }
    ask.waiters.clear();
    return true;
  };
  const prune = (): void => {
    const at = now();
    for (const [id, ask] of asks) {
      if (ask.state.state === "waiting" && at >= ask.expiresAt) {
        settle(ask, { state: "expired" });
      }
      if (ask.state.state !== "waiting" && at >= ask.expiresAt + GRANT_MS) {
        asks.delete(id);
      }
    }
    grants.prune();
    limits.prune();
  };

  return {
    start(owner, digests, prepared) {
      const askId = randomBytes(32).toString("base64url");
      const expiresAt = now() + ASK_MS;
      const ask: Ask = {
        owner,
        digests,
        ...(prepared !== undefined ? { prepared } : {}),
        expiresAt,
        state: { state: "waiting", expires_at: expiresAt },
        waiters: new Set(),
        stop: new AbortController(),
        timer: setTimeout(() => settle(ask, { state: "expired" }), ASK_MS),
      };
      ask.timer.unref?.();
      asks.set(idKey(askId), ask);
      return { askId, ask };
    },
    settle,
    grant(ask, hold = false) {
      if (ask.state.state !== "waiting") return false;
      if (now() >= ask.expiresAt) {
        settle(ask, { state: "expired" });
        return false;
      }
      const { grantId, until } = grants.issue(ask.owner, ask.digests);
      return settle(ask, { state: "approved", grant_id: grantId, until }, true, hold);
    },
    find(owner, askId) {
      prune();
      const ask = typeof askId === "string" ? asks.get(idKey(askId)) : undefined;
      if (ask === undefined || ask.owner !== owner) {
        throw new HttpError(404, "GATE_ASK_UNKNOWN", "No such ask from this device");
      }
      return ask;
    },
    get: (ref) => asks.get(ref),
    entries: () => [...asks],
    replaceOpen(owner) {
      prune();
      for (const ask of asks.values()) {
        if (ask.owner === owner && ask.state.state === "waiting") {
          settle(ask, { state: "cancelled" }); // one open ask per device: a new one replaces it
        }
      }
    },
    // Long-polls up to pollMs (or the ask's end) for a change from waiting.
    // A caller that hangs up ends its poll (the response's close, not the request's).
    async wait(ask, { response }) {
      if (ask.state.state === "waiting" && !response.destroyed) {
        await new Promise<void>((resolve) => {
          const done = (): void => {
            clearTimeout(timer);
            ask.waiters.delete(done);
            response.off("close", done);
            resolve();
          };
          const timer = setTimeout(done, Math.max(0, Math.min(pollMs, ask.expiresAt - now())));
          ask.waiters.add(done);
          response.once("close", done);
        });
        prune();
      }
      return ask.state;
    },
    prune,
  };
}
