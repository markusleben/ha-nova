package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const uninstallTestCredential = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"

func stubUninstallRevoke(t *testing.T, result error) *int {
	t.Helper()
	calls := 0
	original := revokeSelfDeviceV1ForUninstall
	revokeSelfDeviceV1ForUninstall = func(base, pin, credential string) error {
		calls++
		if base != "https://192.168.1.5:18792" || pin != "pin" || credential != uninstallTestCredential {
			t.Errorf("revoke called with %q %q %q", base, pin, credential)
		}
		return result
	}
	t.Cleanup(func() { revokeSelfDeviceV1ForUninstall = original })
	return &calls
}

func TestPurgeDeviceCredentialRevokesAndRemovesBothSlots(t *testing.T) {
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	if err := writeDeviceCredential(uninstallTestCredential); err != nil {
		t.Fatalf("writeDeviceCredential() error: %v", err)
	}
	if err := writePendingDeviceCredential(uninstallTestCredential); err != nil {
		t.Fatalf("writePendingDeviceCredential() error: %v", err)
	}
	calls := stubUninstallRevoke(t, nil)

	report := &uninstallReport{}
	purgeDeviceCredentialWithReport("https://192.168.1.5:18792", "pin", report, false)

	if *calls != 1 {
		t.Fatalf("revoke calls = %d, want 1", *calls)
	}
	if _, ok, _ := readDeviceCredential(); ok {
		t.Fatal("current device credential must be removed")
	}
	if _, ok, _ := readPendingDeviceCredential(); ok {
		t.Fatal("pending device credential must be removed")
	}
	output := strings.Join(report.notes, "\n")
	if !strings.Contains(output, "Revoked this device's pairing on the relay.") {
		t.Fatalf("expected revoke note, got: %q", output)
	}
}

func TestPurgeDeviceCredentialUnreachableRelayLeavesNovaHint(t *testing.T) {
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	if err := writeDeviceCredential(uninstallTestCredential); err != nil {
		t.Fatalf("writeDeviceCredential() error: %v", err)
	}
	stubUninstallRevoke(t, errors.New("connection refused"))

	report := &uninstallReport{}
	purgeDeviceCredentialWithReport("https://192.168.1.5:18792", "pin", report, false)

	if _, ok, _ := readDeviceCredential(); ok {
		t.Fatal("local cleanup must proceed even when the relay is unreachable")
	}
	output := strings.Join(report.notes, "\n")
	if !strings.Contains(output, "AAAAAAAAAAAAAAAAAAAAAA") || !strings.Contains(output, "NOVA page") {
		t.Fatalf("expected device id + NOVA page hint, got: %q", output)
	}
}

func TestPurgeDeviceCredentialWithoutPairingIsSilent(t *testing.T) {
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	calls := stubUninstallRevoke(t, nil)

	report := &uninstallReport{}
	purgeDeviceCredentialWithReport("https://192.168.1.5:18792", "pin", report, false)

	if *calls != 0 {
		t.Fatalf("revoke must not be called without a credential (calls=%d)", *calls)
	}
	if len(report.notes) != 0 || len(report.removed) != 0 {
		t.Fatalf("expected an empty report, got notes=%v removed=%v", report.notes, report.removed)
	}
}

// F4/S3 coverage: an unreadable (malformed) credential slot is removed with an
// honest note, and the NOVA hint respects a completed teardown.
func TestPurgeDeviceCredentialUnreadableSlotIsRemovedWithHonestNote(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "ha-nova.device-credential"), []byte("garbage-not-a-credential"), 0o600); err != nil {
		t.Fatalf("write malformed slot: %v", err)
	}
	calls := stubUninstallRevoke(t, nil)

	report := &uninstallReport{}
	purgeDeviceCredentialWithReport("https://192.168.1.5:18792", "pin", report, false)

	if *calls != 0 {
		t.Fatalf("revoke must not run for an unreadable credential (calls=%d)", *calls)
	}
	if _, err := os.Stat(filepath.Join(dir, "ha-nova.device-credential")); !os.IsNotExist(err) {
		t.Fatalf("expected the malformed slot to be deleted, stat err=%v", err)
	}
	notes := strings.Join(report.notes, "\n")
	if !strings.Contains(notes, "unreadable and was removed without revoking") || !strings.Contains(notes, "NOVA page") {
		t.Fatalf("expected honest removal note with NOVA hint, got: %q", notes)
	}

	// Same, after a completed teardown: no NOVA hint (the App is gone).
	if err := os.WriteFile(filepath.Join(dir, "ha-nova.device-credential"), []byte("garbage-again"), 0o600); err != nil {
		t.Fatalf("write malformed slot: %v", err)
	}
	report = &uninstallReport{}
	purgeDeviceCredentialWithReport("https://192.168.1.5:18792", "pin", report, true)
	notes = strings.Join(report.notes, "\n")
	if strings.Contains(notes, "NOVA page") {
		t.Fatalf("completed teardown must suppress the NOVA hint, got: %q", notes)
	}
}

// F4/S2 coverage: the revoke is attempted even after a completed teardown (the
// App may not actually be gone), while the NOVA hint stays suppressed.
func TestPurgeDeviceCredentialAfterTeardownStillAttemptsRevoke(t *testing.T) {
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	if err := writeDeviceCredential(uninstallTestCredential); err != nil {
		t.Fatalf("writeDeviceCredential() error: %v", err)
	}
	calls := stubUninstallRevoke(t, errors.New("connection refused"))

	report := &uninstallReport{}
	purgeDeviceCredentialWithReport("https://192.168.1.5:18792", "pin", report, true)

	if *calls != 1 {
		t.Fatalf("revoke attempts = %d, want 1 even after a completed teardown", *calls)
	}
	if _, ok, _ := readDeviceCredential(); ok {
		t.Fatal("local credential must be removed")
	}
	notes := strings.Join(report.notes, "\n")
	if strings.Contains(notes, "NOVA page") {
		t.Fatalf("completed teardown must suppress the NOVA hint, got: %q", notes)
	}
}
