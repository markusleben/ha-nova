import { HttpError } from "./errors.js";
import type { AskState } from "./gate-ask-types.js";

// Each asker's limits (docs/work/2026-11-03-relay-typed-gate-spec.md §3,
// Limits): at most ten asks an hour, and its phone asks paused for 15 minutes
// after three in a row end cancelled or unanswered. Memory only.

const ASKS_PER_HOUR = 10;
const HOUR_MS = 3_600_000;
const PAUSE_AFTER = 3;
const PAUSE_MS = 900_000;
export const PAUSED = "Phone asks from this device are paused for 15 minutes after three in a row went unanswered or were cancelled; nothing was sent";

interface Owner {
  opened: number[]; // when it asked, in the last hour (each ask the worker rated)
  misses: number; // its asks in a row that ended cancelled or unanswered; an idle hour forgets them
  pausedUntil: number; // its phone asks
}

export interface OwnerLimits {
  limit(owner: string): void; // 429 past the hour's asks
  count(owner: string): void;
  paused(owner: string): boolean;
  ended(owner: string, state: AskState["state"]): void;
  pausedOwners(): string[];
  prune(): void;
}

export function createOwnerLimits(now: () => number): OwnerLimits {
  const owners = new Map<string, Owner>();
  const get = (owner: string): Owner => {
    let state = owners.get(owner);
    if (state === undefined) {
      state = { opened: [], misses: 0, pausedUntil: 0 };
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
    ended(owner, state) {
      const asker = get(owner);
      if (state === "approved" || state === "linked") {
        asker.misses = 0;
      } else if (state !== "unavailable" && ++asker.misses >= PAUSE_AFTER) {
        asker.misses = 0;
        asker.pausedUntil = now() + PAUSE_MS;
      }
    },
    pausedOwners() {
      prune();
      return [...owners.keys()].filter(paused);
    },
    prune,
  };
}
