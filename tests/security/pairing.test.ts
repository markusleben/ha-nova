import { describe, expect, it } from "vitest";

import {
  createPairingManager,
  PAIRING_CODE_TTL_MS,
  PAIRING_GLOBAL_FAILURE_LIMIT,
  PAIRING_GLOBAL_WINDOW_MS,
  PAIRING_MAX_TRACKED_PEERS,
  PAIRING_PEER_FAILURE_LIMIT,
  PAIRING_PEER_WINDOW_MS
} from "../../nova/src/security/pairing.js";

describe("pairing manager", () => {
  it("zero-pads codes, returns the relay token once, and rotates immediately", () => {
    const numbers = [123, 456];
    const pairing = createPairingManager({
      relayToken: "relay-secret",
      now: () => 1_000,
      generateCodeNumber: () => numbers.shift()!
    });

    expect(pairing.getStatus()).toEqual({ code: "000123", expiresAtMs: 1_000 + PAIRING_CODE_TTL_MS });
    expect(pairing.exchange("000123", "peer-a")).toEqual({
      ok: true,
      relayToken: "relay-secret"
    });
    expect(pairing.getStatus().code).toBe("000456");
    expect(pairing.exchange("000123", "peer-a")).toEqual({ ok: false, reason: "invalid" });
  });

  it("rotates an expired code and gives expired and wrong codes the same result", () => {
    let now = 0;
    const numbers = [111_111, 222_222];
    const pairing = createPairingManager({
      relayToken: "relay-secret",
      now: () => now,
      generateCodeNumber: () => numbers.shift()!
    });

    now = PAIRING_CODE_TTL_MS;
    expect(pairing.exchange("111111", "peer-a")).toEqual({ ok: false, reason: "invalid" });
    expect(pairing.getStatus()).toEqual({
      code: "222222",
      expiresAtMs: PAIRING_CODE_TTL_MS * 2
    });
  });

  it("blocks a peer after five failed attempts for the rest of its fixed window", () => {
    let now = 10_000;
    const numbers = [123_456, 654_321];
    const pairing = createPairingManager({
      relayToken: "relay-secret",
      now: () => now,
      generateCodeNumber: () => numbers.shift()!
    });

    for (let attempt = 0; attempt < PAIRING_PEER_FAILURE_LIMIT; attempt += 1) {
      expect(pairing.exchange("999999", "peer-a")).toEqual({ ok: false, reason: "invalid" });
    }
    expect(pairing.exchange("123456", "peer-a")).toEqual({
      ok: false,
      reason: "rate_limited",
      retryAfterSeconds: 60
    });

    now += PAIRING_PEER_WINDOW_MS;
    expect(pairing.exchange("123456", "peer-a")).toEqual({
      ok: true,
      relayToken: "relay-secret"
    });
  });

  it("blocks all peers after 30 global failures until the global window resets", () => {
    let now = 20_000;
    const numbers = [123_456, 654_321];
    const pairing = createPairingManager({
      relayToken: "relay-secret",
      now: () => now,
      generateCodeNumber: () => numbers[0]!
    });

    for (let attempt = 0; attempt < PAIRING_GLOBAL_FAILURE_LIMIT; attempt += 1) {
      expect(pairing.exchange("999999", `peer-${attempt}`)).toEqual({
        ok: false,
        reason: "invalid"
      });
    }
    expect(pairing.exchange("123456", "fresh-peer")).toEqual({
      ok: false,
      reason: "rate_limited",
      retryAfterSeconds: 300
    });

    now += PAIRING_GLOBAL_WINDOW_MS;
    numbers.shift();
    expect(pairing.exchange("123456", "fresh-peer")).toEqual({
      ok: true,
      relayToken: "relay-secret"
    });
  });

  it("pins the bounded peer-table contract and rejects broken code generators", () => {
    expect(PAIRING_MAX_TRACKED_PEERS).toBe(256);
    expect(() =>
      createPairingManager({
        relayToken: "relay-secret",
        generateCodeNumber: () => 1_000_000
      })
    ).toThrowError("Pairing code generator returned an out-of-range value");
  });

  it("fails loud instead of looping when a generator repeats a used code", () => {
    const pairing = createPairingManager({
      relayToken: "relay-secret",
      generateCodeNumber: () => 123_456
    });

    expect(() => pairing.exchange("123456", "peer-a")).toThrowError(
      "Pairing code generator repeated the previous code"
    );
  });
});
