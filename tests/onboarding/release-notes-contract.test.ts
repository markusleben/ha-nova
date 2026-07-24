import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("release notes contract", () => {
  const goreleaser = readFileSync(".goreleaser.yml", "utf8");
  const readme = readFileSync("README.md", "utf8");
  const stableNotes = goreleaser
    .split("{{ else }}", 2)[1]
    ?.split("Stable install commands are release-pinned", 1)[0];

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

  it("keeps v0.21.3 release-facing wording aligned to distinct private Census counts", () => {
    expect(goreleaser).toContain(
      "Existing Census Yes choices will be asked again",
    );
    expect(goreleaser).toContain("Reporting stays off until Yes");
    expect(goreleaser).toContain("dedicated random Census ID");
    expect(goreleaser).toContain("Private maintainer stats");
    expect(goreleaser).toContain("Official Relay totals stay separate");
    expect(goreleaser).toContain("Cloudflare's role stays explicit");
    expect(goreleaser).toContain("receives source-IP/connection metadata");
    expect(goreleaser).toContain("does not read or store the IP");
    expect(goreleaser).toContain("no sooner than seven days later");
    expect(goreleaser).toContain("Home Status explains unavailable entities");
    expect(goreleaser).toContain("Unicode-safe Relay files");
    expect(goreleaser).not.toContain("ISO week");
    expect(readme).toContain("Census off by default");
    expect(readme).toContain("dedicated random Census installation ID");
    expect(readme).toContain("private maintainer statistics");
    expect(readme).not.toContain("public aggregate ping counts");
  });

  it("keeps every curated stable highlight below the 220-character digest cap", () => {
    expect(stableNotes).toBeTruthy();
    const bullets = stableNotes?.match(/^    - (.+)$/gm) ?? [];
    expect(bullets.length).toBeGreaterThan(0);
    for (const bullet of bullets) {
      expect([...bullet.slice(6)].length).toBeLessThanOrEqual(220);
    }
  });
});
