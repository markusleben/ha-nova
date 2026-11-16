package main

import (
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"testing"
)

func stubGateWindowOff(t *testing.T, err error) *int {
	t.Helper()
	calls := 0
	saved := gateWindowOff
	gateWindowOff = func(runtimePaths) error { calls++; return err }
	t.Cleanup(func() { gateWindowOff = saved })
	return &calls
}

// A file key never confirms in a window (spec §1): the pairing says where
// the key is, from the install-wide backend decision.
func TestPairingCredentialStoreNamesTheBackend(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saved := deviceCredentialFileModeForced
	t.Cleanup(func() { deviceCredentialFileModeForced = saved })
	deviceCredentialFileModeForced = false
	if got, want := pairingCredentialStore(), map[string]string{"darwin": "keychain", "windows": "credential_manager", "linux": "secret_service"}[runtime.GOOS]; got != want {
		t.Fatalf("keyring: %q, want %q", got, want)
	}
	deviceCredentialFileModeForced = true
	if got := pairingCredentialStore(); got != "file" {
		t.Fatalf("file mode: %q", got)
	}
	meta, _ := json.Marshal(deviceMetadata{Name: "n", Platform: "p", Client: "c", ClientInstallID: "i"})
	if strings.Contains(string(meta), "credential_store") {
		t.Fatalf("an unknown store is left out: %s", meta)
	}
}

// confirm-window off takes this computer's window approval away on the App
// first, whatever the local window can do; a failure there changes nothing.
func TestConfirmWindowOffTurnsTheAppsWindowApprovalOffFirst(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	useWindow(t) // a failed App call opens no window
	calls := stubGateWindowOff(t, errors.New("down"))
	if exit := runConfirmWindowSetting(h.paths, []string{"off"}); exit != 1 || windowOff(h.paths) || *calls != 1 {
		t.Fatalf("App down: exit %d, off %v, %d calls", exit, windowOff(h.paths), *calls)
	}
	useWindow(t, windowConfirmed)
	calls = stubGateWindowOff(t, nil)
	if exit := runConfirmWindowSetting(h.paths, []string{"off"}); exit != 0 || !windowOff(h.paths) || *calls != 1 {
		t.Fatalf("off: exit %d, off %v, %d calls", exit, windowOff(h.paths), *calls)
	}
	if exit := runConfirmWindowSetting(h.paths, []string{"off"}); exit != 0 || *calls != 2 { // already off here: the App still hears it
		t.Fatalf("again: exit %d, %d calls", exit, *calls)
	}
	if exit := runConfirmWindowSetting(h.paths, []string{"on"}); exit != 0 || windowOff(h.paths) || *calls != 2 {
		t.Fatalf("on: exit %d, %d calls", exit, *calls)
	}
}
