import { HttpError } from "./errors.js";
import type { AskState } from "./gate-ask-types.js";

// Each asker's limits (docs/work/2026-11-03-relay-typed-gate-spec.md §3,
// Limits): at most ten asks an hour, and its phone asks paused for 5 minutes
// after five in a row go unanswered: expired, or ended by the asker after 15 s
// without an answer (the CLI stops waiting after 90 s, its MCP tools after
// 40 s, and asks no phone with under 15 s left). A person's answer (on the
// phone or the NOVA page) resets the count; a cancel or replacing ask within
// those 15 s, or no phone to ask, neither counts nor resets it, so
// cancelling can't dodge a pause. The NOVA
// page shows those five and lets an administrator lift the pause. Memory only.

const ASKS_PER_HOUR = 10;
const HOUR_MS = 3_600_000;
const PAUSE_AFTER = 5;
const PAUSE_MS = 300_000;
const MISS_MS = 15_000; // open this long without an answer: a miss, however the asker ended it (no asker waits less)
export const PAUSED = "Phone asks from this device are paused for 5 minutes after five in a row went unanswered; nothing was sent";

// An ask that went unanswered: when, and its first typed item's text ("": a link ask).
export interface Miss {
  at: number;
  what: string;
}

export interface Pause {
  owner: string;
  until: number;
  misses: Miss[]; // the asks that paused it, oldest first
}

interface Owner {
  opened: number[]; // when it asked, in the last hour (each ask the worker rated)
  misses: Miss[]; // its asks in a row that went unanswered; an idle hour forgets them
  pausedUntil: number; // its phone asks
  pausedBy: Miss[]; // the five that paused them
}

export interface OwnerLimits {
  limit(owner: string): void; // 429 past the hour's asks
  count(owner: string): void;
  paused(owner: string): boolean;
  ended(owner: string, state: AskState["state"], answered: boolean, what: string, openMs: number): void; // answered: a person ended it
  pauses(): Pause[];
  resume(owner: string): boolean; // false: not paused
  prune(): void;
}

export function createOwnerLimits(now: () => number): OwnerLimits {
  const owners = new Map<string, Owner>();
  const get = (owner: string): Owner => {
    let state = owners.get(owner);
    if (state === undefined) {
      state = { opened: [], misses: [], pausedUntil: 0, pausedBy: [] };
      owners.set(owner, state);
    }
    return state;
  };
  const prune = (): void => {
    for (const [id, owner] of owners) {
      owner.opened = owner.opened.filter((at) => now() - at < HOUR_MS);
      if (owner.opened.length === 0 && now() >= owner.pausedUntil) {
        owners.delete(id);
      }
    }
  };
  const paused = (owner: string): boolean => now() < (owners.get(owner)?.pausedUntil ?? 0);
  return {
    limit(owner) {
      prune();
      if ((owners.get(owner)?.opened.length ?? 0) >= ASKS_PER_HOUR) {
        throw new HttpError(429, "GATE_ASK_LIMIT", `This device asked ${ASKS_PER_HOUR} times in the last hour; try again later. Nothing was asked or sent`);
      }
    },
    count(owner) {
      get(owner).opened.push(now()); // counted whatever the worker answers
    },
    paused,
    ended(owner, state, answered, what, openMs) {
      const asker = get(owner);
      if (answered || state === "approved" || state === "linked") {
        asker.misses = [];
      } else if (state === "expired" || (state === "cancelled" && openMs >= MISS_MS)) {
        asker.misses.push({ at: now(), what });
        if (asker.misses.length >= PAUSE_AFTER) {
          [asker.pausedBy, asker.misses] = [asker.misses, []];
          asker.pausedUntil = now() + PAUSE_MS;
        }
      }
    },
    pauses() {
      prune();
      return [...owners].filter(([id]) => paused(id)).map(([id, asker]) => ({ owner: id, until: asker.pausedUntil, misses: asker.pausedBy }));
    },
    resume(owner) {
      const asker = owners.get(owner);
      if (asker === undefined || !paused(owner)) {
        return false;
      }
      asker.pausedUntil = 0;
      asker.misses = [];
      asker.pausedBy = [];
      return true;
    },
    prune,
  };
}
