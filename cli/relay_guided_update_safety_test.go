package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGuidedRelayUpdateDeclinedTouchesNothing(t *testing.T) {
	paths := guidedUpdatePaths(t)
	var installCalled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			w.Write(registryEnvelope(t, []map[string]interface{}{novaRegistryEntry("update.nova_relay_update")}))
			return
		}
		var req struct {
			Path string `json:"path"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Path == "/api/states" {
			w.Write(statesEnvelope(t, []map[string]interface{}{
				relayUpdateEntity("update.nova_relay_update", "NOVA Relay"),
			}))
			return
		}
		installCalled.Store(true)
		t.Errorf("declined preview must not install, got %s %s", r.Method, req.Path)
	}))
	defer server.Close()

	var out bytes.Buffer
	if runGuidedRelayUpdate(paths, config{RelayBaseURL: server.URL}, "token", bufio.NewReader(strings.NewReader("n\n")), &out) {
		t.Fatalf("a declined prompt must not report success")
	}
	if installCalled.Load() {
		t.Fatal("declining the exact preview must not call update/install")
	}
	if !strings.Contains(out.String(), "NOVA Relay App update preview: v0.2.6 → v0.4.0") {
		t.Fatalf("decline must be bound to an exact preview: %s", out.String())
	}
}

func TestGuidedRelayUpdateRejectsStateDriftAfterConfirmation(t *testing.T) {
	paths := guidedUpdatePaths(t)
	var statesCalls atomic.Int32
	var installCalled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			w.Write(registryEnvelope(t, []map[string]interface{}{novaRegistryEntry("update.nova_relay_update")}))
			return
		}
		var req struct {
			Path string `json:"path"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Path == "/api/states" {
			entity := relayUpdateEntity("update.nova_relay_update", "NOVA Relay")
			if statesCalls.Add(1) > 1 {
				entity["state"] = "off"
			}
			w.Write(statesEnvelope(t, []map[string]interface{}{entity}))
			return
		}
		installCalled.Store(true)
		t.Errorf("preview drift must stop before install, got %q", req.Path)
	}))
	defer server.Close()

	var out bytes.Buffer
	if runGuidedRelayUpdate(paths, config{RelayBaseURL: server.URL}, "token", bufio.NewReader(strings.NewReader("y\n")), &out) {
		t.Fatal("changed update state must not report success")
	}
	if installCalled.Load() {
		t.Fatal("changed update state must not call update/install")
	}
	if !strings.Contains(out.String(), "changed after the preview") ||
		!strings.Contains(out.String(), "Nothing was installed") {
		t.Fatalf("missing preview-drift stop message: %s", out.String())
	}
}

