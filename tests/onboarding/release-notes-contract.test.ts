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

  it("keeps v0.21.1 release-facing wording aligned to first-task update discovery", () => {
    expect(goreleaser).toContain(
      "Relay App updates now surface with the first Home Assistant task",
    );
    expect(goreleaser).toContain("registry-proven NOVA Relay App");
    expect(goreleaser).toContain("last-second state/provenance recheck");
    expect(goreleaser).toContain("latest-at-execution installation");
    expect(goreleaser).toContain("Start a new AI session after updating");
    expect(goreleaser).toContain(
      "Available updates are visible again on the first Home Assistant task",
    );
    expect(goreleaser).toContain("Supported v0.20+ copied client installs");
    expect(goreleaser).toContain(
      "Older, foreign, or unreadable layouts still fail closed",
    );
    expect(goreleaser).not.toContain(
      "Optional public census, off until you opt in",
    );
    expect(readme).toContain("Census off by default");
    expect(readme).toContain("public aggregate ping counts");
    expect(readme).toContain("not verified unique installs");
  });
});
