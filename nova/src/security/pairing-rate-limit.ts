// Attempt-based rate limiter for structurally valid pairing starts while an
// owner code is active. Fixed windows: 5 accepted attempts / peer / 60 s and 30
// accepted attempts globally / 5 min. A peer already blocked by its own budget
// cannot consume the global budget and deny pairing to everyone else.

export const PAIR_PEER_WINDOW_MS = 60 * 1_000;
export const PAIR_GLOBAL_WINDOW_MS = 5 * 60 * 1_000;
export const PAIR_PEER_ATTEMPT_LIMIT = 5;
export const PAIR_GLOBAL_ATTEMPT_LIMIT = 30;
export const PAIR_MAX_TRACKED_PEERS = 256;

interface Window {
  startedAtMs: number;
  count: number;
  lastSeenAtMs: number;
}

export type RateDecision =
  { allowed: true } | { allowed: false; retryAfterSeconds: number };

export interface PairingRateLimiter {
  // Records an allowed attempt for the peer and globally. Rejected attempts do
  // not consume another budget.
  attempt(peer: string, now: number): RateDecision;
}

export function createPairingRateLimiter(): PairingRateLimiter {
  const peers = new Map<string, Window>();
  let global: Window | undefined;

  return {
    attempt(peer, now) {
      const peerWindow = roll(peers.get(peer), now, PAIR_PEER_WINDOW_MS);
      if (!peers.has(peer) && peers.size >= PAIR_MAX_TRACKED_PEERS) {
        evictOldest(peers);
      }
      peerWindow.lastSeenAtMs = now;
      peers.set(peer, peerWindow);

      global = roll(global, now, PAIR_GLOBAL_WINDOW_MS);
      global.lastSeenAtMs = now;

      if (peerWindow.count >= PAIR_PEER_ATTEMPT_LIMIT) {
        return blockedUntil(peerWindow.startedAtMs + PAIR_PEER_WINDOW_MS, now);
      }
      if (global.count >= PAIR_GLOBAL_ATTEMPT_LIMIT) {
        return blockedUntil(global.startedAtMs + PAIR_GLOBAL_WINDOW_MS, now);
      }

      peerWindow.count += 1;
      global.count += 1;
      return { allowed: true };
    },
  };
}

function blockedUntil(untilMs: number, now: number): RateDecision {
  return {
    allowed: false,
    retryAfterSeconds: Math.max(1, Math.ceil((untilMs - now) / 1_000)),
  };
}

function roll(
  window: Window | undefined,
  now: number,
  windowMs: number,
): Window {
  if (!window || now >= window.startedAtMs + windowMs) {
    return { startedAtMs: now, count: 0, lastSeenAtMs: now };
  }
  return window;
}

function evictOldest(peers: Map<string, Window>): void {
  let oldest: string | undefined;
  let oldestSeen = Number.POSITIVE_INFINITY;
  for (const [peer, w] of peers) {
    if (w.lastSeenAtMs < oldestSeen) {
      oldest = peer;
      oldestSeen = w.lastSeenAtMs;
    }
  }
  if (oldest !== undefined) {
    peers.delete(oldest);
  }
}
