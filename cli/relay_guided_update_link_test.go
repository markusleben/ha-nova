package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The guided NOVA App update over Home Assistant Link (MAR-124), on the real
// Link client stack (CloudIngressClient + cloudRelayRoundTripper) against a
// stand-in for what Link reaches: the Supervisor's Ingress proxy in front of
// the Relay. VMs have no Link; what only a real Link setup shows is the
// Cloud checklist's app_update_over_link row (docs/releasing.md).

const linkLaneIngressRoot = "/api/hassio_ingress/abcdefghijklmnopqrstuvwxyzABCDEF"

// linkLaneIngress plays Supervisor Ingress: a session cookie it knows (else
// 401 without the Relay's header), the App (else 502, as while its container
// restarts), then the Relay, which answers with its version header.
type linkLaneIngress struct {
	t      *testing.T
	store  *staleStoreServer
	origin *httptest.Server

	mu              sync.Mutex
	sessions        map[string]bool
	current         string // the session a new Link route gets
	appDown         bool
	restartPolls    int // 502s while the App restarts, before the session is lost
	loseSession     bool
	comesBack       bool // a route opened while the App is down brings it back
	notReadyOpens   int
	oldAfterNew     int // requests on a lost session after a new one was issued
	issued          []string
	installResponse int
}

func newLinkLaneIngress(t *testing.T, store *staleStoreServer) *linkLaneIngress {
	t.Helper()
	first := strings.Repeat("a", 128)
	lane := &linkLaneIngress{t: t, store: store, sessions: map[string]bool{first: true}, current: first, issued: []string{first}}
	lane.origin = httptest.NewTLSServer(http.HandlerFunc(lane.serve))
	t.Cleanup(lane.origin.Close)
	return lane
}

func (lane *linkLaneIngress) serve(w http.ResponseWriter, r *http.Request) {
	endpoint, ok := strings.CutPrefix(r.URL.Path, linkLaneIngressRoot+"/")
	cookie, _ := r.Cookie("ingress_session")
	lane.mu.Lock()
	session := ""
	if cookie != nil {
		session = cookie.Value
	}
	if len(lane.issued) > 1 && session != lane.issued[len(lane.issued)-1] {
		lane.oldAfterNew++
	}
	switch {
	case !ok:
		lane.mu.Unlock()
		http.NotFound(w, r)
		return
	case !lane.sessions[session]:
		lane.mu.Unlock()
		w.WriteHeader(http.StatusUnauthorized)
		return
	case lane.appDown && (lane.restartPolls > 0 || lane.loseSession):
		if lane.restartPolls > 0 {
			lane.restartPolls--
		} else {
			delete(lane.sessions, session) // e.g. the Supervisor restarted before saving it
		}
		lane.mu.Unlock()
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	lane.appDown = false // its restart ends with its 502s
	lane.mu.Unlock()

	var payload map[string]any
	_ = json.NewDecoder(r.Body).Decode(&payload)
	relay := r.Clone(r.Context())
	relay.URL.Path = "/" + endpoint
	install := endpoint == "core" && payload["path"] == "/api/services/update/install"
	if install && lane.installResponse != 0 {
		w.Header().Set(relayVersionHeader, "0.2.6")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": coreReply(lane.installResponse, []any{})})
		return
	}
	if install { // the install lands, and the App dies before it answers
		serveFakeHA(httptest.NewRecorder(), relay, lane.store.ha, payload)
		lane.mu.Lock()
		lane.appDown = true
		lane.mu.Unlock()
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	version := "0.2.6"
	if lane.store.installed.Load() {
		version = "0.4.0"
	}
	w.Header().Set(relayVersionHeader, version)
	serveFakeHA(w, relay, lane.store.ha, payload)
}

// open is the Link route resolver: Home Assistant signs in and makes an
// Ingress session only for a started App (CloudErrAppNotReady otherwise).
func (lane *linkLaneIngress) open(context.Context, runtimeConfig) (relayTransportSelection, error) {
	lane.mu.Lock()
	if lane.appDown && !lane.sessions[lane.current] {
		lane.notReadyOpens++
		lane.appDown = !lane.comesBack // up by the next try
		lane.mu.Unlock()
		return relayTransportSelection{}, newCloudError(CloudErrAppNotReady, "validate HA NOVA App ingress", nil)
	}
	if !lane.sessions[lane.current] {
		next := strings.Repeat("b", 128)
		lane.sessions[next], lane.current = true, next
		lane.issued = append(lane.issued, next)
	}
	session := lane.current
	lane.mu.Unlock()
	origin, _ := url.Parse(lane.origin.URL)
	ingress, err := newCloudIngressClientForOrigin(origin, linkLaneIngressRoot, session, lane.origin.Client())
	if err != nil {
		lane.t.Fatalf("Link ingress client: %v", err)
	}
	selected, err := newCloudRelayTransport(ingress, validCredential(31))
	if err != nil {
		lane.t.Fatalf("Link Relay transport: %v", err)
	}
	return selected, nil
}

// linkOnlyAwayFromHome: the maintainer's shape, route automatic with the
// home connection unreachable from where the computer is.
func linkOnlyAwayFromHome(t *testing.T) runtimeConfig {
	t.Helper()
	rememberLinkRouteInTemp(t)
	oldLocal := resolveLocalRelayTransportForCLI
	t.Cleanup(func() { resolveLocalRelayTransportForCLI = oldLocal })
	resolveLocalRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
		selected := cloudRuntimeSelection(cloudRuntimeRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, syscall.EHOSTUNREACH
		}), relayViaLocal)
		selected.DeviceMode = true
		return selected, nil
	}
	cfg := automaticLinkTestConfig()
	cfg.RoutePolicy = routePolicyAutomatic
	cfg.RelaySpkiPin = strings.Repeat("p", 43)
	return cfg
}

