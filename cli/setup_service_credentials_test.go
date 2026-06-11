package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShouldOfferServiceCredentialsCoversBlockedKeyringClasses(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"session unavailable", desktopKeyringSessionUnavailableError("no session bus"), true},
		{"provider unavailable", desktopKeyringUnavailableError("no Secret Service provider"), true},
		{"locked", desktopKeyringLockedError("default collection locked"), true},
		{"init required", desktopKeyringInitializationRequiredError("no default collection"), true},
		{"nil error", nil, false},
	}
	for _, tc := range cases {
		if got := shouldOfferServiceCredentials(tc.err); got != tc.want {
			t.Fatalf("%s: shouldOfferServiceCredentials() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFinalizeServiceTokenFileMigrationWritesKeyringBeforeDeleting(t *testing.T) {
	if relayAuthTokenFilePlatformOS == "windows" {
		t.Skip("service token files are not supported on native Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	keyringFile := filepath.Join(home, ".test-relay-auth-token")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", keyringFile)

	tokenDir := filepath.Join(home, ".config", "ha-nova")
	if err := os.MkdirAll(tokenDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	tokenPath := filepath.Join(tokenDir, "relay-token")
	if err := writeRelayAuthTokenFile(tokenPath, "migrated-token"); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	finalizeServiceTokenFileMigration(tokenPath, "migrated-token")

	stored, err := os.ReadFile(keyringFile)
	if err != nil {
		t.Fatalf("expected token to be written to the keyring before deletion: %v", err)
	}
	if strings.TrimSpace(string(stored)) != "migrated-token" {
		t.Fatalf("stored token = %q, want migrated-token", strings.TrimSpace(string(stored)))
	}
	if _, err := os.Stat(tokenPath); !isNotExist(err) {
		t.Fatalf("expected former service token file to be removed, err=%v", err)
	}
}

func TestDisableServiceRelayTokenFileKeepsExternalPaths(t *testing.T) {
	if relayAuthTokenFilePlatformOS == "windows" {
		t.Skip("service token files are not supported on native Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	externalDir := filepath.Join(home, "secrets")
	if err := os.MkdirAll(externalDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	externalToken := filepath.Join(externalDir, "relay-token")
	if err := writeRelayAuthTokenFile(externalToken, "external-token"); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	cfg, cleanupPath, formerToken, restore := disableServiceRelayTokenFile(paths, runtimeConfig{RelayTokenFile: externalToken})
	defer restore()
	if cfg.RelayTokenFile != "" {
		t.Fatalf("expected RelayTokenFile to be cleared, got %q", cfg.RelayTokenFile)
	}
	if cleanupPath != "" {
		t.Fatalf("expected no cleanup path for external token files, got %q", cleanupPath)
	}
	if formerToken != "external-token" {
		t.Fatalf("formerToken = %q, want external-token", formerToken)
	}

	finalizeServiceTokenFileMigration(cleanupPath, "external-token")
	if _, err := os.Stat(externalToken); err != nil {
		t.Fatalf("expected external token file to survive migration, err=%v", err)
	}
}
