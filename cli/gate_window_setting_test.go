package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
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

// A pending key (a pairing not yet promoted here) goes only to the endpoint
// saved with it, never to the live one, which may be another Relay; without
// one, nothing is sent.
func TestGateWindowOffSendsAPendingKeyOnlyToItsOwnEndpoint(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetServerProfileSelection(t)
	var live atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { live.Add(1) }))
	t.Cleanup(other.Close)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	for _, pending := range []string{"", "https://127.0.0.1:1"} {
		cfg := runtimeConfig{HAHost: "ha", HAURL: "http://ha:8123", RelayBaseURL: "http://ha:8791", RelaySecureBaseURL: other.URL, RelaySpkiPin: "live-pin", PendingSecureBaseURL: pending, RoutePolicy: routePolicyLocal}
		if pending != "" {
			cfg.PendingSpkiPin = "pending-pin"
		}
		if err := saveConfig(paths, cfg); err != nil {
			t.Fatal(err)
		}
		err := gateWindowOffForProfile(paths, defaultServerProfileName, "pending-key", false)
		if err == nil || live.Load() != 0 || (pending == "") != strings.Contains(err.Error(), "didn't finish") {
			t.Fatalf("pending endpoint %q: %v; live endpoint reached %d times", pending, err, live.Load())
		}
	}
	// The current key while a re-pairing stopped with the new Relay saved as
	// live: it may be the old Relay's key, so it isn't sent.
	cfg := runtimeConfig{HAHost: "ha", HAURL: "http://ha:8123", RelayBaseURL: "http://ha:8791", RelaySecureBaseURL: other.URL, RelaySpkiPin: "pin", PendingSecureBaseURL: other.URL, PendingSpkiPin: "pin", RoutePolicy: routePolicyLocal}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	if err := gateWindowOffForProfile(paths, defaultServerProfileName, "", true); err == nil || !strings.Contains(err.Error(), "didn't finish") || live.Load() != 0 {
		t.Fatalf("current key mid re-pairing: %v; endpoint reached %d times", err, live.Load())
	}
	// The same endpoint fields left after a pairing that finished (no pending
	// key): not an unfinished pairing.
	if err := gateWindowOffForProfile(paths, defaultServerProfileName, "", false); err != nil && strings.Contains(err.Error(), "didn't finish") {
		t.Fatalf("stale endpoint fields read as an unfinished pairing: %v", err)
	}
}
