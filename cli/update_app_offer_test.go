package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppOfferAfterInstallRunsInTheNewBinary(t *testing.T) {
	oldRun, oldNotice := runNewBinaryAppOffer, relayUpdateNoticeForUpdate
	t.Cleanup(func() { runNewBinaryAppOffer, relayUpdateNoticeForUpdate = oldRun, oldNotice })
	var exitErr error = &exec.ExitError{}
	cases := []struct {
		name, target  string
		newBinary     error
		wantNewBinary bool
		wantInProcess bool
	}{
		{name: "new version offers itself", target: "0.28.3", wantNewBinary: true},
		{name: "release candidate offers itself", target: "0.28.3-rc1", wantNewBinary: true},
		{name: "new binary ran but failed: no second offer", target: "0.29.0", newBinary: exitErr, wantNewBinary: true},
		{name: "new binary didn't start: this one offers", target: "0.29.0", newBinary: errors.New("exec: not found"), wantNewBinary: true, wantInProcess: true},
		{name: "older target lacks the command", target: "0.28.2", wantInProcess: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ranNew, ranHere bool
			runNewBinaryAppOffer = func(runtimePaths) error { ranNew = true; return tc.newBinary }
			relayUpdateNoticeForUpdate = func(context.Context, runtimePaths) humanNotice { ranHere = true; return humanNotice{} }
			offerRelayUpdateAfterInstall(runtimePaths{}, tc.target)
			if ranNew != tc.wantNewBinary || ranHere != tc.wantInProcess {
				t.Fatalf("new binary %v, in-process %v; want %v, %v", ranNew, ranHere, tc.wantNewBinary, tc.wantInProcess)
			}
		})
	}
}

func TestInternalOfferAppUpdateCommandOffersAndExitsZero(t *testing.T) {
	oldNotice := relayUpdateNoticeForUpdate
	t.Cleanup(func() { relayUpdateNoticeForUpdate = oldNotice })
	asked := false
	relayUpdateNoticeForUpdate = func(context.Context, runtimePaths) humanNotice { asked = true; return humanNotice{} }
	paths := guidedUpdatePaths(t)
	t.Cleanup(func() { setServerSelectionOverride("") })
	if code := dispatch(paths, "ha-nova", []string{"internal-offer-app-update", "--server", "default"}); code != 0 || !asked || serverSelectionOverride != "default" {
		t.Fatalf("exit %d, offer asked %v, server flag %q", code, asked, serverSelectionOverride)
	}
}

// The App check after an update is bounded (#1156): a Relay that never
// answers no longer keeps `ha-nova update` silent for 40 s. A terminal sees
// a progress line, and a cut check says so and names doctor; it offers
// nothing.
func TestUpdateAppCheckIsBoundedAndSaysWhenItGivesUp(t *testing.T) {
	resetServerProfileSelection(t) // the doctor command it names follows a --server another test left set
	paths := guidedUpdatePaths(t)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(t.TempDir(), "relay-token"))
	gone := make(chan struct{})
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select { // a route that never answers
		case <-r.Context().Done():
		case <-gone:
		}
	}))
	t.Cleanup(relay.Close)
	t.Cleanup(func() { close(gone) }) // runs first: a kept-alive request never holds Close
	if err := saveConfig(paths, runtimeConfig{HAHost: "127.0.0.1", HAURL: "http://127.0.0.1:8123", RelayBaseURL: relay.URL}); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}
	oldWait, oldTTY := relayUpdateCheckWait, relayUpdateOutputIsTTY
	t.Cleanup(func() { relayUpdateCheckWait, relayUpdateOutputIsTTY = oldWait, oldTTY })
	relayUpdateCheckWait = 100 * time.Millisecond
	relayUpdateOutputIsTTY = func() bool { return true }

	started := time.Now()
	_, output := captureCommandOutput(t, func() int { offerRelayUpdateAfterUpdate(paths); return 0 })
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("the App check took %s", elapsed)
	}
	if !strings.Contains(output, "Checking the NOVA App in Home Assistant...") ||
		!strings.Contains(output, "HA NOVA couldn't check the NOVA App in time. To check it, run `ha-nova doctor`.") ||
		strings.Contains(output, "Update the NOVA App now?") {
		t.Fatalf("output:\n%s", output)
	}
}

// A check the deadline cuts after the Relay's version came back keeps the
// version warning but makes no claim about how the App was installed (no
// own-container line), and still ends within the bound.
func TestUpdateAppCheckCutAfterTheVersionKeepsOnlyTheWarning(t *testing.T) {
	paths := guidedUpdatePaths(t)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(t.TempDir(), "relay-token"))
	gone := make(chan struct{})
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"ok":true,"data":{"status":"ok","version":"0.2.6"}}`)) // below the floor (0.4.0)
			return
		}
		select { // Home Assistant's states never come
		case <-r.Context().Done():
		case <-gone:
		}
	}))
	t.Cleanup(relay.Close)
	t.Cleanup(func() { close(gone) }) // runs first: a kept-alive request never holds Close
	if err := saveConfig(paths, runtimeConfig{HAHost: "127.0.0.1", HAURL: "http://127.0.0.1:8123", RelayBaseURL: relay.URL}); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}
	oldWait := relayUpdateCheckWait
	t.Cleanup(func() { relayUpdateCheckWait = oldWait })
	relayUpdateCheckWait = 200 * time.Millisecond

	started := time.Now()
	notice, ok := checkRelayAfterUpdate(paths, false)
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("the App check took %s", elapsed)
	}
	if !ok || notice.kind != humanNoticeKindRelayOutdated || !strings.Contains(notice.message, relayAppUpdatePath) || strings.Contains(notice.message, relayContainerUpdateLine) {
		t.Fatalf("ok=%t notice=%+v", ok, notice)
	}
}
