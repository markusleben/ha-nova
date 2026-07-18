import { randomBytes, timingSafeEqual } from "node:crypto";

// CSRF tokens for the NOVA page's POST /home/action. Each token is 256-bit,
// single-use, valid for five minutes, and bound to BOTH the owner user id and
// the specific action it was issued for — so a token minted for "generate code"
// cannot drive "revoke device". At most eight tokens are pending at once
// (issuing a ninth evicts the oldest), bounding memory and stale-token surface.

export const CSRF_TTL_MS = 5 * 60 * 1_000;
export const CSRF_MAX_PENDING = 8;
const TOKEN_BYTES = 32;

interface Entry {
  token: string;
  userId: string;
  action: string;
  expiresAtMs: number;
}

export interface CsrfStore {
  issue(userId: string, action: string, now: number): string;
  // Single-use: a successful consume removes the token. Returns false for an
  // unknown, expired, wrong-owner, or wrong-action token.
  consume(userId: string, action: string, token: string, now: number): boolean;
}

export function createCsrfStore(): CsrfStore {
  let entries: Entry[] = [];

  function prune(now: number): void {
    entries = entries.filter((e) => now < e.expiresAtMs);
  }

  return {
    issue(userId, action, now) {
      prune(now);
      if (entries.length >= CSRF_MAX_PENDING) {
        // Evict the oldest (soonest to expire) to make room.
        entries.sort((a, b) => a.expiresAtMs - b.expiresAtMs);
        entries.shift();
      }
      const token = randomBytes(TOKEN_BYTES).toString("base64url");
      entries.push({ token, userId, action, expiresAtMs: now + CSRF_TTL_MS });
      return token;
    },
    consume(userId, action, token, now) {
      prune(now);
      const index = entries.findIndex(
        (e) => e.userId === userId && e.action === action && constantTimeEqual(e.token, token)
      );
      if (index === -1) {
        return false;
      }
      entries.splice(index, 1); // single use
      return true;
    },
  };
}

function constantTimeEqual(a: string, b: string): boolean {
  const ab = Buffer.from(a, "utf8");
  const bb = Buffer.from(b, "utf8");
  if (ab.length !== bb.length) {
    return false;
  }
  return timingSafeEqual(ab, bb);
}
