import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("windows keyring interop contract", () => {
  const goKeyring = readFileSync("cli/keyring_windows.go", "utf8");
  const shellPlatform = readFileSync("scripts/onboarding/platform/windows.sh", "utf8");

  it("keeps the Go runtime aware of the legacy DPAPI mirror used by the shell path", () => {
    expect(goKeyring).toContain("readLegacyWindowsRelayAuthToken");
    expect(goKeyring).toContain("writeLegacyWindowsRelayAuthToken");
    expect(goKeyring).toContain("deleteLegacyWindowsRelayAuthToken");
    expect(goKeyring).toContain(".dpapi");
    expect(goKeyring).toContain("ConvertTo-SecureString");
    expect(goKeyring).toContain("legacy Windows relay token mirror write failed");
    expect(goKeyring).toContain("legacy Windows relay token mirror cleanup failed");
  });

  it("keeps the shell path on the same legacy DPAPI filename contract", () => {
    expect(shellPlatform).toContain(".%s.dpapi");
    expect(shellPlatform).toContain("ConvertFrom-SecureString");
    expect(shellPlatform).toContain("ConvertTo-SecureString");
  });
});
