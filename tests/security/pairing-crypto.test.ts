import { randomBytes } from "node:crypto";

import { describe, expect, it } from "vitest";

import { deriveDirectionKeys, open, seal } from "../../nova/src/security/pairing-crypto.js";

const sessionKey = () => randomBytes(64);
const hsid = () => randomBytes(16);

describe("pairing-crypto", () => {
  it("round-trips a sealed frame in the same direction", () => {
    const sk = sessionKey();
    const id = hsid();
    const keys = deriveDirectionKeys(sk, id);
    const msg = Buffer.from("device-credential-and-pin");
    const blob = seal(keys.s2c, id, "s2c", msg);
    expect(open(keys.s2c, id, "s2c", blob)?.equals(msg)).toBe(true);
  });

  it("both parties derive identical directional keys", () => {
    const sk = sessionKey();
    const id = hsid();
    const a = deriveDirectionKeys(sk, id);
    const b = deriveDirectionKeys(Buffer.from(sk), Buffer.from(id));
    expect(a.c2s.equals(b.c2s)).toBe(true);
    expect(a.s2c.equals(b.s2c)).toBe(true);
    // Directions are distinct keys.
    expect(a.c2s.equals(a.s2c)).toBe(false);
  });

  it("refuses to open a frame sealed for the other direction (no cross-direction replay)", () => {
    const sk = sessionKey();
    const id = hsid();
    const keys = deriveDirectionKeys(sk, id);
    const blob = seal(keys.c2s, id, "c2s", Buffer.from("client-metadata"));
    // Attacker replays the c2s frame as if it were s2c: wrong key AND wrong AAD.
    expect(open(keys.s2c, id, "s2c", blob)).toBeNull();
  });

  it("refuses a frame from a different handshake id", () => {
    const sk = sessionKey();
    const id1 = hsid();
    const id2 = hsid();
    const keys1 = deriveDirectionKeys(sk, id1);
    const blob = seal(keys1.s2c, id1, "s2c", Buffer.from("x"));
    const keys2 = deriveDirectionKeys(sk, id2);
    expect(open(keys2.s2c, id2, "s2c", blob)).toBeNull();
  });

  it("rejects a tampered ciphertext or tag", () => {
    const sk = sessionKey();
    const id = hsid();
    const keys = deriveDirectionKeys(sk, id);
    const blob = seal(keys.s2c, id, "s2c", Buffer.from("secret"));
    const tampered = Buffer.from(blob);
    tampered[tampered.length - 1] = (tampered[tampered.length - 1] ?? 0) ^ 0x01; // flip a tag bit
    expect(open(keys.s2c, id, "s2c", tampered)).toBeNull();
    const short = blob.subarray(0, 10);
    expect(open(keys.s2c, id, "s2c", short)).toBeNull();
  });
});
