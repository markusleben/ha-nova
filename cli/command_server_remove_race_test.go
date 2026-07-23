package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerRemoveRejectsNewlyReachableCredentialDuringConfirmation(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	pendingPath, err := deviceSecretFilePath(deviceCredentialPendingServiceForProfile("cabin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(pendingPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pendingPath, []byte("old-raw-credential"), 0o600); err != nil {
		t.Fatal(err)
	}

	reachable := false
	previousGet := secretGetForServerRemove
	secretGetForServerRemove = func(string) (string, error) {
		if reachable {
			return "new-keyring-credential", nil
		}
		return "", errDesktopKeyringSessionUnavailable
	}
	t.Cleanup(func() { secretGetForServerRemove = previousGet })
	previousConfirm := readServerRemoveConfirmationForCommand
	readServerRemoveConfirmationForCommand = func(string) (string, error) {
		reachable = true
		return "cabin", nil
	}
	t.Cleanup(func() { readServerRemoveConfirmationForCommand = previousConfirm })
	revokedAt := stubServerRevoke(t)

	exit, output := captureCommandOutput(t, func() int {
		return runServerCommand(paths, []string{"remove", "cabin"})
	})
	if exit != 1 || !strings.Contains(output, "secure storage changed while awaiting confirmation") {
		t.Fatalf("remove did not reject newly reachable credential: exit=%d\n%s", exit, output)
	}
	if len(*revokedAt) != 0 {
		t.Fatalf("new credential was revoked: %v", *revokedAt)
	}
	if !fileExists(pendingPath) {
		t.Fatal("raw credential was removed")
	}
}

func TestServerRemoveRejectsUninstallSetupABADuringConfirmation(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	if err := secretSet(deviceCredentialServiceForProfile("cabin"), testProfileCredentialB); err != nil {
		t.Fatal(err)
	}
	previousConfirm := readServerRemoveConfirmationForCommand
	readServerRemoveConfirmationForCommand = func(string) (string, error) {
		if err := markCensusLifecycleStopped(paths); err != nil {
			t.Fatal(err)
		}
		replacementLifecycle := [][]byte{
			captureInstallLifecycleGeneration(paths),
			captureCensusLifecycleMarker(paths),
		}
		if err := completeSetupLifecycle(paths, replacementLifecycle...); err != nil {
			t.Fatal(err)
		}
		return "cabin", nil
	}
	t.Cleanup(func() { readServerRemoveConfirmationForCommand = previousConfirm })
	revokedAt := stubServerRevoke(t)

	exit, output := captureCommandOutput(t, func() int {
		return runServerCommand(paths, []string{"remove", "cabin"})
	})
	if exit != 1 || !strings.Contains(output, "install lifecycle changed while awaiting confirmation") {
		t.Fatalf("remove did not reject uninstall/setup ABA: exit=%d\n%s", exit, output)
	}
	if len(*revokedAt) != 0 {
		t.Fatalf("replacement install credential was revoked: %v", *revokedAt)
	}
	if _, ok, err := readCredentialSlot(deviceCredentialServiceForProfile("cabin")); err != nil || !ok {
		t.Fatalf("replacement install credential was removed: ok=%v err=%v", ok, err)
	}
}
