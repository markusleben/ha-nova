import { randomInt } from "node:crypto";

import { constantTimeEqualSecret } from "./secret-compare.js";

export const PAIRING_CODE_TTL_MS = 10 * 60 * 1_000;
export const PAIRING_PEER_WINDOW_MS = 60 * 1_000;
export const PAIRING_GLOBAL_WINDOW_MS = 5 * 60 * 1_000;
export const PAIRING_PEER_FAILURE_LIMIT = 5;
export const PAIRING_GLOBAL_FAILURE_LIMIT = 30;
export const PAIRING_MAX_TRACKED_PEERS = 256;

const PAIRING_CODE_SPACE = 1_000_000;
const MAX_CODE_GENERATION_ATTEMPTS = 10;

export interface PairingStatus {
  code: string;
  expiresAtMs: number;
}

export type PairingExchangeResult =
  | { ok: true; relayToken: string }
  | { ok: false; reason: "invalid" }
  | { ok: false; reason: "rate_limited"; retryAfterSeconds: number };

export interface PairingManager {
  getStatus(): PairingStatus;
  exchange(code: string, peer: string): PairingExchangeResult;
}

interface FailureBucket {
  windowStartedAtMs: number;
  failures: number;
  lastSeenAtMs: number;
}

export interface PairingManagerOptions {
  relayToken: string;
  now?: () => number;
  generateCodeNumber?: () => number;
}

export function createPairingManager(options: PairingManagerOptions): PairingManager {
  const now = options.now ?? Date.now;
  const generateCodeNumber = options.generateCodeNumber ?? (() => randomInt(0, PAIRING_CODE_SPACE));
  const peerBuckets = new Map<string, FailureBucket>();
  let globalBucket: FailureBucket | undefined;
  let status = rotateCode(now(), undefined, generateCodeNumber);

  return {
    getStatus() {
      status = rotateIfExpired(status, now(), generateCodeNumber);
      return { ...status };
    },
    exchange(code, peer) {
      const currentTime = now();
      status = rotateIfExpired(status, currentTime, generateCodeNumber);

      const peerBucket = activeBucket(peerBuckets.get(peer), currentTime, PAIRING_PEER_WINDOW_MS);
      if (peerBucket) {
        peerBucket.lastSeenAtMs = currentTime;
        peerBuckets.set(peer, peerBucket);
      }
      globalBucket = activeBucket(globalBucket, currentTime, PAIRING_GLOBAL_WINDOW_MS);

      const peerBlocked = bucketBlocked(peerBucket, PAIRING_PEER_FAILURE_LIMIT);
      const globalBlocked = bucketBlocked(globalBucket, PAIRING_GLOBAL_FAILURE_LIMIT);
      if (peerBlocked || globalBlocked) {
        return {
          ok: false,
          reason: "rate_limited",
          retryAfterSeconds: retryAfterSeconds(
            currentTime,
            peerBlocked ? peerBucket : undefined,
            PAIRING_PEER_WINDOW_MS,
            globalBlocked ? globalBucket : undefined,
            PAIRING_GLOBAL_WINDOW_MS
          )
        };
      }

      if (constantTimeEqualSecret(code, status.code)) {
        const usedCode = status.code;
        status = rotateCode(currentTime, usedCode, generateCodeNumber);
        return { ok: true, relayToken: options.relayToken };
      }

      const updatedPeerBucket = recordPeerFailure(peerBuckets, peer, peerBucket, currentTime);
      peerBuckets.set(peer, updatedPeerBucket);
      globalBucket = recordFailure(globalBucket, currentTime);
      return { ok: false, reason: "invalid" };
    }
  };
}

function rotateIfExpired(
  current: PairingStatus,
  now: number,
  generateCodeNumber: () => number
): PairingStatus {
  return now >= current.expiresAtMs
    ? rotateCode(now, current.code, generateCodeNumber)
    : current;
}

function rotateCode(
  now: number,
  previousCode: string | undefined,
  generateCodeNumber: () => number
): PairingStatus {
  for (let attempt = 0; attempt < MAX_CODE_GENERATION_ATTEMPTS; attempt += 1) {
    const value = generateCodeNumber();
    if (!Number.isInteger(value) || value < 0 || value >= PAIRING_CODE_SPACE) {
      throw new Error("Pairing code generator returned an out-of-range value");
    }
    const code = value.toString().padStart(6, "0");
    if (code !== previousCode) {
      return { code, expiresAtMs: now + PAIRING_CODE_TTL_MS };
    }
  }

  throw new Error("Pairing code generator repeated the previous code");
}

function activeBucket(
  bucket: FailureBucket | undefined,
  now: number,
  windowMs: number
): FailureBucket | undefined {
  if (!bucket || now >= bucket.windowStartedAtMs + windowMs) {
    return undefined;
  }
  return bucket;
}

function bucketBlocked(bucket: FailureBucket | undefined, limit: number): bucket is FailureBucket {
  return Boolean(bucket && bucket.failures >= limit);
}

function recordPeerFailure(
  peerBuckets: Map<string, FailureBucket>,
  peer: string,
  active: FailureBucket | undefined,
  now: number
): FailureBucket {
  if (!active && !peerBuckets.has(peer) && peerBuckets.size >= PAIRING_MAX_TRACKED_PEERS) {
    evictOldestPeer(peerBuckets);
  }
  return recordFailure(active, now);
}

function recordFailure(bucket: FailureBucket | undefined, now: number): FailureBucket {
  if (!bucket) {
    return { windowStartedAtMs: now, failures: 1, lastSeenAtMs: now };
  }
  bucket.failures += 1;
  bucket.lastSeenAtMs = now;
  return bucket;
}

function evictOldestPeer(peerBuckets: Map<string, FailureBucket>): void {
  let oldestPeer: string | undefined;
  let oldestSeenAt = Number.POSITIVE_INFINITY;
  for (const [peer, bucket] of peerBuckets) {
    if (bucket.lastSeenAtMs < oldestSeenAt) {
      oldestPeer = peer;
      oldestSeenAt = bucket.lastSeenAtMs;
    }
  }
  if (oldestPeer) {
    peerBuckets.delete(oldestPeer);
  }
}

function retryAfterSeconds(
  now: number,
  peerBucket: FailureBucket | undefined,
  peerWindowMs: number,
  globalBucket: FailureBucket | undefined,
  globalWindowMs: number
): number {
  const peerRemaining = peerBucket
    ? peerBucket.windowStartedAtMs + peerWindowMs - now
    : 0;
  const globalRemaining = globalBucket
    ? globalBucket.windowStartedAtMs + globalWindowMs - now
    : 0;
  return Math.max(1, Math.ceil(Math.max(peerRemaining, globalRemaining) / 1_000));
}
