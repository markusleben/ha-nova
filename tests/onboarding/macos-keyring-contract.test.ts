import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("macOS keyring contract", () => {
  const content = readFileSync("cli/keyring_darwin.go", "utf8");

  it("trusts the security tool explicitly when writing the relay token", () => {
    expect(content).toContain('"security", "add-generic-password"');
    expect(content).not.toContain('"-T", "/usr/bin/security"');
    expect(content).toContain("relayAuthTokenServiceName()");
    expect(content).not.toContain("login.keychain-db");
  });
});
