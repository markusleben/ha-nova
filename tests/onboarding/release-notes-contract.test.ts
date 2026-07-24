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

  it("keeps v0.21.2 release-facing wording aligned to explicit census consent", () => {
    expect(goreleaser).toContain(
      "HA NOVA may ask for census consent again",
    );
    expect(goreleaser).toContain(
      "Census consent is now a clear choice",
    );
    expect(goreleaser).toContain(
      "choose **Yes**, **No**, or **Show exact data**",
    );
    expect(goreleaser).toContain("permits at most one per week");
    expect(goreleaser).toContain("Cloudflare, the hosting provider");
    expect(goreleaser).toContain(
      "receives the source IP for HTTPS delivery",
    );
    expect(goreleaser).toContain(
      "The JSON contains no IP; HA NOVA does not read or store it",
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

  it("keeps every curated stable highlight below the 220-character digest cap", () => {
    expect(stableNotes).toBeTruthy();
    const bullets = stableNotes?.match(/^    - (.+)$/gm) ?? [];
    expect(bullets.length).toBeGreaterThan(0);
    for (const bullet of bullets) {
      expect([...bullet.slice(6)].length).toBeLessThanOrEqual(220);
    }
  });
});
