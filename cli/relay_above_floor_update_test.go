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
			"title":             relayUpdateEntityTitle,
			"installed_version": installed,
			"latest_version":    latest,
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
					"title":             relayUpdateEntityTitle,
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
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Write(statesEnvelope(t, tc.states))
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

func TestRunDoctorReportsAboveFloorRelayUpdateWithoutFailing(t *testing.T) {
	paths, cfg := doctorTestSetup(t)
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/core" {
			http.NotFound(w, r)
			return
		}
		w.Write(statesEnvelope(t, []map[string]interface{}{
			pendingRelayUpdateEntity("update.nova_relay_update", "0.4.1", "0.6.0"),
		}))
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
