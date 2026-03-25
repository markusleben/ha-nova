import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("linux keyring contract", () => {
  const content = readFileSync("cli/keyring_linux.go", "utf8");

  it("keeps Linux on the go-keyring Secret Service path", () => {
    expect(content).toContain('//go:build linux');
    expect(content).toContain('github.com/zalando/go-keyring');
    expect(content).toContain("keyring.Get(service, u.Username)");
    expect(content).toContain("keyring.Set(relayAuthTokenServiceName(), u.Username, token)");
    expect(content).toContain("keyring.Delete(relayAuthTokenServiceName(), u.Username)");
    expect(content).toContain("relayAuthTokenReadError");
    expect(content).toContain("missingRelayAuthTokenError");
  });
});
