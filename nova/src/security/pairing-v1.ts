import { createHash, randomBytes, randomInt } from "node:crypto";

import type { DeviceRegistry } from "./device-registry.js";
import { generateCredential } from "./device-credential.js";
import { finishLogin, registerCode, startLogin, type OpaqueRegistration } from "./opaque-server.js";
import { deriveDirectionKeys, open, seal } from "./pairing-crypto.js";
import { createPairingRateLimiter, type PairingRateLimiter } from "./pairing-rate-limit.js";

// The pairing state machine: inactive -> active (owner generated a code) ->
// consumed (exactly one device finished the OPAQUE handshake). A code is never
// auto-generated and never logged. The code and in-flight handshakes are
// in-memory only, so an App restart before finish invalidates them; the
// consumed response and the pending credential are persisted so a restart AFTER
// finish stays resumable and a lost finish response can be retried idempotently.

export const PAIR_CODE_TTL_MS = 10 * 60 * 1_000;
export const HANDSHAKE_TTL_MS = 60 * 1_000;
// How long the owner page keeps saying "a device was just connected".
export const CONSUMED_NOTICE_TTL_MS = 2 * 60 * 1_000;
export const HANDSHAKE_ID_BYTES = 16; // 128-bit
export const MAX_CONCURRENT_HANDSHAKES = 32;
export const MAX_KE_MESSAGE_CHARS = 8 * 1024;
export const MAX_METADATA_BYTES = 4 * 1024;

const CODE_SPACE = 1_000_000;
const MAX_CODE_ATTEMPTS = 10;

export type PairingPhase = "inactive" | "active" | "consumed";

export interface SecureEndpoint {
  spkiPin: string;
  securePort: number;
}

export interface DeviceMetadata {
  name: string;
  platform: string;
  client: string;
  clientInstallId: string;
}

// Persisted consumed-response record, keyed by handshake id, for idempotent
// finish retries (including after a restart).
export interface ConsumedResponseStore {
  get(handshakeId: string): { ke3Digest: string; ciphertextB64: string } | null;
  put(handshakeId: string, ke3Digest: string, ciphertextB64: string, now: number): void;
}

export interface PairingV1Deps {
  registry: DeviceRegistry;
  // Effective secure endpoint (TLS pin + mapped host port). Null means the
  // secure port is unmapped/disabled, in which case a code cannot be activated
  // (the client could never reach the TLS listener the response points at).
  secureEndpoint: () => SecureEndpoint | null;
  now: () => number;
  generateCodeNumber?: () => number;
  rateLimiter?: PairingRateLimiter;
  responseStore?: ConsumedResponseStore;
}

export interface PairingV1Status {
  phase: PairingPhase;
  code?: string;
  expiresAtMs?: number;
}

export type StartResult =
  | { ok: true; handshakeId: string; ke2: string }
  | { ok: false; reason: "inactive" | "invalid" | "busy" }
  | { ok: false; reason: "rate_limited"; retryAfterSeconds: number };

export type FinishResult =
  | { ok: true; responseB64: string }
  | { ok: false; reason: "invalid" };

interface ActiveCode {
  generation: number;
  code: string;
  userIdentifier: string;
  registration: OpaqueRegistration;
  expiresAtMs: number;
}

interface Handshake {
  generation: number;
  peer: string;
  serverLoginState: string;
  createdAtMs: number;
}

class MemoryResponseStore implements ConsumedResponseStore {
  private readonly map = new Map<string, { ke3Digest: string; ciphertextB64: string }>();
  get(handshakeId: string) {
    return this.map.get(handshakeId) ?? null;
  }
  put(handshakeId: string, ke3Digest: string, ciphertextB64: string): void {
    this.map.set(handshakeId, { ke3Digest, ciphertextB64 });
  }
}

export interface PairingV1Manager {
  getStatus(): PairingV1Status;
  generateCode(): { code: string; expiresAtMs: number };
  cancel(): void;
  start(ke1: string, peer: string): StartResult;
  finish(handshakeId: string, ke3: string, encryptedMetadataB64: string, peer: string): FinishResult;
}