// linkOnlySetUpAway: `cloud add` away from home saves no home address at all.
func linkOnlySetUpAway(*testing.T) runtimeConfig {
	return runtimeConfig{ProfileID: "profile-1", RelayInstanceID: "relay-1", RoutePolicy: routePolicyCloud, Cloud: readyCloudForTransportTest()}
}

// runLinkLane runs the user's path (`ha-nova update`'s App half in a
// terminal, answering yes) on a home reached only over Link.
func runLinkLane(t *testing.T, shape func(*testing.T) runtimeConfig, setup func(*linkLaneIngress)) (*linkLaneIngress, string) {
	t.Helper()
	useDefaultServer(t)
	paths := guidedUpdatePaths(t)
	shrinkGuidedUpdatePolling(t, 2*time.Second)
	cfg := shape(t)
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	store, _ := newStaleStoreServer(t, true, 200)
	store.checks.Store(1) // Home Assistant already shows the update: no store check
	lane := newLinkLaneIngress(t, store)
	setup(lane)

	oldCloud := resolveCloudRelayTransportForCLI
	t.Cleanup(func() { resolveCloudRelayTransportForCLI = oldCloud })
	resolveCloudRelayTransportForCLI = lane.open
	forceGuidedUpdateTTY(t, "\n")
	var out bytes.Buffer
	oldInput, oldOutput := relayUpdateInput, relayUpdateOutput
	t.Cleanup(func() { relayUpdateInput, relayUpdateOutput = oldInput, oldOutput })
	relayUpdateInput = func() *bufio.Reader { return bufio.NewReader(strings.NewReader("\n")) }
	relayUpdateOutput = func() io.Writer { return &out }

	offerRelayUpdate(paths, relayUpdateNotice(paths))
	return lane, out.String()
}

// useDefaultServer: the default server, whatever an earlier test selected.
func useDefaultServer(t *testing.T) {
	t.Helper()
	old := serverSelectionOverride
	serverSelectionOverride = ""
	t.Cleanup(func() { serverSelectionOverride = old })
	t.Setenv(serverSelectionEnvVar, "")
}

func TestGuidedAppUpdateOverLinkSurvivesTheRestartAndALostSession(t *testing.T) {
	lane, out := runLinkLane(t, linkOnlyAwayFromHome, func(lane *linkLaneIngress) {
		lane.restartPolls, lane.loseSession, lane.comesBack = 2, true, true
	})
	if lane.store.installs.Load() != 1 {
		t.Fatalf("installs = %d, want exactly one", lane.store.installs.Load())
	}
	if !strings.Contains(out, "Update the NOVA App now?") || !strings.Contains(out, "The NOVA App is updated and works: v0.4.0.") {
		t.Fatalf("output = %q", out)
	}
	if lane.notReadyOpens != 1 || len(lane.issued) != 2 {
		t.Fatalf("route opens while the App was down = %d, sessions issued = %d", lane.notReadyOpens, len(lane.issued))
	}
	if lane.oldAfterNew != 0 {
		t.Fatalf("%d requests still used the lost Ingress session", lane.oldAfterNew)
	}
}

// The expected restart: 502s while the App is down, on a session that stays.
func TestGuidedAppUpdateOverLinkKeepsItsSessionThroughTheRestart(t *testing.T) {
	lane, out := runLinkLane(t, linkOnlySetUpAway, func(lane *linkLaneIngress) { lane.restartPolls = 3 })
	if lane.store.installs.Load() != 1 || lane.notReadyOpens != 0 || len(lane.issued) != 1 ||
		!strings.Contains(out, "The NOVA App is updated and works: v0.4.0.") {
		t.Fatalf("installs = %d, opens = %d, sessions = %d, output = %q", lane.store.installs.Load(), lane.notReadyOpens, len(lane.issued), out)
	}
}

func TestGuidedAppUpdateOverLinkWithoutTheAppComingBackStaysHonest(t *testing.T) {
	lane, out := runLinkLane(t, linkOnlyAwayFromHome, func(lane *linkLaneIngress) {
		lane.restartPolls, lane.loseSession = 2, true
	})
	if lane.store.installs.Load() != 1 || lane.notReadyOpens == 0 {
		t.Fatalf("installs = %d, route opens = %d", lane.store.installs.Load(), lane.notReadyOpens)
	}
	if strings.Contains(out, "updated and works") ||
		!strings.Contains(out, "The NOVA App didn't show the new version within") ||
		!strings.Contains(out, relayUpdateManualPath) {
		t.Fatalf("output = %q", out)
	}
}

func TestGuidedAppUpdateOverLinkRejectedInstallSkipsTheWait(t *testing.T) {
	lane, out := runLinkLane(t, linkOnlyAwayFromHome, func(lane *linkLaneIngress) { lane.installResponse = http.StatusBadRequest })
	if lane.store.installs.Load() != 0 || lane.notReadyOpens != 0 ||
		!strings.Contains(out, "Home Assistant didn't start the update.") ||
		strings.Contains(out, "updated and works") {
		t.Fatalf("installs = %d, opens = %d, output = %q", lane.store.installs.Load(), lane.notReadyOpens, out)
	}
}
