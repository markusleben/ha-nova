package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// linkDoctorFixture: a paired install on the automatic route, a fake NOVA App
// that answers as relayID, and the given saved Link. Link itself is never
// contacted: the App answers at home.
func linkDoctorFixture(t *testing.T, relayID string, cloud *cloudLifecycleMetadata, wsDown ...bool) (runtimePaths, *[]string) {
	t.Helper()
	paths, _ := doctorTestSetup(t)
	t.Cleanup(setCloudFeatureTestBuild(t, true))
	withClientRuntimeAvailability(t, map[string]bool{"claude": true})
	withClientAttachmentPresence(t, map[string]bool{"claude": true})
	const credential = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	var asked []string
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer "+credential {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"data":{"relay_instance_id":"` + relayID + `","ha_ws_connected":` + strconv.FormatBool(len(wsDown) == 0) + `}}`))
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
	return paths, &asked
}

// An unfinished Link on a working install: doctor checks everything, passes,
// and says what Link is waiting for, with both ways out.
func TestDoctorLinkUnfinishedStillChecksAndShowsBothCommands(t *testing.T) {
	current := cloudMetadataForTest(strings.Repeat("b", 32))
	pending := cloudMetadataForTest(strings.Repeat("a", 32))
	paths, _ := linkDoctorFixture(t, "relay-instance-1", &cloudLifecycleMetadata{State: cloudStateAuthorizing, Current: &current, Pending: &pending})

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
	paths, asked := linkDoctorFixture(t, "relay-instance-2", &cloudLifecycleMetadata{State: cloudStateReady, Current: &current})

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
	for _, request := range *asked { // diagnostic only: no update offer, no state read
		if request != "GET /health" {
			t.Fatalf("doctor sent %q to an App that failed the safety check", request)
		}
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

// Over Link, a reinstalled App gets the same plain words, not "unsupported".
func TestDoctorLinkMismatchOverLinkSaysReinstalled(t *testing.T) {
	problem := cloudProblemForError(newCloudError(CloudErrHAProtocol, "verify Cloud device", errRelayHealthIdentityMismatch))
	if problem.Code != cloudProblemIdentityMismatch || !strings.Contains(problem.humanText(), "(was it reinstalled?)") {
		t.Fatalf("problem = %+v", problem)
	}
	if plain := cloudProblemForError(newCloudError(CloudErrHAProtocol, "verify Cloud device", nil)); plain.Code != cloudProblemApp {
		t.Fatalf("plain protocol error = %+v", plain)
	}
}

// Only a present, different Relay id is a changed identity; a reply without
// one stays an unsupported reply.
func TestDoctorLinkHealthWithoutRelayIDIsUnsupported(t *testing.T) {
	for _, body := range []string{`{"ok":true,"data":{}}`, `{"ok":true,"data":null}`, `{"ok":true,"data":{"relay_instance_id":""}}`} {
		err := validateCloudRelayHealthIdentity([]byte(body), "relay-1")
		if !IsCloudErrorCode(err, CloudErrHAProtocol) || errors.Is(err, errRelayHealthIdentityMismatch) {
			t.Fatalf("%s: %v", body, err)
		}
	}
	if err := validateCloudRelayHealthIdentity([]byte(`{"ok":true,"data":{"relay_instance_id":"relay-2"}}`), "relay-1"); !errors.Is(err, errRelayHealthIdentityMismatch) {
		t.Fatalf("different id: %v", err)
	}
}

// A removal that didn't finish says so, with the command that finishes it.
func TestDoctorLinkRemovalUnfinishedSaysFinishRemoving(t *testing.T) {
	current := cloudMetadataForTest(strings.Repeat("b", 32))
	paths, _ := linkDoctorFixture(t, "relay-instance-1", &cloudLifecycleMetadata{State: cloudStateReady, Current: &current,
		DeviceRevocationCompleted: &cloudDeviceRevocationCheckpoint{CurrentDeviceID: "AAAAAAAAAAAAAAAAAAAAAA"}})
	code, out := captureCommandOutput(t, func() int { return runDoctor(paths, nil) })
	if !strings.Contains(out, "Removing Home Assistant Link didn't finish. To finish removing: ha-nova cloud remove --server default.") ||
		strings.Contains(out, "isn't finished") || strings.Contains(out, "To finish:") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
}

// On the Link route, a problem that already names the Link commands isn't
// followed by the same commands again.
func TestDoctorLinkRouteProblemDoesntRepeatCommands(t *testing.T) {
	current := cloudMetadataForTest(strings.Repeat("b", 32))
	pending := cloudMetadataForTest(strings.Repeat("a", 32))
	paths, asked := linkDoctorFixture(t, "relay-instance-1", &cloudLifecycleMetadata{State: cloudStateAuthorizing, Current: &current, Pending: &pending})
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RoutePolicy = routePolicyCloud
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	previous := resolveCloudRelayTransportForCLI // never the real Link
	resolveCloudRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
		return relayTransportSelection{}, &cloudProblem{Code: cloudProblemAuthorization, Remediation: cloudRemediationSignIn,
			Detail: "sign in again.\nTo finish it, run: ha-nova cloud reconnect --server default\nTo remove it, run: ha-nova cloud remove --server default"}
	}
	t.Cleanup(func() { resolveCloudRelayTransportForCLI = previous })
	code, out := captureCommandOutput(t, func() int { return runDoctor(paths, nil) })
	for _, request := range *asked { // diagnostic only after a failed route too
		if request != "GET /health" {
			t.Fatalf("doctor sent %q after the Link route failed", request)
		}
	}
	if code != 1 || !strings.Contains(out, "Home Assistant Link isn't finished on this computer.") ||
		strings.Contains(out, "To finish:") || strings.Count(out, "ha-nova cloud remove --server default") != 1 ||
		!strings.Contains(out, "The NOVA App answers at") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
}

// After a failed safety check, an App that says Home Assistant is down gets
// no WebSocket check: doctor reads /health and nothing else.
func TestDoctorLinkMismatchReadsHealthOnly(t *testing.T) {
	current := cloudMetadataForTest(strings.Repeat("b", 32))
	paths, asked := linkDoctorFixture(t, "relay-instance-2", &cloudLifecycleMetadata{State: cloudStateReady, Current: &current}, true)
	code, out := captureCommandOutput(t, func() int { return runDoctor(paths, nil) })
	if code != 1 || !strings.Contains(out, "The NOVA App answers at") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, request := range *asked {
		if request != "GET /health" {
			t.Fatalf("doctor sent %q to an App that failed the safety check:\n%s", request, out)
		}
	}
}
