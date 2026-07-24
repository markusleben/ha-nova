import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("release notes contract", () => {
  const goreleaser = readFileSync(".goreleaser.yml", "utf8");
  const readme = readFileSync("README.md", "utf8");

  it("keeps release notes aligned to the single supported Windows install path", () => {
    expect(goreleaser).toContain(
      "Stable install commands are release-pinned for this tag.",
    );
    expect(goreleaser).toContain(
      "https://raw.githubusercontent.com/markusleben/ha-nova/{{ .Tag }}/install.sh",
    );
    expect(goreleaser).toContain("HA_NOVA_VERSION={{ .Tag }}");
    expect(goreleaser).toContain(
      "https://raw.githubusercontent.com/markusleben/ha-nova/{{ .Tag }}/install.ps1",
    );
    expect(goreleaser).toContain(
      "Windows uses a single supported install path: `install.ps1`.",
    );
    expect(goreleaser).not.toContain(
      "raw.githubusercontent.com/markusleben/ha-nova/main/install.sh",
    );
    expect(goreleaser).not.toContain(
      "raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1",
    );
    expect(goreleaser).not.toContain(
      "$ProgressPreference = 'SilentlyContinue'",
    );
    expect(goreleaser).not.toContain("winget");
  });

  it("keeps v0.21.2 release-facing wording aligned to explicit census consent", () => {
    expect(goreleaser).toContain(
      "HA NOVA may ask for census consent again",
    );
    expect(goreleaser).toContain(
      "Census consent is now an explicit, inspectable choice",
    );
    expect(goreleaser).toContain(
      "separate **Yes**, **No**, and **Show exact data** actions",
    );
    expect(goreleaser).toContain("at most one report per week");
    expect(goreleaser).toContain("Cloudflare, the hosting provider");
    expect(goreleaser).toContain(
      "receives the source IP as connection metadata",
    );
    expect(goreleaser).toContain(
      "does not include it in the JSON payload or read or store it",
    );
    expect(goreleaser).toContain(
      "Older informational notices no longer count as consent prompts",
    );
    expect(goreleaser).not.toContain("ISO week");
    expect(goreleaser).not.toContain("seven days");
    expect(readme).toContain("Census off by default");
    expect(readme).toContain("public aggregate ping counts");
    expect(readme).toContain("not verified unique installs");
  });
});
