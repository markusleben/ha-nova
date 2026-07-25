import { createHash, randomBytes, randomInt } from "node:crypto";

import { generateCredential } from "./device-credential.js";
import { finishLogin, registerCode, startLogin } from "./opaque-server.js";
import { deriveDirectionKeys, open, seal } from "./pairing-crypto.js";
import { decodePairingMetadata } from "./pairing-metadata.js";
import { createPairingRateLimiter } from "./pairing-rate-limit.js";
import type {
  ActiveCode,
  FinishResult,
  Handshake,
  PairingPhase,
  PairingV1Deps,
  PairingV1Manager,
  StartResult,
} from "./pairing-v1-types.js";

export type {
  ConsumedResponseStore,
  FinishResult,
  PairingPhase,
  PairingV1Deps,
  PairingV1Manager,
  PairingV1Status,
  SecureEndpoint,
  StartResult,
} from "./pairing-v1-types.js";

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

const CODE_SPACE = 1_000_000;
const MAX_CODE_ATTEMPTS = 10;

export function createPairingV1Manager(deps: PairingV1Deps): PairingV1Manager {
  const now = deps.now;
  const genCodeNumber =
    deps.generateCodeNumber ?? (() => randomInt(0, CODE_SPACE));
  const rateLimiter = deps.rateLimiter ?? createPairingRateLimiter();
  const legacyResponseStore = deps.legacyResponseStore;

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
    if (
      phase === "consumed" &&
      now() >= consumedAtMs + CONSUMED_NOTICE_TTL_MS
    ) {
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
      // Local-only pairing needs a mapped TLS endpoint. App mode can still
      // issue the same owner code for v2 because that flow stays in ingress.
      if (deps.secureEndpoint() === null && deps.cloudPairing !== true) {
        throw new Error(
          "secure device port is not available; cannot activate a pairing code",
        );
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
      return startHandshake(ke1, peer, "local");
    },

    startCloud(ke1, userId) {
      if (deps.cloudPairing !== true) {
        return { ok: false, reason: "invalid" };
      }
      return startHandshake(ke1, cloudPeer(userId), "cloud");
    },

    finish(handshakeId, ke3, encryptedMetadataB64, peer) {
      return finishHandshake(
        handshakeId,
        ke3,
        encryptedMetadataB64,
        peer,
        "local",
        () => {
          const endpoint = deps.secureEndpoint();
          return endpoint === null
            ? null
            : { spki_pin: endpoint.spkiPin, secure_port: endpoint.securePort };
        },
        undefined,
      );
    },

    finishCloud(
      handshakeId,
      ke3,
      encryptedMetadataB64,
      userId,
      relayInstanceId,
    ) {
      if (deps.cloudPairing !== true) {
        return { ok: false, reason: "invalid" };
      }
      return finishHandshake(
        handshakeId,
        ke3,
        encryptedMetadataB64,
        cloudPeer(userId),
        "cloud",
        () => ({ relay_instance_id: relayInstanceId }),
        { userId, relayInstanceId },
      );
    },
  };

  function startHandshake(
    ke1: string,
    peer: string,
    mode: "local" | "cloud",
  ): StartResult {
    expireCode();
    pruneHandshakes();
    if (phase !== "active" || !active) {
      return { ok: false, reason: "inactive" };
    }
    if (!isValidMessage(ke1)) {
      return { ok: false, reason: "invalid" };
    }
    const decision = rateLimiter.attempt(peer, now());
    if (!decision.allowed) {
      return {
        ok: false,
        reason: "rate_limited",
        retryAfterSeconds: decision.retryAfterSeconds,
      };
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
      mode,
      serverLoginState: started.serverLoginState,
      createdAtMs: now(),
    });
    return { ok: true, handshakeId, ke2: started.loginResponse };
  }

  function finishHandshake(
    handshakeId: string,
    ke3: string,
    encryptedMetadataB64: string,
    peer: string,
    mode: "local" | "cloud",
    responseFields: () => Record<string, unknown> | null,
    cloudBinding: { userId: string; relayInstanceId: string } | undefined,
  ): FinishResult {
    const contextKey = pairingContext(
      mode,
      peer,
      cloudBinding?.relayInstanceId,
    );
    // Idempotent retry (survives restart): a matching handshake id + identical
    // KE3 returns the exact persisted ciphertext; a divergent retry is generic.
    const persisted =
      deps.registry.getPairingResponse(handshakeId, contextKey, now()) ??
      legacyResponseStore?.get(handshakeId, contextKey);
    if (persisted) {
      return persisted.ke3Digest === digest(ke3)
        ? { ok: true, responseB64: persisted.ciphertextB64 }
        : { ok: false, reason: "invalid" };
    }

    expireCode();
    pruneHandshakes();
    if (
      !isValidMessage(ke3) ||
      encryptedMetadataB64.length > MAX_KE_MESSAGE_CHARS
    ) {
      return { ok: false, reason: "invalid" };
    }
    const hs = handshakes.get(handshakeId);
    if (
      !hs ||
      hs.peer !== peer ||
      hs.mode !== mode ||
      !active ||
      hs.generation !== active.generation
    ) {
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

    const metadata = decodePairingMetadata(
      open(keys.c2s, hsIdBuf, "c2s", decodeB64(encryptedMetadataB64)),
    );
    if (metadata === null) {
      handshakes.delete(handshakeId);
      return { ok: false, reason: "invalid" };
    }

    const extraResponse = responseFields();
    if (extraResponse === null) {
      return { ok: false, reason: "invalid" };
    }

    const cred = generateCredential();
    const t = now();
    const responsePlaintext = Buffer.from(
      JSON.stringify({
        credential: cred.credential,
        device_id: cred.deviceId,
        ...extraResponse,
      }),
      "utf8",
    );
    const ciphertextB64 = seal(
      keys.s2c,
      hsIdBuf,
      "s2c",
      responsePlaintext,
    ).toString("base64url");
    try {
      // The provisional credential and its encrypted, restart-replayable finish
      // response share one atomic registry write. A crash can expose both or
      // neither, never a pending slot whose client cannot recover.
      deps.registry.createPendingWithResponse(
        {
          deviceId: cred.deviceId,
          secretDigest: cred.secretDigest,
          clientInstallId: metadata.clientInstallId,
          name: metadata.name,
          platform: metadata.platform,
          client: metadata.client,
          createdAtMs: t,
          ...(cloudBinding === undefined
            ? {}
            : {
                cloudUserId: cloudBinding.userId,
                cloudRelayInstanceId: cloudBinding.relayInstanceId,
              }),
        },
        {
          handshakeId,
          contextKey,
          deviceId: cred.deviceId,
          ke3Digest: digest(ke3),
          ciphertextB64,
        },
        t,
      );
    } catch {
      // Capacity or durable-write failure: preserve the active owner code.
      return { ok: false, reason: "invalid" };
    }

    // Consume: retire the active code and all other in-flight handshakes.
    active = null;
    handshakes.clear();
    phase = "consumed";
    consumedAtMs = t;
    return { ok: true, responseB64: ciphertextB64 };
  }

  function rollCode(): string {
    for (let i = 0; i < MAX_CODE_ATTEMPTS; i++) {
      const value = genCodeNumber();
      if (!Number.isInteger(value) || value < 0 || value >= CODE_SPACE) {
        throw new Error(
          "pairing code generator returned an out-of-range value",
        );
      }
      const code = value.toString().padStart(6, "0");
      if (!active || code !== active.code) {
        return code;
      }
    }
    throw new Error("pairing code generator repeated the previous code");
  }
}

function cloudPeer(userId: string): string {
  return `cloud:${userId}`;
}

function pairingContext(
  mode: "local" | "cloud",
  peer: string,
  relayInstanceId?: string,
): string {
  return mode === "local"
    ? "local"
    : digest(`${peer}\n${relayInstanceId ?? ""}`);
}

function isValidMessage(value: string): boolean {
  return (
    typeof value === "string" &&
    value.length > 0 &&
    value.length <= MAX_KE_MESSAGE_CHARS &&
    /^[A-Za-z0-9_-]+$/.test(value)
  );
}

function decodeB64(value: string): Buffer {
  return Buffer.from(value, "base64url");
}

function digest(value: string): string {
  return createHash("sha256").update(value, "utf8").digest("base64url");
}
