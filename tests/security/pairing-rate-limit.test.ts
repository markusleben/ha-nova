import { describe, expect, it } from "vitest";

import {
  PAIR_GLOBAL_ATTEMPT_LIMIT,
  PAIR_PEER_ATTEMPT_LIMIT,
  PAIR_PEER_WINDOW_MS,
  createPairingRateLimiter,
} from "../../nova/src/security/pairing-rate-limit.js";

describe("pairing-rate-limit", () => {
  it("allows up to the peer limit, then blocks with a retry-after", () => {
    const rl = createPairingRateLimiter();
    for (let i = 0; i < PAIR_PEER_ATTEMPT_LIMIT; i++) {
      expect(rl.attempt("1.2.3.4", 1000).allowed).toBe(true);
    }
    const blocked = rl.attempt("1.2.3.4", 1000);
    expect(blocked.allowed).toBe(false);
    if (!blocked.allowed) {
      expect(blocked.retryAfterSeconds).toBeGreaterThan(0);
    }
  });

  it("resets after the window elapses", () => {
    const rl = createPairingRateLimiter();
    for (let i = 0; i < PAIR_PEER_ATTEMPT_LIMIT; i++) rl.attempt("p", 1000);
    expect(rl.attempt("p", 1000).allowed).toBe(false);
    expect(rl.attempt("p", 1000 + PAIR_PEER_WINDOW_MS + 1).allowed).toBe(true);
  });

  it("isolates peers but enforces a global cap", () => {
    const rl = createPairingRateLimiter();
    // Distinct peers, each under the peer limit, still trip the global cap.
    let blockedByGlobal = false;
    for (let i = 0; i < PAIR_GLOBAL_ATTEMPT_LIMIT + 5; i++) {
      const d = rl.attempt(`peer-${i}`, 1000);
      if (!d.allowed) blockedByGlobal = true;
    }
    expect(blockedByGlobal).toBe(true);
  });

  it("does not let one peer consume the global budget after its own cap", () => {
    const rl = createPairingRateLimiter();
    for (let i = 0; i < PAIR_PEER_ATTEMPT_LIMIT; i++) {
      expect(rl.attempt("attacker", 1000).allowed).toBe(true);
    }
    for (let i = 0; i < 100; i++) {
      expect(rl.attempt("attacker", 1000).allowed).toBe(false);
    }

    // The attacker's rejected attempts did not consume the remaining global
    // capacity. Distinct legitimate peers can still use all remaining slots.
    for (let i = PAIR_PEER_ATTEMPT_LIMIT; i < PAIR_GLOBAL_ATTEMPT_LIMIT; i++) {
      expect(rl.attempt(`legitimate-${i}`, 1000).allowed).toBe(true);
    }
    expect(rl.attempt("over-global-cap", 1000).allowed).toBe(false);
  });
});
