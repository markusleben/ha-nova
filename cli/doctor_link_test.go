package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// linkDoctorFixture: a paired install on the automatic route, a fake NOVA App
// that answers as relayID, and the given saved Link. Link itself is never
// contacted: the App answers at home.
func linkDoctorFixture(t *testing.T, relayID string, cloud *cloudLifecycleMetadata) runtimePaths {
	t.Helper()
	paths, _ := doctorTestSetup(t)
	t.Cleanup(setCloudFeatureTestBuild(t, true))
	withClientRuntimeAvailability(t, map[string]bool{"claude": true})
	withClientAttachmentPresence(t, map[string]bool{"claude": true})
	const credential = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+credential {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"data":{"relay_instance_id":"` + relayID + `","ha_ws_connected":true}}`))
	}))
	t.Cleanup(app.Close)
	stubDoctorTransport(t, app.URL, credential)
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RelaySecureBaseURL, cfg.RelaySpkiPin = app.URL, "PIN"
	cfg.RelayInstanceID, cfg.RoutePolicy, cfg.Cloud = "relay-instance-1", routePolicyAutomatic, cloud
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	state := loadStateOrDefault(paths)
	state.InstalledClients = []string{"claude"}
	if err := saveState(paths, state); err != nil {
		t.Fatal(err)
	}
	return paths
}

// An unfinished Link on a working install: doctor checks everything, passes,
// and says what Link is waiting for, with both ways out.
func TestDoctorLinkUnfinishedStillChecksAndShowsBothCommands(t *testing.T) {
	current := cloudMetadataForTest(strings.Repeat("b", 32))
	pending := cloudMetadataForTest(strings.Repeat("a", 32))
	paths := linkDoctorFixture(t, "relay-instance-1", &cloudLifecycleMetadata{State: cloudStateAuthorizing, Current: &current, Pending: &pending})

	code, out := captureCommandOutput(t, func() int { return runDoctor(paths, nil) })
	for _, want := range []string{
		"Home Assistant Link isn't finished on this computer. HA NOVA still works at home without it.",
		"To finish: ha-nova cloud reconnect --server default (you sign in to Home Assistant once)",
		"To remove: ha-nova cloud remove --server default",
		"The NOVA App answers at",
		"Claude Code is ready",
		"All checks passed",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q (exit %d):\n%s", want, code, out)
		}
	}
	if code != 0 || strings.Contains(out, "ha-nova doctor") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
}

// A reinstalled App answers with another Relay id: doctor names it, gives the
// remove command, and still checks the App and the AI apps at home.
func TestDoctorLinkIdentityMismatchSaysReinstalledAndKeepsChecking(t *testing.T) {
	current := cloudMetadataForTest(strings.Repeat("b", 32))
	paths := linkDoctorFixture(t, "relay-instance-2", &cloudLifecycleMetadata{State: cloudStateReady, Current: &current})

	code, out := captureCommandOutput(t, func() int { return runDoctor(paths, nil) })
	for _, want := range []string{
		"The NOVA App isn't the one this computer's Link was set up with (was it reinstalled?). Remove Link: ha-nova cloud remove --server default. Then connect this computer again: ha-nova setup.",
		"The NOVA App answers at",
		"Claude Code is ready",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q (exit %d):\n%s", want, code, out)
		}
	}
	if code != 1 || strings.Contains(out, "ha-nova doctor") || strings.Contains(out, "unsupported health response") ||
		strings.Contains(out, "isn't finished") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
}

// Inside doctor, a key-read problem never says "run: ha-nova doctor";
// everywhere else it still does.
func TestDoctorLinkKeyReadTextNeverPointsAtDoctor(t *testing.T) {
	for _, cause := range []error{
		newCloudError(CloudErrSecretStore, "read device credential", nil),
		newCloudError(CloudErrSecretCorrupt, "read device credential", nil),
	} {
		err := &localRelayCredentialError{cause: cause}
		if !strings.Contains(err.Error(), "run: ha-nova doctor") {
			t.Fatalf("Error() = %q", err.Error())
		}
		if text := doctorTransportErrorMessage(err); strings.Contains(text, "ha-nova doctor") ||
			!strings.HasPrefix(err.Error(), text) {
			t.Fatalf("doctor text = %q, Error() = %q", text, err.Error())
		}
	}
	if !errors.Is(validateCloudRelayHealthIdentity([]byte(`{"ok":true,"data":{"relay_instance_id":"b"}}`), "a"), errRelayHealthIdentityMismatch) ||
		errors.Is(validateCloudRelayHealthIdentity([]byte(`{"ok":true,"data":[]}`), "a"), errRelayHealthIdentityMismatch) {
		t.Fatal("only a readable, different Relay id is a mismatch")
	}
}
