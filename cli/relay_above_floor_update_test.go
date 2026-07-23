package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func pendingRelayUpdateEntity(id, installed, latest string) map[string]interface{} {
	return map[string]interface{}{
		"entity_id": id,
		"state":     "on",
		"attributes": map[string]interface{}{
			"title":              "NOVA Relay",
			"installed_version":  installed,
			"latest_version":     latest,
			"supported_features": updateFeatureInstall | updateFeatureBackup,
			"in_progress":        false,
		},
	}
}

func TestRelayAvailableUpdateNoticeRequiresExactPendingEvidence(t *testing.T) {
	cases := []struct {
		name       string
		states     []map[string]interface{}
		wantNotice bool
	}{
		{
			name:       "pending exact App update",
			states:     []map[string]interface{}{pendingRelayUpdateEntity("update.nova_relay_update", "0.4.1", "0.6.0")},
			wantNotice: true,
		},
		{
			name: "current update entity",
			states: []map[string]interface{}{{
				"entity_id": "update.nova_relay_update",
				"state":     "off",
				"attributes": map[string]interface{}{
					"title":             "NOVA Relay",
					"installed_version": "0.6.0",
					"latest_version":    "0.6.0",
				},
			}},
		},
		{
			name: "malformed versions",
			states: []map[string]interface{}{
				pendingRelayUpdateEntity("update.nova_relay_update", "unknown", "0.6.0"),
			},
		},
		{
			name: "ambiguous exact title",
			states: []map[string]interface{}{
				pendingRelayUpdateEntity("update.nova_relay_update", "0.4.1", "0.6.0"),
				pendingRelayUpdateEntity("update.nova_relay_update_2", "0.4.1", "0.6.0"),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/core":
					w.Write(statesEnvelope(t, tc.states))
				case "/ws":
					registry := []map[string]interface{}{}
					for _, state := range tc.states {
						if entityID, ok := state["entity_id"].(string); ok {
							registry = append(registry, novaRegistryEntry(entityID))
						}
					}
					w.Write(registryEnvelope(t, registry))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			notice := relayAvailableUpdateNotice(config{RelayBaseURL: server.URL}, "token")
			if tc.wantNotice {
				if notice.kind != humanNoticeKindRelayUpdateAvailable {
					t.Fatalf("notice kind = %q, want %q", notice.kind, humanNoticeKindRelayUpdateAvailable)
				}
				if !strings.Contains(notice.message, "v0.4.1 → v0.6.0") {
					t.Fatalf("notice does not name installed and latest versions: %q", notice.message)
				}
			} else if !notice.empty() {
				t.Fatalf("unexpected notice: %q", notice.message)
			}
		})
	}
}

func TestRelayUpdateNoticeClassifiesBelowFloorAppBeforeOfferingGuidedUpdate(t *testing.T) {
	currentApp := pendingRelayUpdateEntity("update.nova_relay_update", "0.3.0", "0.3.0")
	currentApp["state"] = "off"
	cases := []struct {
		name       string
		states     []map[string]interface{}
		wantKind   humanNoticeKind
		wantText   string
		rejectText string
	}{
		{
			name: "exact pending App update",
			states: []map[string]interface{}{
				pendingRelayUpdateEntity("update.nova_relay_update", "0.3.0", "0.7.1"),
			},
			wantKind:   humanNoticeKindRelayUpdateAvailable,
			wantText:   "offer to prepare the guided App update through ha-nova:updates",
			rejectText: "standalone Relay container",
		},
		{
			name:       "no App update entity",
			states:     []map[string]interface{}{},
			wantKind:   humanNoticeKindRelayOutdated,
			wantText:   "pull and recreate a standalone Relay container",
			rejectText: "ha-nova:updates",
		},
		{
			name:       "registry proves current App even without pending update",
			states:     []map[string]interface{}{currentApp},
			wantKind:   humanNoticeKindRelayOutdated,
			wantText:   "confirms the NOVA Relay App is installed",
			rejectText: "standalone Relay container",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			paths, cfg := doctorTestSetup(t)
			relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/health":
					fmt.Fprint(w, `{"ok":true,"data":{"status":"ok","version":"0.3.0"}}`)
				case "/core":
					w.Write(statesEnvelope(t, tc.states))
				case "/ws":
					registry := []map[string]interface{}{}
					for _, state := range tc.states {
						if entityID, ok := state["entity_id"].(string); ok {
							registry = append(registry, novaRegistryEntry(entityID))
						}
					}
					w.Write(registryEnvelope(t, registry))
				default:
					http.NotFound(w, r)
				}
			}))
			defer relay.Close()

			cfg.RelayBaseURL = relay.URL
			if err := saveConfig(paths, cfg); err != nil {
				t.Fatalf("saveConfig() error: %v", err)
			}
			if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
				t.Fatalf("mkdir version dir: %v", err)
			}
			if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.21.0","min_relay_version":"0.4.0"}`), 0o644); err != nil {
				t.Fatalf("write version file: %v", err)
			}

			notice := relayUpdateNotice(paths)
			if notice.kind != tc.wantKind {
				t.Fatalf("notice kind = %q, want %q; message: %s", notice.kind, tc.wantKind, notice.message)
			}
			if !strings.Contains(notice.message, tc.wantText) {
				t.Fatalf("notice missing %q: %s", tc.wantText, notice.message)
			}
			if strings.Contains(notice.message, tc.rejectText) {
				t.Fatalf("notice must not contain %q: %s", tc.rejectText, notice.message)
			}
		})
	}
}