export function createPairingV1Manager(deps: PairingV1Deps): PairingV1Manager {
  const now = deps.now;
  const genCodeNumber = deps.generateCodeNumber ?? (() => randomInt(0, CODE_SPACE));
  const rateLimiter = deps.rateLimiter ?? createPairingRateLimiter();
  const responseStore = deps.responseStore ?? new MemoryResponseStore();

  let phase: PairingPhase = "inactive";
  let active: ActiveCode | null = null;
  let consumedAtMs = 0;
  let generationCounter = 0;
  const handshakes = new Map<string, Handshake>();

  function expireCode(): void {
    // Only an active (non-null) code can expire.
    if (active && now() >= active.expiresAtMs) {
      active = null;
      handshakes.clear();
      phase = "inactive";
    }
    // The "a device was just connected" state is a NOTICE, not a terminal
    // state: hours later "just" would be a lie (and contradict an
    // owner-emptied device list), so it decays back to inactive.
    if (phase === "consumed" && now() >= consumedAtMs + CONSUMED_NOTICE_TTL_MS) {
      phase = "inactive";
    }
  }

  function pruneHandshakes(): void {
    const t = now();
    for (const [id, hs] of handshakes) {
      if (t >= hs.createdAtMs + HANDSHAKE_TTL_MS) {
        handshakes.delete(id);
      }
    }
  }

  return {
    getStatus() {
      expireCode();
      if (phase === "active" && active) {
        return { phase, code: active.code, expiresAtMs: active.expiresAtMs };
      }
      return { phase };
    },

    generateCode() {
      // A code can only be activated when the secure endpoint is mappable: the
      // response points the client at the TLS port, so an unmapped port would
      // hand out an unreachable credential.
      if (deps.secureEndpoint() === null) {
        throw new Error("secure device port is not available; cannot activate a pairing code");
      }
      const t = now();
      const code = rollCode();
      const generation = ++generationCounter;
      const userIdentifier = `gen-${generation}-${randomBytes(8).toString("base64url")}`;
      active = {
        generation,
        code,
        userIdentifier,
        registration: registerCode(code, userIdentifier),
        expiresAtMs: t + PAIR_CODE_TTL_MS,
      };
      handshakes.clear();
      phase = "active";
      return { code, expiresAtMs: active.expiresAtMs };
    },

    cancel() {
      active = null;
      handshakes.clear();
      phase = "inactive";
    },

    start(ke1, peer) {
      const decision = rateLimiter.attempt(peer, now());
      if (!decision.allowed) {
        return { ok: false, reason: "rate_limited", retryAfterSeconds: decision.retryAfterSeconds };
      }
      expireCode();
      pruneHandshakes();
      if (phase !== "active" || !active) {
        return { ok: false, reason: "inactive" };
      }
      if (!isValidMessage(ke1)) {
        return { ok: false, reason: "invalid" };
      }
      if (handshakes.size >= MAX_CONCURRENT_HANDSHAKES) {
        return { ok: false, reason: "busy" };
      }
      const started = startLogin(active.registration, active.userIdentifier, ke1);
      if (started === null) {
        return { ok: false, reason: "invalid" };
      }
      const handshakeId = randomBytes(HANDSHAKE_ID_BYTES).toString("base64url");
      handshakes.set(handshakeId, {
        generation: active.generation,
        peer,
        serverLoginState: started.serverLoginState,
        createdAtMs: now(),
      });
      return { ok: true, handshakeId, ke2: started.loginResponse };
    },

    finish(handshakeId, ke3, encryptedMetadataB64, peer) {
      // Idempotent retry (survives restart): a matching handshake id + identical
      // KE3 returns the exact persisted ciphertext; a divergent retry is generic.
      const persisted = responseStore.get(handshakeId);
      if (persisted) {
        return persisted.ke3Digest === digest(ke3)
          ? { ok: true, responseB64: persisted.ciphertextB64 }
          : { ok: false, reason: "invalid" };
      }

      expireCode();
      pruneHandshakes();
      if (!isValidMessage(ke3) || encryptedMetadataB64.length > MAX_KE_MESSAGE_CHARS) {
        return { ok: false, reason: "invalid" };
      }
      const hs = handshakes.get(handshakeId);
      if (!hs || hs.peer !== peer || !active || hs.generation !== active.generation) {
        return { ok: false, reason: "invalid" };
      }

      const sessionKeyB64 = finishLogin(hs.serverLoginState, ke3);
      if (sessionKeyB64 === null) {
        handshakes.delete(handshakeId);
        return { ok: false, reason: "invalid" };
      }
      const sessionKey = Buffer.from(sessionKeyB64, "base64url");
      const hsIdBuf = Buffer.from(handshakeId, "base64url");
      const keys = deriveDirectionKeys(sessionKey, hsIdBuf);

      const metadata = decodeMetadata(open(keys.c2s, hsIdBuf, "c2s", decodeB64(encryptedMetadataB64)));
      if (metadata === null) {
        handshakes.delete(handshakeId);
        return { ok: false, reason: "invalid" };
      }

      const endpoint = deps.secureEndpoint();
      if (endpoint === null) {
        return { ok: false, reason: "invalid" };
      }

      // Compare-and-swap: consuming the code is the single winning transition.
      // Because finish is fully synchronous, exactly one call reaches here for a
      // given generation; the rest already failed the generation check above.
      const cred = generateCredential();
      const t = now();
      try {
        deps.registry.createPending(
          {
            deviceId: cred.deviceId,
            secretDigest: cred.secretDigest,
            clientInstallId: metadata.clientInstallId,
            name: metadata.name,
            platform: metadata.platform,
            client: metadata.client,
            createdAtMs: t,
          },
          t
        );
      } catch {
        // Pending cap reached or a registry write error: fail closed WITHOUT
        // consuming the code, so the owner can retry after freeing a slot.
        return { ok: false, reason: "invalid" };
      }

      const responsePlaintext = Buffer.from(
        JSON.stringify({
          credential: cred.credential,
          device_id: cred.deviceId,
          spki_pin: endpoint.spkiPin,
          secure_port: endpoint.securePort,
        }),
        "utf8"
      );
      const ciphertextB64 = seal(keys.s2c, hsIdBuf, "s2c", responsePlaintext).toString("base64url");
      responseStore.put(handshakeId, digest(ke3), ciphertextB64, t);

      // Consume: retire the active code and all other in-flight handshakes.
      active = null;
      handshakes.clear();
      phase = "consumed";
      consumedAtMs = t;
      return { ok: true, responseB64: ciphertextB64 };
    },
  };

  function rollCode(): string {
    for (let i = 0; i < MAX_CODE_ATTEMPTS; i++) {
      const value = genCodeNumber();
      if (!Number.isInteger(value) || value < 0 || value >= CODE_SPACE) {
        throw new Error("pairing code generator returned an out-of-range value");
      }
      const code = value.toString().padStart(6, "0");
      if (!active || code !== active.code) {
        return code;
      }
    }
    throw new Error("pairing code generator repeated the previous code");
  }
}

