import { describe, expect, it } from "vitest";
import {
  CLOUD_CHECK_NAMES,
  type CloudPlatform,
  cloudGateFixture,
  runCloudGate,
  validCloudEvidence,
} from "./cloud-release-gate-fixture.js";
import { registerCloudReleaseCommitGateBehaviorTests } from "./cloud-release-commit-gate-behavior.js";
export function registerCloudReleaseGateBehaviorTests(): void {
  describe("Home Assistant Cloud release gate behavior", () => {
    it("allows a disabled release without external evidence", () => {
      const fixture = cloudGateFixture({
        cloud_remote_enabled: false,
        cloud_remote_platforms: [],
      });
      const result = runCloudGate(fixture);
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain("Cloud remote disabled");
    });
    it.each([
      [
        "root disabled while the App is enabled",
        {
          cloud_remote_enabled: false,
          cloud_remote_platforms: [],
        },
        {
          cloud_remote_enabled: true,
          cloud_remote_platforms: ["darwin"],
        },
      ],
      [
        "App disabled while the root is enabled",
        {
          cloud_remote_enabled: true,
          cloud_remote_platforms: ["darwin"],
        },
        {
          cloud_remote_enabled: false,
          cloud_remote_platforms: [],
        },
      ],
      [
        "enabled platform lists differ",
        {
          cloud_remote_enabled: true,
          cloud_remote_platforms: ["darwin"],
        },
        {
          cloud_remote_enabled: true,
          cloud_remote_platforms: ["linux"],
        },
      ],
    ])(
      "rejects root/App metadata drift before the disabled early exit: %s",
      (_name, version, appVersion) => {
        const fixture = cloudGateFixture(version, appVersion);
        const result = runCloudGate(fixture);
        expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
        expect(`${result.stdout}\n${result.stderr}`).toContain(
          "Cloud release metadata must match exactly",
        );
        expect(result.stdout).not.toContain("Cloud remote disabled");
      },
    );
    it.each([
      [
        "disabled with a platform",
        { cloud_remote_enabled: false, cloud_remote_platforms: ["darwin"] },
      ],
      [
        "enabled without a platform",
        { cloud_remote_enabled: true, cloud_remote_platforms: [] },
      ],
      [
        "duplicate platform",
        {
          cloud_remote_enabled: true,
          cloud_remote_platforms: ["darwin", "darwin"],
        },
      ],
      [
        "unknown platform",
        { cloud_remote_enabled: true, cloud_remote_platforms: ["solaris"] },
      ],
      [
        "non-boolean switch",
        { cloud_remote_enabled: "false", cloud_remote_platforms: [] },
      ],
      [
        "non-array platforms",
        { cloud_remote_enabled: false, cloud_remote_platforms: "darwin" },
      ],
    ])("rejects invalid version metadata: %s", (_name, version) => {
      const fixture = cloudGateFixture(version);
      const result = runCloudGate(fixture);
      expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
    });
    it("requires valid evidence without disclosing the payload", () => {
      const fixture = cloudGateFixture({
        cloud_remote_enabled: true,
        cloud_remote_platforms: ["linux"],
      });
      expect(runCloudGate(fixture).status).not.toBe(0);
      const marker = "private-evidence-metadata-marker";
      const result = runCloudGate(fixture, `{"${marker}":`);
      expect(result.status).not.toBe(0);
      expect(`${result.stdout}\n${result.stderr}`).not.toContain(marker);
    });
    it("accepts complete evidence bound to the exact checked-out commit", () => {
      const platforms: CloudPlatform[] = ["linux", "windows"];
      const fixture = cloudGateFixture({
        cloud_remote_enabled: true,
        cloud_remote_platforms: platforms,
      });
      const result = runCloudGate(
        fixture,
        validCloudEvidence(fixture, platforms),
      );
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain(
        "full-tree-bound Cloud evidence verified for 2 platform(s)",
      );
    });
    it("accepts Darwin only with its complete exact-commit evidence", () => {
      const platforms: CloudPlatform[] = ["darwin"];
      const fixture = cloudGateFixture({
        cloud_remote_enabled: true,
        cloud_remote_platforms: platforms,
      });
      const result = runCloudGate(
        fixture,
        validCloudEvidence(fixture, platforms),
      );
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain(
        "full-tree-bound Cloud evidence verified for 1 platform(s)",
      );
    });
    it.each(["0.7.1", "0.7.0", "0.6.99", "garbage", "01.0.0"])(
      "rejects Relay App version %s instead of treating any non-0.7.1 string as newer",
      (relayVersion) => {
        const version = {
          min_relay_version: relayVersion,
          cloud_remote_enabled: true,
          cloud_remote_platforms: ["linux"],
        };
        const fixture = cloudGateFixture(version);
        const result = runCloudGate(
          fixture,
          validCloudEvidence(fixture, ["linux"]),
        );
        expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
      },
    );
    it("requires min_relay_version to match the installed App version", () => {
      const fixture = cloudGateFixture(
        {
          min_relay_version: "0.8.0",
          cloud_remote_enabled: true,
          cloud_remote_platforms: ["linux"],
        },
        undefined,
        "0.8.1",
      );
      const result = runCloudGate(
        fixture,
        validCloudEvidence(fixture, ["linux"]),
      );
      expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
      expect(`${result.stdout}\n${result.stderr}`).toContain(
        "must exactly match nova/config.yaml",
      );
    });
    it("binds installed Relay App evidence to the exact source commit", () => {
      const fixture = cloudGateFixture({
        cloud_remote_enabled: true,
        cloud_remote_platforms: ["linux"],
      });
      const evidence = validCloudEvidence(fixture, ["linux"]);
      (evidence.relay_app as Record<string, unknown>).source_commit =
        "0".repeat(40);
      const result = runCloudGate(fixture, evidence);
      expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
      expect(`${result.stdout}\n${result.stderr}`).toContain(
        "source_commit does not match",
      );
    });
    it("rejects evidence or workflow context for any other commit", () => {
      const platforms: CloudPlatform[] = ["linux"];
      const fixture = cloudGateFixture({
        cloud_remote_enabled: true,
        cloud_remote_platforms: platforms,
      });
      const wrongEvidence = validCloudEvidence("0".repeat(40), platforms);
      expect(runCloudGate(fixture, wrongEvidence).status).not.toBe(0);
      expect(
        runCloudGate(
          fixture,
          validCloudEvidence(fixture, platforms),
          "0".repeat(40),
        ).status,
      ).not.toBe(0);
    });
    it.each(CLOUD_CHECK_NAMES)("rejects an incomplete %s matrix", (check) => {
      const platforms: CloudPlatform[] = ["linux"];
      const fixture = cloudGateFixture({
        cloud_remote_enabled: true,
        cloud_remote_platforms: platforms,
      });
      const evidence = validCloudEvidence(fixture, platforms);
      const checks = evidence.checks as Record<string, unknown>;
      checks[check] = false;
      const result = runCloudGate(fixture, evidence);
      expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
    });
    it("requires the exact enabled-platform keyring matrix", () => {
      const platforms: CloudPlatform[] = ["linux", "windows"];
      const fixture = cloudGateFixture({
        cloud_remote_enabled: true,
        cloud_remote_platforms: platforms,
      });
      const missing = validCloudEvidence(fixture, platforms);
      const missingKeyrings = (
        missing.checks as { keyrings: Record<string, boolean> }
      ).keyrings;
      delete missingKeyrings.windows;
      expect(runCloudGate(fixture, missing).status).not.toBe(0);
      const extra = validCloudEvidence(fixture, platforms);
      (extra.checks as { keyrings: Record<string, boolean> }).keyrings.darwin =
        true;
      expect(runCloudGate(fixture, extra).status).not.toBe(0);
      const falseResult = validCloudEvidence(fixture, platforms);
      (
        falseResult.checks as { keyrings: Record<string, boolean> }
      ).keyrings.windows = false;
      expect(runCloudGate(fixture, falseResult).status).not.toBe(0);
    });
    it("rejects schema drift instead of ignoring unknown fields", () => {
      const platforms: CloudPlatform[] = ["linux"];
      const fixture = cloudGateFixture({
        cloud_remote_enabled: true,
        cloud_remote_platforms: platforms,
      });
      const evidence = validCloudEvidence(fixture, platforms);
      evidence.unreviewed = true;
      expect(runCloudGate(fixture, evidence).status).not.toBe(0);
    });
  });
  registerCloudReleaseCommitGateBehaviorTests();
}