func TestGuidedRelayUpdateWaitsForOfferedAboveFloorVersion(t *testing.T) {
	paths := guidedUpdatePaths(t)
	shrinkGuidedUpdatePolling(t, time.Second)
	var installed atomic.Bool
	var healthPolls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			poll := healthPolls.Add(1)
			version := "0.4.1" // Already above the 0.4.0 compatibility floor.
			if installed.Load() && poll >= 3 {
				version = "0.6.0"
			}
			fmt.Fprintf(w, `{"ok":true,"data":{"version":%q}}`, version)
		case "/core":
			var req struct {
				Path string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode core payload: %v", err)
			}
			if req.Path == "/api/states" {
				w.Write(statesEnvelope(t, []map[string]interface{}{
					pendingRelayUpdateEntity("update.nova_relay_update", "0.4.1", "0.6.0"),
				}))
				return
			}
			if req.Path != "/api/services/update/install" {
				t.Errorf("unexpected core path %q", req.Path)
				return
			}
			installed.Store(true)
			fmt.Fprint(w, `{"ok":true,"data":{"status":200}}`)
		case "/ws":
			w.Write(registryEnvelope(t, []map[string]interface{}{novaRegistryEntry("update.nova_relay_update")}))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	var out bytes.Buffer
	if !runGuidedRelayUpdate(paths, config{RelayBaseURL: server.URL}, "token", bufio.NewReader(strings.NewReader("y\n")), &out) {
		t.Fatalf("above-floor update must verify the offered target; output: %s", out.String())
	}
	if healthPolls.Load() < 3 {
		t.Fatalf("guided update accepted the old above-floor relay after %d poll(s)", healthPolls.Load())
	}
	if !strings.Contains(out.String(), "Relay updated and verified: v0.6.0 is running.") {
		t.Fatalf("missing target-version verification: %s", out.String())
	}
}

func TestWaitForRelayVersionKeepsFloorForStaleTarget(t *testing.T) {
	paths := guidedUpdatePaths(t)
	shrinkGuidedUpdatePolling(t, time.Second)
	var healthPolls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		version := "0.3.0" // Offered target, but still below the 0.4.0 floor.
		if healthPolls.Add(1) >= 3 {
			version = "0.4.0"
		}
		fmt.Fprintf(w, `{"ok":true,"data":{"version":%q}}`, version)
	}))
	defer server.Close()

	version, ok := waitForRelayVersion(paths, config{RelayBaseURL: server.URL}, "token", "0.3.0")
	if !ok || version != "0.4.0" {
		t.Fatalf("verification = (%q, %v), want floor-compatible v0.4.0", version, ok)
	}
	if healthPolls.Load() < 3 {
		t.Fatalf("verification accepted the stale below-floor target after %d poll(s)", healthPolls.Load())
	}
}

