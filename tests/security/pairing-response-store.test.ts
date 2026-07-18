import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { chmodSync, mkdtempSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { PENDING_TTL_MS } from "../../nova/src/security/device-registry.js";
import { createFileResponseStore } from "../../nova/src/security/pairing-response-store.js";

const STORE_FILE = "pairing-responses.json";

describe("pairing response store (durable)", () => {
  let dir: string;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "ha-nova-resp-"));
  });

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  it("returns the exact stored ciphertext for a matching handshake and null otherwise", () => {
    const t = 1_000;
    const store = createFileResponseStore(dir, () => t);
    store.put("hs1", "digestA", "cipherA", t);
    expect(store.get("hs1")).toEqual({ ke3Digest: "digestA", ciphertextB64: "cipherA" });
    expect(store.get("unknown")).toBeNull();
  });

  it("survives a restart: a fresh instance reads the persisted response from /data", () => {
    const t = 1_000;
    createFileResponseStore(dir, () => t).put("hs1", "digestA", "cipherA", t);
    // A brand-new store (simulating an App restart) reads the same file.
    const afterRestart = createFileResponseStore(dir, () => t);
    expect(afterRestart.get("hs1")).toEqual({ ke3Digest: "digestA", ciphertextB64: "cipherA" });
  });

  it("expires an entry once the pending-credential window has passed", () => {
    let t = 1_000;
    const store = createFileResponseStore(dir, () => t);
    store.put("hs1", "digestA", "cipherA", t);
    t += PENDING_TTL_MS + 1;
    expect(store.get("hs1")).toBeNull();
  });

  it("replaces a prior entry for the same handshake id", () => {
    const t = 1_000;
    const store = createFileResponseStore(dir, () => t);
    store.put("hs1", "digestA", "cipherA", t);
    store.put("hs1", "digestB", "cipherB", t);
    expect(store.get("hs1")).toEqual({ ke3Digest: "digestB", ciphertextB64: "cipherB" });
  });

  it("caps the store so a handshake flood cannot grow the file without bound", () => {
    const t = 1_000;
    const store = createFileResponseStore(dir, () => t);
    for (let i = 0; i < 40; i += 1) {
      store.put(`hs${i}`, `d${i}`, `c${i}`, t);
    }
    expect(store.get("hs0")).toBeNull(); // oldest evicted
    expect(store.get("hs39")).toEqual({ ke3Digest: "d39", ciphertextB64: "c39" }); // newest kept
  });

  it("starts empty when the persisted file is corrupt rather than crashing pairing", () => {
    writeFileSync(join(dir, STORE_FILE), "{ not json", { mode: 0o600 });
    const t = 1_000;
    const store = createFileResponseStore(dir, () => t);
    expect(store.get("hs1")).toBeNull();
    // And it can still persist new entries over the corrupt file.
    store.put("hs1", "digestA", "cipherA", t);
    expect(store.get("hs1")).toEqual({ ke3Digest: "digestA", ciphertextB64: "cipherA" });
  });

  it("writes the store as an owner-only (0600) file", () => {
    const t = 1_000;
    createFileResponseStore(dir, () => t).put("hs1", "digestA", "cipherA", t);
    expect(statSync(join(dir, STORE_FILE)).mode & 0o777).toBe(0o600);
  });

  it("degrades to memory without throwing when the store cannot be persisted", () => {
    const t = 1_000;
    const warnings: string[] = [];
    const store = createFileResponseStore(dir, () => t, { warn: (message) => warnings.push(message) });
    store.put("hs1", "digestA", "cipherA", t); // persists fine

    chmodSync(dir, 0o500); // make the data dir unwritable so the next atomic write fails
    try {
      // A finish response must never throw out of put() — that would leave the
      // pairing code half-consumed. It falls back to in-memory for this session.
      expect(() => store.put("hs2", "digestB", "cipherB", t)).not.toThrow();
      expect(store.get("hs2")).toEqual({ ke3Digest: "digestB", ciphertextB64: "cipherB" });
      expect(warnings.length).toBeGreaterThan(0);
    } finally {
      chmodSync(dir, 0o700); // restore so afterEach cleanup succeeds
    }
  });
});
