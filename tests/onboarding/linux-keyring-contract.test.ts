import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("linux keyring contract", () => {
  const content = readFileSync("cli/keyring_linux.go", "utf8");
  const preflight = readFileSync("cli/keyring_preflight_linux.go", "utf8");
  const recovery = readFileSync("cli/setup_secure_storage_recovery.go", "utf8");
  const recoveryLinux = readFileSync(
    "cli/setup_secure_storage_recovery_linux.go",
    "utf8",
  );
  const secureStorage = readFileSync(
    "cli/keyring_linux_secure_storage.go",
    "utf8",
  );
  const cloudSecret = readFileSync("cli/cloud_oauth_secret_linux.go", "utf8");
  const cloudSecretPrompt = readFileSync(
    "cli/cloud_oauth_secret_linux_prompt.go",
    "utf8",
  );

  it("keeps Linux on bounded native Secret Service access behind testable wrappers", () => {
    expect(content).toContain("//go:build linux");
    expect(content).toContain("github.com/zalando/go-keyring");
    expect(content).toContain(
      "var keyringGetWithService = nativeLinuxKeyringGet",
    );
    expect(content).toContain(
      "var keyringSetWithService = nativeLinuxKeyringSet",
    );
    expect(content).toContain(
      "var keyringDeleteWithService = nativeLinuxKeyringDelete",
    );
    expect(content).toContain(
      "var newNativeLinuxCredentialBackend = newNativeCredentialSecretBackend",
    );
    expect(content).toContain("context.WithTimeout");
    expect(content).toContain("SecretStoreForbidUI");
    expect(content).not.toContain("var keyringGetWithService = keyring.Get");
    expect(content).not.toContain("var keyringSetWithService = keyring.Set");
    expect(content).not.toContain(
      "var keyringDeleteWithService = keyring.Delete",
    );
    expect(content).toContain(
      "readSecretWithService(relayAuthTokenServiceName())",
    );
    expect(content).toContain(
      "writeSecretWithService(relayAuthTokenServiceName(), token)",
    );
    expect(content).toContain(
      "deleteSecretWithService(relayAuthTokenServiceName())",
    );
    expect(content).toContain("relayAuthTokenReadError");
    expect(content).toContain("missingRelayAuthTokenError");
  });

  it("preflights Secret Service before setup writes tokens", () => {
    expect(preflight).toContain("inspectLinuxSecureStorageState()");
    expect(preflight).toContain("linuxSecureStorageStateNeedsInit");
    expect(preflight).toContain("desktopKeyringInitializationRequiredError");
    expect(preflight).toContain("linuxSecureStorageStateLocked");
    expect(preflight).toContain("desktopKeyringLockedError");
    expect(preflight).toContain("probeLinuxKeyringWritable()");
  });

  it("delegates recovery credentials exclusively to a trusted provider prompt", () => {
    expect(recovery).toContain(
      "Your Secret Service provider opens its own trusted desktop prompt",
    );
    expect(recovery).toContain("HA NOVA never reads it in the terminal.");
    expect(recovery).not.toContain("Passwords did not match.");
    expect(recovery).not.toContain("ReadPassword");
    expect(recoveryLinux).toContain(
      "platformNativeSecretPromptContextAvailable",
    );
    expect(recoveryLinux).toContain(
      "createLinuxSecureStorageCollectionForRecovery",
    );
    expect(recoveryLinux).toContain(
      "unlockLinuxSecureStorageCollectionForRecovery",
    );
    expect(recoveryLinux).toContain("platformSecureStorageRecoveryInitialize");
    expect(recoveryLinux).toContain("platformSecureStorageRecoveryUnlock");
    expect(recoveryLinux).toContain("SecretStoreAllowUI");
    expect(recoveryLinux).not.toContain("ReadPassword");
  });

  it("uses bounded D-Bus inspection and one provider-owned prompt at most", () => {
    expect(secureStorage).toContain("context.WithTimeout");
    expect(secureStorage).toContain("ReadAlias");
    expect(secureStorage).toContain(
      "normalizeLinuxKeyringErrorWithoutAmbiguousClassification",
    );
    expect(secureStorage).toContain("Secret Service preflight timed out");
    expect(cloudSecret).toContain(
      'secretServiceDBusInterface+".CreateCollection"',
    );
    expect(cloudSecret).toContain('secretServiceDBusInterface+".Unlock"');
    expect(cloudSecret).toContain("SecretStoreForbidUI");
    expect(cloudSecret).toContain("validateLinuxOAuthSecretUI");
    expect(cloudSecretPrompt).toContain('oauthSecretPromptInterface+".Prompt"');
    expect(cloudSecretPrompt).toContain(
      "linuxSecretServiceConsumePromptBudget",
    );
    expect(cloudSecretPrompt).toContain("if ui == SecretStoreForbidUI");
    expect(secureStorage).not.toContain("CreateWithMasterPassword");
    expect(secureStorage).not.toContain("UnlockWithMasterPassword");
  });
});
