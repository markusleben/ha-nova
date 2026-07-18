import { describe, expect, it } from "vitest";

import { CSRF_MAX_PENDING, CSRF_TTL_MS, createCsrfStore } from "../../nova/src/security/csrf.js";

describe("csrf store", () => {
  it("issues a token that consumes once for the same owner+action", () => {
    const s = createCsrfStore();
    const t = s.issue("owner", "generate_code", 1000);
    expect(s.consume("owner", "generate_code", t, 1000)).toBe(true);
    // Single use: the second consume fails.
    expect(s.consume("owner", "generate_code", t, 1000)).toBe(false);
  });

  it("binds the token to the action and the owner", () => {
    const s = createCsrfStore();
    const t = s.issue("owner", "generate_code", 1000);
    expect(s.consume("owner", "revoke_device", t, 1000)).toBe(false); // wrong action
    expect(s.consume("intruder", "generate_code", t, 1000)).toBe(false); // wrong owner
    // Original still valid.
    expect(s.consume("owner", "generate_code", t, 1000)).toBe(true);
  });

  it("expires tokens after the TTL", () => {
    const s = createCsrfStore();
    const t = s.issue("owner", "a", 1000);
    expect(s.consume("owner", "a", t, 1000 + CSRF_TTL_MS + 1)).toBe(false);
  });

  it("rejects an unknown token", () => {
    const s = createCsrfStore();
    expect(s.consume("owner", "a", "not-a-real-token", 1000)).toBe(false);
  });

  it("caps pending tokens, evicting the oldest", () => {
    const s = createCsrfStore();
    const first = s.issue("owner", "a", 1000);
    for (let i = 0; i < CSRF_MAX_PENDING; i++) {
      s.issue("owner", "a", 1000 + i + 1); // fill past the cap
    }
    // The oldest (first) was evicted.
    expect(s.consume("owner", "a", first, 1000)).toBe(false);
  });
});
