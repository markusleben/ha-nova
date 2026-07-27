import type { DeviceRegistry } from "./device-registry-types.js";
import type { OpaqueRegistration } from "./opaque-server.js";
import type { PairingRateLimiter } from "./pairing-rate-limit.js";

export type PairingPhase = "inactive" | "active" | "consumed";

export interface SecureEndpoint {
  spkiPin: string;
  securePort: number;
}

// Legacy standalone response store retained as a read fallback for upgrades
// from versions that persisted responses separately from the device registry.
export interface ConsumedResponseStore {
  get(
    handshakeId: string,
    contextKey?: string,
  ): { ke3Digest: string; ciphertextB64: string } | null;
  put(
    handshakeId: string,
    ke3Digest: string,
    ciphertextB64: string,
    now: number,
    contextKey?: string,
  ): void;
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
  legacyResponseStore?: ConsumedResponseStore;
  cloudPairing?: boolean;
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
  { ok: true; responseB64: string } | { ok: false; reason: "invalid" };

export interface ActiveCode {
  generation: number;
  code: string;
  userIdentifier: string;
  registration: OpaqueRegistration;
  expiresAtMs: number;
}

export interface Handshake {
  generation: number;
  peer: string;
  mode: "local" | "cloud";
  serverLoginState: string;
  createdAtMs: number;
}

export interface PairingV1Manager {
  getStatus(): PairingV1Status;
  generateCode(): { code: string; expiresAtMs: number };
  cancel(): void;
  start(ke1: string, peer: string): StartResult;
  finish(
    handshakeId: string,
    ke3: string,
    encryptedMetadataB64: string,
    peer: string,
  ): FinishResult;
  startCloud(ke1: string, userId: string): StartResult;
  finishCloud(
    handshakeId: string,
    ke3: string,
    encryptedMetadataB64: string,
    userId: string,
    relayInstanceId: string,
  ): FinishResult;
}
