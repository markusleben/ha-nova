import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("relay Unicode file contract", () => {
  const relayApi = readFileSync("skills/ha-nova/relay-api.md", "utf8");
  const relayApiNormalized = relayApi.replace(/\s+/g, " ");
  const ci = readFileSync(".github/workflows/ci.yml", "utf8");

  it("documents the strict cross-platform text boundary", () => {
    expect(relayApi).toContain("One leading UTF-8 BOM is accepted.");
    expect(relayApi).toContain(
      "UTF-16 and invalid/ambiguous UTF-8 are rejected before configuration lookup, authentication, or a Relay request.",
    );
    expect(relayApi).toContain(
      "prefer `ha-nova relay ... --out <result-file>` over shell redirection",
    );
    expect(relayApi).toContain(
      "BOM-less `Get-Content` uses the active legacy code page",
    );
    expect(relayApi).toContain(
      "A wrong read followed by a UTF-8 write produces valid but already-corrupted UTF-8 that no CLI can detect.",
    );
    expect(relayApi).toContain(
      "New-Object -TypeName System.Text.UTF8Encoding -ArgumentList $false, $true",
    );
    expect(relayApi).toContain("System.IO.File]::ReadAllBytes");
    expect(relayApi).toContain("$strictUtf8.GetString");
    expect(relayApi).toContain("`ReadAllText` is not strict enough");
    expect(relayApi).toContain("System.IO.File]::WriteAllText");
    expect(relayApiNormalized).toContain(
      "`--out` always writes BOM-less UTF-8.",
    );
  });

  it("runs the documented round trip in Windows PowerShell 5.1", () => {
    expect(ci).toContain("windows-powershell-unicode:");
    expect(ci).toContain("runs-on: windows-latest");
    expect(ci).toContain("$PSVersionTable.PSVersion.Major -ne 5");
    expect(ci).toContain(
      "New-Object -TypeName System.Text.UTF8Encoding -ArgumentList $false, $true",
    );
    expect(ci).toContain(
      '$expected = $strictUtf8.GetString($expectedBytes)',
    );
    expect(ci).not.toMatch(/[^\x00-\x7F]/);
    expect(ci).toContain("PowerShell 5.1 changed Unicode code points");
    expect(ci).toContain("UTF-8 test vector did not round-trip");
    expect(ci).toContain("Strict UTF-8 reader accepted");
    expect(ci).toContain("payload-bom.json");
    expect(ci).toContain("payload-double-bom.json");
    expect(ci).toContain(
      "Strict UTF-8 reader accepted more than one leading UTF-8 BOM",
    );
    expect(ci).toContain("go build -o ha-nova.exe .");
    expect(ci).toContain("ha-nova.exe relay jq");
    expect(ci).toContain("[Console]::OutputEncoding = $utf8NoBom");
    expect(ci).toContain("$cliHex -cne $expectedHex");
    expect(ci).toContain("Test(RelayUnicodeRoundTrip|RelayRejectsUTF16");
  });
});
