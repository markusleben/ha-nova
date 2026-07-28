import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("macOS keyring contract", () => {
  const content = readFileSync("cli/keyring_darwin.go", "utf8");
  const cloudOAuth = readFileSync(
    "cli/cloud_oauth_secret_darwin_api.go",
    "utf8",
  );
  const interaction = readFileSync(
    "cli/keyring_darwin_interaction.go",
    "utf8",
  );
  const cloudBackend = readFileSync("cli/cloud_oauth_secret_darwin.go", "utf8");
  const cloudWorker = readFileSync("cli/native_secret_worker.go", "utf8");
  const cloudWorkerDarwin = readFileSync(
    "cli/native_secret_worker_darwin.go",
    "utf8",
  );
  const darwinTestBackend = readFileSync(
    "cli/keyring_darwin_test_backend_test.go",
    "utf8",
  );
  const ciWorkflow = readFileSync(".github/workflows/ci.yml", "utf8");

  it("writes the relay token through the same native Keychain identity", () => {
    // The token never enters subprocess argv, and matching native read/write
    // identity keeps later non-interactive reads inside the stored item ACL.
    expect(content).toContain("setDarwinSecretInProcess(");
    expect(content).toContain("nativeSecretSet");
    expect(content).toContain("SecretStoreAllowUI");
    expect(content).not.toContain('"-w", token');
    expect(content).not.toContain('"add-generic-password"');
    expect(content).not.toContain("keyring.Set(service, u.Username, token)");
    expect(content).not.toContain('"-T", "/usr/bin/security"');
    expect(content).toContain("relayAuthTokenServiceName()");
    expect(content).not.toContain("login.keychain-db");
  });

  it("forbids Keychain UI for background reads and decodes legacy values", () => {
    expect(content).toContain("readDarwinSecretInProcess(");
    expect(content).toContain("SecretStoreForbidUI");
    expect(content).toContain("decodeDarwinGoKeyringValue(token)");
    expect(content).toContain("go-keyring-base64:");
    expect(interaction).toContain("setDarwinKeychainInteraction(ui)");
    expect(interaction).toContain("darwinKeychainInteractionSemaphore");
    expect(interaction).toContain("darwinOAuthErrorCode(status, ui)");
    expect(content).toContain("keyring.ErrNotFound");
    expect(content).not.toContain("keyring.Get(service, u.Username)");
    expect(content).not.toContain("exec.Command");
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
    expect(ciWorkflow).toContain("Verify macOS Keychain no-UI policy");
    expect(ciWorkflow).toContain("DoctorResume");
    expect(ciWorkflow).toContain(
      'deny process-exec (literal "/usr/bin/security")',
    );
    expect(ciWorkflow).toContain("windows-native-secret-boundary:");
    expect(ciWorkflow).toContain(
      "TestWindowsOAuthNativeOperationsHonorHardDeadline",
    );
  });

  it("mocks go-keyring before any macOS package test can access Keychain", () => {
    expect(darwinTestBackend).toContain("func init()");
    expect(darwinTestBackend).toContain("keyring.MockInit()");
  });
});
