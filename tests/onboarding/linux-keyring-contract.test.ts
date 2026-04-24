import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("linux keyring contract", () => {
  const content = readFileSync("cli/keyring_linux.go", "utf8");
  const preflight = readFileSync("cli/keyring_preflight_linux.go", "utf8");
  const recovery = readFileSync("cli/setup_secure_storage_recovery.go", "utf8");
  const recoveryLinux = readFileSync("cli/setup_secure_storage_recovery_linux.go", "utf8");
  const secureStorage = readFileSync("cli/keyring_linux_secure_storage.go", "utf8");

  it("keeps Linux on the go-keyring Secret Service path behind testable wrappers", () => {
    expect(content).toContain('//go:build linux');
    expect(content).toContain('github.com/zalando/go-keyring');
    expect(content).toContain("var keyringGetWithService = keyring.Get");
    expect(content).toContain("var keyringSetWithService = keyring.Set");
    expect(content).toContain("var keyringDeleteWithService = keyring.Delete");
    expect(content).toContain("readSecretWithService(relayAuthTokenServiceName())");
    expect(content).toContain("writeSecretWithService(relayAuthTokenServiceName(), token)");
    expect(content).toContain("deleteSecretWithService(relayAuthTokenServiceName())");
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

  it("keeps inline recovery limited to explicit local Linux keyring copy", () => {
    expect(recovery).toContain("local Linux keyring password");
    expect(recovery).toContain("not the Relay token or the Home Assistant token");
    expect(recovery).toContain("HA NOVA, NOVA Relay, and Home Assistant never receive it.");
    expect(recovery).toContain("Passwords did not match.");
    expect(recovery).toContain("local Linux keyring password was rejected");
    expect(recoveryLinux).toContain("supportsGNOMEKeyringRecovery()");
    expect(recoveryLinux).toContain("secureStorageRecoverySupportsGNOMEMethods");
    expect(recoveryLinux).toContain("platformSecureStorageRecoveryInitialize");
    expect(recoveryLinux).toContain("platformSecureStorageRecoveryUnlock");
  });

  it("uses bounded D-Bus inspection instead of surfacing raw Secret Service failures", () => {
    expect(secureStorage).toContain("context.WithTimeout");
    expect(secureStorage).toContain("ReadAlias");
    expect(secureStorage).toContain("CreateWithMasterPassword");
    expect(secureStorage).toContain("UnlockWithMasterPassword");
    expect(secureStorage).toContain("normalizeLinuxKeyringErrorWithoutAmbiguousClassification");
    expect(secureStorage).toContain("Secret Service preflight timed out");
  });
});
