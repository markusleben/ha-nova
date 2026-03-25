import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("windows keyring interop contract", () => {
  const goKeyring = readFileSync("cli/keyring_windows.go", "utf8");

  it("keeps the Go runtime aware of the legacy DPAPI file for one-shot migration and cleanup", () => {
    expect(goKeyring).toContain("readLegacyWindowsRelayAuthToken");
    expect(goKeyring).toContain("deleteLegacyWindowsRelayAuthToken");
    expect(goKeyring).toContain(".dpapi");
    expect(goKeyring).toContain("ConvertTo-SecureString");
    expect(goKeyring).toContain("legacy Windows relay token migration failed");
    expect(goKeyring).toContain("legacy Windows relay token cleanup failed after migration");
    expect(goKeyring).not.toContain("legacy Windows relay token mirror write failed");
    expect(goKeyring).not.toContain("writeLegacyWindowsRelayAuthToken");
    expect(goKeyring).toContain('filepath.Join(home, ".config", "ha-nova", "."+safe+".dpapi")');
  });
});