function isValidMessage(value: string): boolean {
  return typeof value === "string" && value.length > 0 && value.length <= MAX_KE_MESSAGE_CHARS && /^[A-Za-z0-9_-]+$/.test(value);
}

function decodeB64(value: string): Buffer {
  return Buffer.from(value, "base64url");
}

function digest(value: string): string {
  return createHash("sha256").update(value, "utf8").digest("base64url");
}

function decodeMetadata(plaintext: Buffer | null): DeviceMetadata | null {
  if (plaintext === null || plaintext.length > MAX_METADATA_BYTES) {
    return null;
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(plaintext.toString("utf8"));
  } catch {
    return null;
  }
  if (typeof parsed !== "object" || parsed === null) {
    return null;
  }
  const m = parsed as Record<string, unknown>;
  const name = boundedString(m.name, 64);
  const platform = boundedString(m.platform, 32);
  const client = boundedString(m.client, 32);
  const clientInstallId = boundedString(m.client_install_id, 128);
  if (name === null || platform === null || client === null || clientInstallId === null) {
    return null;
  }
  return { name, platform, client, clientInstallId };
}

function boundedString(value: unknown, maxLen: number): string | null {
  if (typeof value !== "string" || value.length === 0 || value.length > maxLen) {
    return null;
  }
  // Strip control characters; the owner page escapes on render, but keep stored
  // metadata clean.
  // eslint-disable-next-line no-control-regex
  if (/[\u0000-\u001f\u007f]/.test(value)) {
    return null;
  }
  return value;
}
