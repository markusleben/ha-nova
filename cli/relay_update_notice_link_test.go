package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
)

// A home reached only over Home Assistant Link: `ha-nova update` says the
// NOVA App needs its update (the home connection is unreachable) and, in a
// terminal, offers the guided install over Link too (MAR-124); "Not now"
// installs nothing.
func TestUpdateOffersTheAppUpdateOverLink(t *testing.T) {
	useDefaultServer(t)
	paths := guidedUpdatePaths(t)
	rememberLinkRouteInTemp(t)
	cfg := automaticLinkTestConfig()
	cfg.RoutePolicy = routePolicyAutomatic
	cfg.RelaySpkiPin = strings.Repeat("p", 43)
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	pending := pendingRelayUpdateEntity("update.nova_relay_update", "0.3.0", "0.4.0")
	var installs atomic.Int32
	app := httptest.NewServer(withTierReads(appUpdateHA(), func(w http.ResponseWriter, r *http.Request) { // the check before the question reads the rest of the home
		if r.URL.Path == "/core" && r.Method == http.MethodPost {
			if body, _ := io.ReadAll(r.Body); strings.Contains(string(body), "update/install") {
				installs.Add(1)
			}
		}
		switch r.URL.Path {
		case "/health":
			fmt.Fprint(w, `{"ok":true,"data":{"status":"ok","version":"0.3.0"}}`)
		case "/core":
			w.Write(statesEnvelope(t, []map[string]interface{}{pending}))
		case "/ws":
			w.Write(registryEnvelope(t, []map[string]interface{}{novaRegistryEntry("update.nova_relay_update")}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer app.Close()

	oldLocal, oldCloud := resolveLocalRelayTransportForCLI, resolveCloudRelayTransportForCLI
	oldIn, oldOut, oldInput, oldOutput := relayUpdateInputIsTTY, relayUpdateOutputIsTTY, relayUpdateInput, relayUpdateOutput
	t.Cleanup(func() {
		resolveLocalRelayTransportForCLI, resolveCloudRelayTransportForCLI = oldLocal, oldCloud
		relayUpdateInputIsTTY, relayUpdateOutputIsTTY, relayUpdateInput, relayUpdateOutput = oldIn, oldOut, oldInput, oldOutput
	})
	resolveLocalRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
		selected := cloudRuntimeSelection(cloudRuntimeRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, syscall.EHOSTUNREACH
		}), relayViaLocal)
		selected.DeviceMode = true
		return selected, nil
	}
	resolveCloudRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
		return relayTransportSelection{BaseURL: app.URL, Client: app.Client(), Credential: validCredential(31), DeviceMode: true, Via: relayViaCloud}, nil
	}

	notice := relayUpdateNotice(paths)
	if notice.kind != humanNoticeKindRelayUpdateAvailable || !strings.Contains(notice.message, relayAppUpdatePath) {
		t.Fatalf("notice over Link = %+v", notice)
	}

	relayUpdateInputIsTTY = func() bool { return true }
	relayUpdateOutputIsTTY = func() bool { return true }
	var asked bytes.Buffer
	relayUpdateInput = func() *bufio.Reader { return bufio.NewReader(strings.NewReader("n\n")) }
	relayUpdateOutput = func() io.Writer { return &asked }
	offerRelayUpdate(paths, notice)
	if !strings.Contains(asked.String(), "Update the NOVA App now?") || installs.Load() != 0 {
		t.Fatalf("over Link: offer = %q, installs = %d", asked.String(), installs.Load())
	}

	updateNotice, appNotice := withAppUpdateToo(humanNotice{kind: humanNoticeKindUpdateAvailable, message: "HA NOVA 0.30.0 is out.\nTo update, run: ha-nova update"}, notice)
	if !appNotice.empty() || !strings.Contains(updateNotice.message, appUpdateTooLine) {
		t.Fatalf("check-update over Link didn't fold the App update into `ha-nova update`: %q, %+v", updateNotice.message, appNotice)
	}
}

// A Link install never writes with a legacy token: not when Link fails before
// the offer, and not as the route a lost Ingress session is replaced with.
func TestGuidedAppUpdateOverLinkNeverFallsBackToTheLegacyToken(t *testing.T) {
	useDefaultServer(t)
	paths := guidedUpdatePaths(t)
	var requests atomic.Int32
	legacy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.NotFound(w, r)
	}))
	defer legacy.Close()
	cfg := linkOnlySetUpAway(t)
	cfg.RelayBaseURL = legacy.URL
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	oldCloud, oldRead := resolveCloudRelayTransportForCLI, readRelayAuthTokenForDoctor
	t.Cleanup(func() { resolveCloudRelayTransportForCLI, readRelayAuthTokenForDoctor = oldCloud, oldRead })
	resolveCloudRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
		return relayTransportSelection{}, newCloudError(CloudErrAppNotReady, "validate HA NOVA App ingress", nil)
	}
	readRelayAuthTokenForDoctor = func() (string, error) { return "legacy-token", nil }
	forceGuidedUpdateTTY(t, "\n")

	if maybeOfferGuidedRelayUpdate(paths, humanNotice{kind: humanNoticeKindRelayUpdateAvailable, appUpdate: true}) || requests.Load() != 0 {
		t.Fatalf("the guided update ran with the legacy token: %d requests", requests.Load())
	}
	route := &relayUpdateRoute{relayTransportSelection{BaseURL: "https://relay.invalid", Credential: validCredential(31), DeviceMode: true, Via: relayViaCloud}, cfg}
	route.reopenAfter(newCloudHTTPError(CloudErrOuterSessionExpired, "authorize Cloud ingress session", http.StatusUnauthorized, true))
	if route.Via != relayViaCloud || route.BaseURL != "https://relay.invalid" {
		t.Fatalf("a lost Link session was replaced with %+v", route.relayTransportSelection)
	}
}