func TestGuidedRelayUpdateRejectsRegistryDriftAfterConfirmation(t *testing.T) {
	paths := guidedUpdatePaths(t)
	var registryCalls atomic.Int32
	var installCalled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/core":
			var req struct {
				Path string `json:"path"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if req.Path == "/api/states" {
				w.Write(statesEnvelope(t, []map[string]interface{}{
					relayUpdateEntity("update.nova_relay_update", "NOVA Relay"),
				}))
				return
			}
			installCalled.Store(true)
			t.Errorf("registry drift must stop before install, got %q", req.Path)
		case "/ws":
			uniqueID := relayUpdateUniqueID
			if registryCalls.Add(1) > 1 {
				uniqueID = "different_slug_ha_nova_relay_version_latest"
			}
			w.Write(registryEnvelope(t, []map[string]interface{}{
				relayRegistryEntry("update.nova_relay_update", "hassio", uniqueID),
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var out bytes.Buffer
	if runGuidedRelayUpdate(paths, config{RelayBaseURL: server.URL}, "token", bufio.NewReader(strings.NewReader("y\n")), &out) {
		t.Fatal("changed registry provenance must not report success")
	}
	if installCalled.Load() {
		t.Fatal("changed registry provenance must not call update/install")
	}
	if !strings.Contains(out.String(), "changed after the preview") ||
		!strings.Contains(out.String(), "Nothing was installed") {
		t.Fatalf("missing provenance-drift stop message: %s", out.String())
	}
}

func TestGuidedRelayUpdateRequiresIdleInstallAndBackupSupport(t *testing.T) {
	cases := []struct {
		name         string
		mutate       func(map[string]interface{})
		wantFragment string
	}{
		{
			name: "update already in progress",
			mutate: func(entity map[string]interface{}) {
				entity["attributes"].(map[string]interface{})["in_progress"] = true
			},
			wantFragment: "has no newer pending version",
		},
		{
			name: "backup capability missing",
			mutate: func(entity map[string]interface{}) {
				entity["attributes"].(map[string]interface{})["supported_features"] = updateFeatureInstall
			},
			wantFragment: "does not support both install and partial backup",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			paths := guidedUpdatePaths(t)
			var installCalled atomic.Bool
			entity := relayUpdateEntity("update.nova_relay_update", "NOVA Relay")
			tc.mutate(entity)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/core":
					var req struct {
						Path string `json:"path"`
					}
					json.NewDecoder(r.Body).Decode(&req)
					if req.Path == "/api/states" {
						w.Write(statesEnvelope(t, []map[string]interface{}{entity}))
						return
					}
					installCalled.Store(true)
				case "/ws":
					w.Write(registryEnvelope(t, []map[string]interface{}{novaRegistryEntry("update.nova_relay_update")}))
				case "/health":
					t.Error("unavailable guided update must not start polling")
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			var out bytes.Buffer
			if runGuidedRelayUpdate(paths, config{RelayBaseURL: server.URL}, "token", bufio.NewReader(strings.NewReader("y\n")), &out) {
				t.Fatal("unsafe candidate must not report success")
			}
			if installCalled.Load() {
				t.Fatal("unsafe candidate must not call update/install")
			}
			if strings.Contains(out.String(), "Install the latest available NOVA Relay update now?") {
				t.Fatalf("unsafe candidate must not prompt: %s", out.String())
			}
			if !strings.Contains(out.String(), tc.wantFragment) {
				t.Fatalf("missing refusal reason %q: %s", tc.wantFragment, out.String())
			}
		})
	}
}

func TestGuidedRelayUpdateContainerFallsBackToManualPath(t *testing.T) {
	paths := guidedUpdatePaths(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/core":
			w.Write(statesEnvelope(t, nil))
		case "/ws":
			w.Write(registryEnvelope(t, nil))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var out bytes.Buffer
	if runGuidedRelayUpdate(paths, config{RelayBaseURL: server.URL}, "token", bufio.NewReader(strings.NewReader("")), &out) {
		t.Fatalf("container fallback must not report success")
	}
	if !strings.Contains(out.String(), "no registry-proven NOVA Relay App update entity found") {
		t.Fatalf("missing container reason: %s", out.String())
	}
	if !strings.Contains(out.String(), "Manual path:") {
		t.Fatalf("container path must stay manual: %s", out.String())
	}
	if strings.Contains(out.String(), "Install the latest available NOVA Relay update now?") {
		t.Fatalf("container/manual evidence must not receive an App install prompt: %s", out.String())
	}
}

func TestRunDoctorClassifiesBelowFloorRelayBeforeAnyInstallPrompt(t *testing.T) {
	currentEntity := relayUpdateEntity("update.nova_relay_update", "NOVA Relay")
	currentEntity["state"] = "off"
	malformedEntity := relayUpdateEntity("update.nova_relay_update", "NOVA Relay")
	malformedEntity["attributes"].(map[string]interface{})["latest_version"] = "not-a-version"
	cases := []struct {
		name           string
		states         []map[string]interface{}
		wantPreview    bool
		wantManualPath bool
	}{
		{
			name:           "standalone container gets no App prompt",
			states:         nil,
			wantManualPath: true,
		},
		{
			name:           "current App gets no install prompt",
			states:         []map[string]interface{}{currentEntity},
			wantManualPath: true,
		},
		{
			name:           "malformed App version gets no install prompt",
			states:         []map[string]interface{}{malformedEntity},
			wantManualPath: true,
		},
		{
			name: "exact pending App gets preview before prompt",
			states: []map[string]interface{}{
				relayUpdateEntity("update.nova_relay_update", "NOVA Relay"),
			},
			wantPreview: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			paths, cfg := doctorTestSetup(t)
			if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
				t.Fatalf("mkdir version dir: %v", err)
			}
			if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.21.0","min_relay_version":"0.4.0"}`), 0o644); err != nil {
				t.Fatalf("write version file: %v", err)
			}

			var installCalled atomic.Bool
			relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/core":
					var req struct {
						Path string `json:"path"`
					}
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Errorf("decode core request: %v", err)
						return
					}
					if req.Path == "/api/states" {
						w.Write(statesEnvelope(t, tc.states))
						return
					}
					installCalled.Store(true)
					t.Errorf("unexpected install path %q", req.Path)
				case "/ws":
					w.Write(registryEnvelope(t, []map[string]interface{}{novaRegistryEntry("update.nova_relay_update")}))
				default:
					http.NotFound(w, r)
				}
			}))
			defer relay.Close()
			cfg.RelayBaseURL = relay.URL
			if err := saveConfig(paths, cfg); err != nil {
				t.Fatalf("save config: %v", err)
			}

			oldHealth := fetchRelayHealthForReadiness
			oldPing := probeRelayWSPingForReadiness
			fetchRelayHealthForReadiness = func(string, string) ([]byte, error) {
				return []byte(`{"status":"ok","data":{"ha_ws_connected":true},"version":"0.3.0"}`), nil
			}
			probeRelayWSPingForReadiness = func(string, string) (relayWSPingResponse, error) {
				return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"type":"pong"}`)}, nil
			}
			t.Cleanup(func() {
				fetchRelayHealthForReadiness = oldHealth
				probeRelayWSPingForReadiness = oldPing
			})
			forceGuidedUpdateTTY(t, "")

			exitCode, output := captureCommandOutput(t, func() int {
				return runDoctor(paths, nil)
			})
			if exitCode == 0 {
				t.Fatalf("declined/unavailable below-floor update must keep doctor failing:\n%s", output)
			}
			if installCalled.Load() {
				t.Fatal("doctor must not install without a confirmed current preview")
			}
			hasPreview := strings.Contains(output, "NOVA Relay App update preview:")
			hasPrompt := strings.Contains(output, "Install the latest available NOVA Relay update now?")
			if tc.wantPreview {
				if !hasPreview || !hasPrompt || strings.Index(output, "NOVA Relay App update preview:") > strings.Index(output, "Install the latest available NOVA Relay update now?") {
					t.Fatalf("exact App evidence must produce preview then prompt:\n%s", output)
				}
			} else if hasPreview || hasPrompt {
				t.Fatalf("standalone evidence must not produce an App preview or prompt:\n%s", output)
			}
			if tc.wantManualPath && !strings.Contains(output, relayUpdateManualPath) {
				t.Fatalf("standalone evidence must produce the manual path:\n%s", output)
			}
		})
	}
}

func TestMaybeOfferGuidedRelayUpdateIgnoresOtherNoticeKinds(t *testing.T) {
	paths := guidedUpdatePaths(t)
	// Wrong kind returns before any prompt or network use; under `go test`
	// stdin is a pipe, so the TTY gate also holds both Relay notice kinds.
	maybeOfferGuidedRelayUpdate(paths, humanNotice{kind: humanNoticeKindUpdateAvailable, level: humanNoticeWarning})
	maybeOfferGuidedRelayUpdate(paths, humanNotice{kind: humanNoticeKindRelayOutdated, level: humanNoticeWarning})
	maybeOfferGuidedRelayUpdate(paths, humanNotice{kind: humanNoticeKindRelayUpdateAvailable, level: humanNoticeWarning})
}
