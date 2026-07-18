import { join } from "node:path";

import { readPrivateFileSync, writeFileAtomicSync } from "../storage/atomic-file.js";
import { PENDING_TTL_MS } from "./device-registry.js";
import type { ConsumedResponseStore } from "./pairing-v1.js";

// A durable ConsumedResponseStore so a lost finish response survives an App
// restart: the client retries POST /pair/v1/finish and gets back the exact same
// sealed credential instead of a dead code. Only ciphertext, a KE3 digest and a
// random handshake id are stored — the raw credential lives inside the blob that
// is sealed with the OPAQUE session key, which is never persisted. Entries are
// pruned once the pending credential they belong to would have expired
// (PENDING_TTL_MS), and capped so a handshake flood cannot grow the file.

const RESPONSE_STORE_FILE = "pairing-responses.json";
const MAX_RESPONSES = 32;
const RESPONSE_TTL_MS = PENDING_TTL_MS;
const MAX_FILE_BYTES = 256 * 1024;

interface StoredEntry {
  handshakeId: string;
  ke3Digest: string;
  ciphertextB64: string;
  expiresAtMs: number;
}

interface ResponseStoreLogger {
  warn(message: string, context?: Record<string, unknown>): void;
}

export function createFileResponseStore(
  dataDir: string,
  now: () => number,
  logger?: ResponseStoreLogger,
): ConsumedResponseStore {
  const path = join(dataDir, RESPONSE_STORE_FILE);
  let entries = load(path);

  const persist = (): void => {
    // Best-effort durability: the entry is already held in memory for this
    // process, so a persist failure only costs a lost retry after a restart —
    // never worse than the in-memory store. It must NOT throw out of finish(),
    // which would leave the pairing code half-consumed.
    try {
      writeFileAtomicSync(path, JSON.stringify({ version: 1, entries }));
    } catch (error) {
      logger?.warn("Could not persist the pairing finish response", { error: String((error as Error).message) });
    }
  };

  return {
    get(handshakeId) {
      const t = now();
      const entry = entries.find((e) => e.handshakeId === handshakeId && e.expiresAtMs > t);
      return entry ? { ke3Digest: entry.ke3Digest, ciphertextB64: entry.ciphertextB64 } : null;
    },
    put(handshakeId, ke3Digest, ciphertextB64, t) {
      // Drop this handshake's prior entry plus anything expired, append the fresh
      // record, then keep only the most recent MAX_RESPONSES.
      entries = entries.filter((e) => e.handshakeId !== handshakeId && e.expiresAtMs > t);
      entries.push({ handshakeId, ke3Digest, ciphertextB64, expiresAtMs: t + RESPONSE_TTL_MS });
      if (entries.length > MAX_RESPONSES) {
        entries = entries.slice(entries.length - MAX_RESPONSES);
      }
      persist();
    },
  };
}

function load(path: string): StoredEntry[] {
  try {
    const buffer = readPrivateFileSync(path, MAX_FILE_BYTES);
    if (buffer === null) {
      return [];
    }
    const parsed = JSON.parse(buffer.toString("utf8")) as unknown;
    const list = (parsed as { entries?: unknown })?.entries;
    return Array.isArray(list) ? list.filter(isValidEntry) : [];
  } catch {
    // A corrupt or unreadable store is not fatal: idempotent finish-retry is a
    // best-effort convenience, so pairing starts from an empty store rather than
    // crashing the relay.
    return [];
  }
}

function isValidEntry(entry: unknown): entry is StoredEntry {
  return (
    typeof entry === "object" &&
    entry !== null &&
    typeof (entry as StoredEntry).handshakeId === "string" &&
    typeof (entry as StoredEntry).ke3Digest === "string" &&
    typeof (entry as StoredEntry).ciphertextB64 === "string" &&
    typeof (entry as StoredEntry).expiresAtMs === "number"
  );
}
