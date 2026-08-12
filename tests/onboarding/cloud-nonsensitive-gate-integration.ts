import { describe, expect, it } from "vitest";
import {
  type CloudPlatform,
  cloudGateFixture,
  runCloudGate,
  validCloudEvidence,
} from "./cloud-release-gate-fixture.js";
import { commitAll, write } from "./cloud-nonsensitive-source-helpers.js";

export function registerCloudNonsensitiveGateIntegrationTests(): void {
  describe("Cloud release gate wired non-sensitive escape", () => {
    function wiredFixture() {
      const platforms: CloudPlatform[] = ["linux"];
      const gateFixture = cloudGateFixture({
        cloud_remote_enabled: true,
        cloud_remote_platforms: platforms,
      });
      const evidence = validCloudEvidence(gateFixture, platforms);
      return { gateFixture, evidence };
    }

    it("carries stale evidence across a docs-only delta through the gate", () => {
      const { gateFixture, evidence } = wiredFixture();
      write(gateFixture.root, "docs/note.md", "carried docs delta\n");
      commitAll(gateFixture.root, "docs only");
      const result = runCloudGate(gateFixture, evidence);
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    });

    it("still rejects stale evidence across a script delta through the gate", () => {
      const { gateFixture, evidence } = wiredFixture();
      write(
        gateFixture.root,
        "scripts/release/new-tool.sh",
        "#!/usr/bin/env bash\n",
      );
      commitAll(gateFixture.root, "script delta");
      const result = runCloudGate(gateFixture, evidence);
      expect(result.status).not.toBe(0);
      expect(`${result.stdout}\n${result.stderr}`).toContain(
        "stale Home Assistant Cloud evidence",
      );
    });
  });
}
