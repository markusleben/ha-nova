import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("macOS keyring contract", () => {
  const content = readFileSync("cli/keyring_darwin.go", "utf8");
  const cloudOAuth = readFileSync(
    "cli/cloud_oauth_secret_darwin_api.go",
    "utf8",
  );
  const cloudBackend = readFileSync("cli/cloud_oauth_secret_darwin.go", "utf8");
  const cloudWorker = readFileSync("cli/native_secret_worker.go", "utf8");
  const cloudWorkerDarwin = readFileSync(
    "cli/native_secret_worker_darwin.go",
    "utf8",
  );
  const ciWorkflow = readFileSync(".github/workflows/ci.yml", "utf8");

  it("writes the relay token without exposing it in process argv", () => {
    // The write path must NOT pass the secret as a `security ... -w <token>`
    // command-line argument (visible via ps). It uses go-keyring, whose macOS
    // backend pipes the command through `security -i` (stdin) instead.
    expect(content).toContain("keyring.Set(service, u.Username, token)");
    expect(content).not.toContain('"-w", token');
    expect(content).not.toContain('"add-generic-password"');
    // No ACL-trust flag (which would prompt); same service; default keychain.
    expect(content).not.toContain('"-T", "/usr/bin/security"');
    expect(content).toContain("relayAuthTokenServiceName()");
    expect(content).not.toContain("login.keychain-db");
  });

  it("reads the relay token through go-keyring so the base64 envelope is decoded", () => {
    // go-keyring's Set base64-wraps the stored value (go-keyring-base64:...); the
    // read path MUST use keyring.Get to decode it. A raw `security
    // find-generic-password -w` read returns the encoded value and would
    // authenticate every relay call with the wrong bearer token.
    expect(content).toContain("keyring.Get(service, u.Username)");
    expect(content).toContain("keyring.ErrNotFound");
    expect(content).not.toContain('"find-generic-password"');
  });

  it("never broadens Cloud OAuth token access beyond the native caller ACL", () => {
    expect(cloudOAuth).toContain("SecKeychainAddGenericPassword");
    expect(cloudOAuth).not.toContain("darwinOAuthTrustAll");
    expect(cloudOAuth).not.toContain("SecACLSetContents");
    expect(cloudOAuth).not.toContain("SecKeychainItemSetAccess");
    expect(cloudOAuth).not.toContain("kSecACLAuthorizationDecrypt");
  });

  it("isolates native Cloud secrets behind the hardened worker boundary", () => {
    expect(cloudBackend).toContain("&darwinOAuthSecretBackend{}");
    expect(cloudBackend).toContain("boundedNativeOAuthSecretContext(ctx, ui)");
    expect(cloudBackend).toContain("runNativeSecretWorkerProcess");
    expect(cloudWorker).toContain("nativeSecretWorkerMaxInput");
    expect(cloudWorker).toContain("validNativeSecretWorkerKey");
    expect(cloudWorker).toContain("command.Stdin = bytes.NewReader(payload)");
    expect(cloudWorker).toContain("command.Stderr = io.Discard");
    expect(cloudWorkerDarwin).toContain("darwinCodeSigningRuntimeFlag");
    expect(cloudWorkerDarwin).toContain("darwinCodeSigningRequireLVFlag");
    expect(cloudWorkerDarwin).toContain("darwinCodeSigningDebuggedFlag");
    expect(cloudWorkerDarwin).toContain("subtle.ConstantTimeCompare");
  });

  it("runs native-secret boundary tests on native macOS and Windows runners", () => {
    expect(ciWorkflow).toContain("macos-native-secret-boundary:");
    expect(ciWorkflow).toContain(
      "TestDarwinNativeSecretWorkerRejectsNonHardenedExecutable",
    );
    expect(ciWorkflow).toContain("--options runtime,hard,kill,library");
    expect(ciWorkflow).toContain(
      "TestDarwinNativeSecretWorkerVerifiesSameExecutableParent",
    );
    expect(ciWorkflow).toContain("windows-native-secret-boundary:");
    expect(ciWorkflow).toContain(
      "TestWindowsOAuthNativeOperationsHonorHardDeadline",
    );
  });
});
