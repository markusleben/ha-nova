import { HttpError } from "./errors.js";
import type { AskState } from "./gate-ask-types.js";

// Each asker's limits: sixty computer asks an hour, ten phone dispatches,
// and a five-minute phone pause after five unanswered exposures of at least
// fifteen seconds. A person's answer resets the run; an unexposed phone or
// a shorter exposure neither counts nor resets it. Memory only.

const ASKS_PER_HOUR = 10;
const COMPUTER_ASKS_PER_HOUR = 60;
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
  phones: { at: number }[];
  misses: Miss[]; // its asks in a row that went unanswered; an idle hour forgets them
  pausedUntil: number; // its phone asks
  pausedBy: Miss[]; // the five that paused them
}

export interface OwnerLimits {
  limit(owner: string): void; // 429 past the hour's asks
  count(owner: string): void;
  phoneAvailable(owner: string): boolean;
  reservePhone(owner: string): (() => void) | undefined;
  paused(owner: string): boolean;
  ended(owner: string, state: AskState["state"], answered: boolean, what: string, exposedMs: number): void; // answered: a person ended it
  pauses(): Pause[];
  resume(owner: string): boolean; // false: not paused
  prune(): void;
}

export function createOwnerLimits(now: () => number): OwnerLimits {
  const owners = new Map<string, Owner>();
  const get = (owner: string): Owner => {
    let state = owners.get(owner);
    if (state === undefined) {
      state = { opened: [], phones: [], misses: [], pausedUntil: 0, pausedBy: [] };
      owners.set(owner, state);
    }
    return state;
  };
  const prune = (): void => {
    for (const [id, owner] of owners) {
      owner.opened = owner.opened.filter((at) => now() - at < HOUR_MS);
      owner.phones = owner.phones.filter(({ at }) => now() - at < HOUR_MS);
      if (owner.opened.length === 0 && owner.phones.length === 0 && now() >= owner.pausedUntil) {
        owners.delete(id);
      }
    }
  };
  const paused = (owner: string): boolean => now() < (owners.get(owner)?.pausedUntil ?? 0);
  const phoneAvailable = (owner: string): boolean => {
    prune();
    return !paused(owner) && (owners.get(owner)?.phones.length ?? 0) < ASKS_PER_HOUR;
  };
  return {
    limit(owner) {
      prune();
      const maximum = owner.startsWith("device:") ? COMPUTER_ASKS_PER_HOUR : ASKS_PER_HOUR;
      if ((owners.get(owner)?.opened.length ?? 0) >= maximum) {
        throw new HttpError(429, "GATE_ASK_LIMIT", `This device asked ${maximum} times in the last hour; try again later. Nothing was asked or sent`);
      }
    },
    count(owner) {
      get(owner).opened.push(now()); // counted whatever the worker answers
    },
    phoneAvailable,
    reservePhone(owner) {
      if (!phoneAvailable(owner)) return undefined;
      const asker = get(owner);
      const reservation = { at: now() };
      asker.phones.push(reservation);
      return () => { asker.phones = asker.phones.filter((entry) => entry !== reservation); };
    },
    paused,
    ended(owner, state, answered, what, exposedMs) {
      const asker = get(owner);
      if (answered || state === "approved" || state === "linked") {
        asker.misses = [];
      } else if ((state === "expired" || state === "cancelled") && exposedMs >= MISS_MS) {
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
