// Attempt-based rate limiter for the pairing bootstrap. Unlike the legacy
// code-exchange limiter (which counted only failures), EVERY /pair/v1/start
// attempt counts here — malformed, wrong-code, and valid alike — so a flood of
// junk KE1s cannot probe the code space or exhaust handshake slots. Fixed
// windows: 5 attempts / peer / 60 s and 30 attempts globally / 5 min. Peer
// buckets are capped and LRU-evicted so the map cannot grow unbounded; the peer
// is the canonical socket address (never a forwarded header).

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
  | { allowed: true }
  | { allowed: false; retryAfterSeconds: number };

export interface PairingRateLimiter {
  // Records one attempt for the peer and globally, returning whether it is
  // within limits. An over-limit attempt is still recorded (a determined
  // attacker keeps the window hot rather than letting it lapse early).
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
      peerWindow.count += 1;
      peerWindow.lastSeenAtMs = now;
      peers.set(peer, peerWindow);

      global = roll(global, now, PAIR_GLOBAL_WINDOW_MS);
      global.count += 1;
      global.lastSeenAtMs = now;

      const peerOver = peerWindow.count > PAIR_PEER_ATTEMPT_LIMIT;
      const globalOver = global.count > PAIR_GLOBAL_ATTEMPT_LIMIT;
      if (!peerOver && !globalOver) {
        return { allowed: true };
      }
      const peerRemaining = peerOver ? peerWindow.startedAtMs + PAIR_PEER_WINDOW_MS - now : 0;
      const globalRemaining = globalOver ? global.startedAtMs + PAIR_GLOBAL_WINDOW_MS - now : 0;
      return {
        allowed: false,
        retryAfterSeconds: Math.max(1, Math.ceil(Math.max(peerRemaining, globalRemaining) / 1_000)),
      };
    },
  };
}

function roll(window: Window | undefined, now: number, windowMs: number): Window {
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