func TestRunDoctorReportsAboveFloorRelayUpdateWithoutFailing(t *testing.T) {
	paths, cfg := doctorTestSetup(t)
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/core":
			w.Write(statesEnvelope(t, []map[string]interface{}{
				pendingRelayUpdateEntity("update.nova_relay_update", "0.4.1", "0.6.0"),
			}))
		case "/ws":
			w.Write(registryEnvelope(t, []map[string]interface{}{novaRegistryEntry("update.nova_relay_update")}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer relay.Close()
	cfg.RelayBaseURL = relay.URL
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.18.0","min_relay_version":"0.4.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}

	originalHealth := fetchRelayHealthForReadiness
	originalWSPing := probeRelayWSPingForReadiness
	defer func() {
		fetchRelayHealthForReadiness = originalHealth
		probeRelayWSPingForReadiness = originalWSPing
	}()
	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"ok":true,"data":{"status":"ok","ha_ws_connected":true,"version":"0.4.1"}}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"type":"pong"}`)}, nil
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, nil)
	})
	if exitCode != 0 {
		t.Fatalf("compatible pending Relay update must not fail doctor: exit %d\n%s", exitCode, output)
	}
	if !strings.Contains(output, "Relay update available: v0.4.1 → v0.6.0") {
		t.Fatalf("doctor did not surface the pending Relay update:\n%s", output)
	}
	if !strings.Contains(output, "Doctor checks passed") {
		t.Fatalf("doctor did not preserve compatible-health success:\n%s", output)
	}
}

func TestRunCheckUpdateQuietReportsHANOVAAndAboveFloorRelayUpdates(t *testing.T) {
	paths, cfg := doctorTestSetup(t)
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			fmt.Fprint(w, `{"ok":true,"data":{"status":"ok","version":"0.4.1"}}`)
		case "/core":
			w.Write(statesEnvelope(t, []map[string]interface{}{
				pendingRelayUpdateEntity("update.nova_relay_update", "0.4.1", "0.6.0"),
			}))
		case "/ws":
			w.Write(registryEnvelope(t, []map[string]interface{}{novaRegistryEntry("update.nova_relay_update")}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer relay.Close()

	cfg.RelayBaseURL = relay.URL
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.20.0","min_relay_version":"0.4.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}
	if err := writeJSONFile(paths.UpdateCacheFile, releaseInfo{
		Version: "0.21.0",
		HTMLURL: "https://example.invalid/releases/v0.21.0",
	}, 0o644); err != nil {
		t.Fatalf("write update cache: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runCheckUpdate(paths, []string{"--quiet"})
	})
	if exitCode != 0 {
		t.Fatalf("compatible pending Relay update must not fail check-update: exit %d\n%s", exitCode, output)
	}
	skillNotice := "Update available: v0.20.0 -> v0.21.0"
	relayNotice := "Relay update available: v0.4.1 → v0.6.0"
	if !strings.Contains(output, skillNotice) || !strings.Contains(output, relayNotice) {
		t.Fatalf("quiet check-update did not surface both updates:\n%s", output)
	}
	if strings.Index(output, skillNotice) > strings.Index(output, relayNotice) {
		t.Fatalf("HA NOVA update must precede the Relay update:\n%s", output)
	}
}

func TestRunCheckUpdateQuietBoundsRelayDiscoveryAsOneDeadline(t *testing.T) {
	paths, cfg := doctorTestSetup(t)
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer relay.Close()
	cfg.RelayBaseURL = relay.URL
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.20.0","min_relay_version":"0.4.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}
	if err := writeJSONFile(paths.UpdateCacheFile, releaseInfo{
		Version: "0.20.0",
		HTMLURL: "https://example.invalid/releases/v0.20.0",
	}, 0o644); err != nil {
		t.Fatalf("write update cache: %v", err)
	}
	t.Setenv("HA_NOVA_NO_CENSUS", "1")
	previousTimeout := firstUseRelayNoticeTimeout
	firstUseRelayNoticeTimeout = 20 * time.Millisecond
	t.Cleanup(func() { firstUseRelayNoticeTimeout = previousTimeout })

	start := time.Now()
	if exit := runCheckUpdate(paths, []string{"--quiet"}); exit != 0 {
		t.Fatalf("best-effort Relay timeout changed exit code: %d", exit)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("quiet Relay discovery exceeded one bounded deadline: %s", elapsed)
	}
}

func TestRunCheckUpdateQuietSurfacesReturnToStable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.22.0-rc1","min_relay_version":"0.4.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}
	if err := writeJSONFile(paths.UpdateCacheFile, releaseInfo{
		Version: "0.21.0",
		HTMLURL: "https://example.invalid/releases/v0.21.0",
	}, 0o644); err != nil {
		t.Fatalf("write update cache: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runCheckUpdate(paths, []string{"--quiet"})
	})
	if exitCode != 0 {
		t.Fatalf("stable-return notice must not fail check-update: exit %d\n%s", exitCode, output)
	}
	if !strings.Contains(output, "Return to stable: v0.22.0-rc1 -> v0.21.0") {
		t.Fatalf("quiet check-update missed stable-return notice:\n%s", output)
	}
}
