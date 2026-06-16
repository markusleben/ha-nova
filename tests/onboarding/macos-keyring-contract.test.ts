import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("macOS keyring contract", () => {
  const content = readFileSync("cli/keyring_darwin.go", "utf8");

  it("writes the relay token without exposing it in process argv", () => {
    // The write path must NOT pass the secret as a `security ... -w <token>`
    // command-line argument (visible via ps). It uses go-keyring, whose macOS
    // backend pipes the command through `security -i` (stdin) instead.
    expect(content).toContain("keyring.Set(service, u.Username, token)");
    expect(content).not.toContain('"-w", token');
    expect(content).not.toContain('"add-generic-password"');
    // No ACL-trust flag (which would prompt); same service; default keychain.
    expect(content).not.toContain('"-T", "/usr/bin/security"');
    expect(content).toContain("relayAuthTokenServiceName()");
    expect(content).not.toContain("login.keychain-db");
  });
});
