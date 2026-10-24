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

			notice := relayAvailableUpdateNotice(config{RelayBaseURL: server.URL}, "token", "")
			if tc.wantNotice {
				if notice.kind != humanNoticeKindRelayUpdateAvailable {
					t.Fatalf("notice kind = %q, want %q", notice.kind, humanNoticeKindRelayUpdateAvailable)
				}
				if !strings.Contains(notice.message, ": 0.4.1 → 0.6.0.") {
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
		wantPage   bool // below the floor with a pending App update: the setup page may update it
	}{
		{
			name: "exact pending App update",
			states: []map[string]interface{}{
				pendingRelayUpdateEntity("update.nova_relay_update", "0.3.0", "0.7.1"),
			},
			wantKind:   humanNoticeKindRelayUpdateAvailable,
			wantText:   relayAppUpdatePath,
			rejectText: relayContainerUpdateLine,
			wantPage:   true,
		},
		{
			name:       "no App update entity",
			states:     []map[string]interface{}{},
			wantKind:   humanNoticeKindRelayOutdated,
			wantText:   relayContainerUpdateLine,
			rejectText: "No Update button yet",
		},
		{
			name:       "registry proves current App even without pending update",
			states:     []map[string]interface{}{currentApp},
			wantKind:   humanNoticeKindRelayOutdated,
			wantText:   "No Update button yet? In Settings > Apps, check for updates first.",
			rejectText: relayContainerUpdateLine,
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
			// Plain words for a person: what an AI app does comes from the kind.
			for _, agentText := range []string{"Inform the user", "ha-nova:updates", "registry", "entity"} {
				if strings.Contains(notice.message, agentText) {
					t.Fatalf("notice speaks to the AI app (%q): %s", agentText, notice.message)
				}
			}
			if strings.Contains(notice.message, tc.rejectText) {
				t.Fatalf("notice must not contain %q: %s", tc.rejectText, notice.message)
			}
			if notice.belowFloor != tc.wantPage || notice.appUpdate != tc.wantPage {
				t.Fatalf("belowFloor=%v appUpdate=%v, want %v", notice.belowFloor, notice.appUpdate, tc.wantPage)
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
			fmt.Fprintf(w, `{"ok":true,"data":{"version":%q,"ha_ws_connected":%t}}`, version, version == "0.6.0")
		case "/core":
			var req struct {
				Path string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode core payload: %v", err)
			}
			if req.Path == "/api/states" {
				if installed.Load() {
					w.Write(statesEnvelope(t, []map[string]interface{}{
						completedRelayUpdateEntity("update.nova_relay_update", "0.6.0"),
					}))
					return
				}
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
			writeRegistryOrPingResponse(t, w, r, "update.nova_relay_update")
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
	if !strings.Contains(out.String(), "The NOVA App is updated and works: v0.6.0.") {
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
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"ok":true,"data":{"type":"pong"}}`)}, nil
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, nil)
	})
	if exitCode != 0 {
		t.Fatalf("compatible pending Relay update must not fail doctor: exit %d\n%s", exitCode, output)
	}
	if !strings.Contains(output, "The NOVA App in Home Assistant has an update: 0.4.1 → 0.6.0") || strings.Contains(output, "WARNING: The NOVA App") {
		t.Fatalf("doctor did not surface the pending Relay update:\n%s", output)
	}
	if !strings.Contains(output, "All checks passed") {
		t.Fatalf("doctor did not preserve compatible-health success:\n%s", output)
	}
}

func TestRunCheckUpdateQuietReportsHANOVAAndAboveFloorRelayUpdates(t *testing.T) {
	resetServerProfileSelection(t) // a leaked --server keeps the App notice separate
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
	skillNotice := "HA NOVA 0.21.0 is ready (you have 0.20.0)."
	// One notice names both updates, so the chat offer can too; the App's
	// own notice is not printed a second time (`ha-nova update` offers it).
	if !strings.Contains(output, skillNotice) || !strings.Contains(output, appUpdateTooLine+"\nTo update, run: ha-nova update") {
		t.Fatalf("the HA NOVA notice must name the App update too:\n%s", output)
	}
	if strings.Contains(output, "has an update: 0.4.1 → 0.6.0") || strings.Contains(output, "WARNING") {
		t.Fatalf("one plain notice per run:\n%s", output)
	}
}

// doctor says the App notice in check-update's words: no WARNING label, and
// "No Update button yet?" when Home Assistant knows the App but offers no
// update yet (rc5 recheck R2).
func TestRunDoctorBelowFloorUsesCheckUpdateWords(t *testing.T) {
	paths, cfg := doctorTestSetup(t)
	currentApp := pendingRelayUpdateEntity("update.nova_relay_update", "0.3.0", "0.3.0")
	currentApp["state"] = "off"
	health := `{"ok":true,"data":{"status":"ok","ha_ws_connected":true,"version":"0.3.0"}}`
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			fmt.Fprint(w, health)
		case "/core":
			w.Write(statesEnvelope(t, []map[string]interface{}{currentApp}))
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
	defer func() { fetchRelayHealthForReadiness = originalHealth }()
	fetchRelayHealthForReadiness = func(string, string) ([]byte, error) { return []byte(health), nil }

	exitCode, output := captureCommandOutput(t, func() int { return runDoctor(paths, []string{"--quiet"}) })
	want := "[ha-nova] The NOVA App in Home Assistant needs an update: it has 0.3.0, HA NOVA needs 0.4.0 or newer. " +
		relayAppUpdatePath + " No Update button yet? In Settings > Apps, check for updates first."
	if exitCode != 1 || !strings.Contains(output, want) || !strings.Contains(output, "To fix: update the NOVA App (see above).") {
		t.Fatalf("exit %d, want 1 and %q:\n%s", exitCode, want, output)
	}
}

// After a Supervisor update the entity can still show the old installed
// version while the Relay already runs the new one: the Relay wins.
func TestRelayAvailableUpdateNoticePrefersTheRunningRelayVersion(t *testing.T) {
	stale := relayUpdateCandidate{EntityID: "update.nova_relay_update", State: "on", InstalledVersion: "0.11.3", LatestVersion: "0.11.4"}
	if notice := relayAvailableUpdateNoticeFromCandidate(stale, "0.11.4"); !notice.empty() {
		t.Fatalf("Relay already at latest, want no notice: %q", notice.message)
	}
	notice := relayAvailableUpdateNoticeFromCandidate(stale, "0.11.2")
	if !strings.Contains(notice.message, ": 0.11.2 → 0.11.4.") {
		t.Fatalf("want the running version as the from-version: %q", notice.message)
	}
	if notice := relayAvailableUpdateNoticeFromCandidate(stale, ""); !strings.Contains(notice.message, ": 0.11.3 → 0.11.4.") {
		t.Fatalf("no running version: want the entity's: %q", notice.message)
	}
}
