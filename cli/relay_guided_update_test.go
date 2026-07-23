package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func guidedUpdatePaths(t *testing.T) runtimePaths {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.15.0","min_relay_version":"0.4.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}
	return paths
}

func statesEnvelope(t *testing.T, states []map[string]interface{}) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{
		"ok":   true,
		"data": map[string]interface{}{"status": 200, "body": states},
	})
	if err != nil {
		t.Fatalf("marshal states envelope: %v", err)
	}
	return body
}

func registryEnvelope(t *testing.T, entries []map[string]interface{}) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{"ok": true, "data": entries})
	if err != nil {
		t.Fatalf("marshal registry envelope: %v", err)
	}
	return body
}

func relayRegistryEntry(entityID, platform, uniqueID string) map[string]interface{} {
	return map[string]interface{}{
		"entity_id": entityID,
		"platform":  platform,
		"unique_id": uniqueID,
	}
}

func novaRegistryEntry(entityID string) map[string]interface{} {
	return relayRegistryEntry(entityID, "hassio", relayUpdateUniqueID)
}

func relayUpdateEntity(id, title string) map[string]interface{} {
	return map[string]interface{}{
		"entity_id": id,
		"state":     "on",
		"attributes": map[string]interface{}{
			"title":              title,
			"installed_version":  "0.2.6",
			"latest_version":     "0.4.0",
			"supported_features": updateFeatureInstall | updateFeatureBackup,
			"in_progress":        false,
		},
	}
}

func TestResolveRelayUpdateEntity(t *testing.T) {
	cases := []struct {
		name       string
		states     []map[string]interface{}
		registry   []map[string]interface{}
		wantEntity string
		wantReason string
	}{
		{
			name: "exact match",
			states: []map[string]interface{}{
				relayUpdateEntity("update.home_assistant_core_update", "Home Assistant Core"),
				relayUpdateEntity("update.nova_relay_update", "NOVA Relay"),
				// A non-update domain carrying the title must not match.
				relayUpdateEntity("sensor.nova_relay_update", "NOVA Relay"),
			},
			registry: []map[string]interface{}{
				relayRegistryEntry("update.home_assistant_core_update", "hassio", "core"),
				novaRegistryEntry("update.nova_relay_update"),
				relayRegistryEntry("sensor.nova_relay_update", "hassio", relayUpdateUniqueID),
			},
			wantEntity: "update.nova_relay_update",
		},
		{
			name: "matching title without registry provenance is rejected",
			states: []map[string]interface{}{
				relayUpdateEntity("update.nova_relay_update", "NOVA Relay"),
			},
			registry: []map[string]interface{}{
				relayRegistryEntry("update.nova_relay_update", "mqtt", relayUpdateUniqueID),
			},
			wantReason: "no registry-proven NOVA Relay App update entity found",
		},
		{
			name: "different hassio App slug is rejected",
			states: []map[string]interface{}{
				relayUpdateEntity("update.nova_relay_update", "NOVA Relay"),
			},
			registry: []map[string]interface{}{
				relayRegistryEntry("update.nova_relay_update", "hassio", "different_slug_ha_nova_relay_version_latest"),
			},
			wantReason: "no registry-proven NOVA Relay App update entity found",
		},
		{
			name: "ambiguity is never guessed",
			states: []map[string]interface{}{
				relayUpdateEntity("update.nova_relay_update", "NOVA Relay"),
				relayUpdateEntity("update.nova_relay_update_2", "NOVA Relay"),
			},
			registry: []map[string]interface{}{
				novaRegistryEntry("update.nova_relay_update"),
				novaRegistryEntry("update.nova_relay_update_2"),
			},
			wantReason: "several registry-proven NOVA Relay App update entities",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/core":
					w.Write(statesEnvelope(t, tc.states))
				case "/ws":
					w.Write(registryEnvelope(t, tc.registry))
				default:
					t.Errorf("unexpected path %q", r.URL.Path)
				}
			}))
			defer server.Close()
			candidate, reason := resolveRelayUpdateCandidate(config{RelayBaseURL: server.URL}, "token")
			entity := candidate.EntityID
			if entity != tc.wantEntity {
				t.Fatalf("entity = %q, want %q (reason %q)", entity, tc.wantEntity, reason)
			}
			if tc.wantReason != "" && !strings.Contains(reason, tc.wantReason) {
				t.Fatalf("reason = %q, want it to contain %q", reason, tc.wantReason)
			}
		})
	}
}

func shrinkGuidedUpdatePolling(t *testing.T, timeout time.Duration) {
	t.Helper()
	oldInterval, oldTimeout := relayUpdatePollInterval, relayUpdatePollTimeout
	relayUpdatePollInterval = time.Millisecond
	relayUpdatePollTimeout = timeout
	t.Cleanup(func() {
		relayUpdatePollInterval = oldInterval
		relayUpdatePollTimeout = oldTimeout
	})
}

func forceGuidedUpdateTTY(t *testing.T, input string) {
	t.Helper()
	oldInputTTY := relayUpdateInputIsTTY
	oldOutputTTY := relayUpdateOutputIsTTY
	oldInput := relayUpdateInput
	oldOutput := relayUpdateOutput
	relayUpdateInputIsTTY = func() bool { return true }
	relayUpdateOutputIsTTY = func() bool { return true }
	relayUpdateInput = func() *bufio.Reader {
		return bufio.NewReader(strings.NewReader(input))
	}
	relayUpdateOutput = func() io.Writer { return os.Stdout }
	t.Cleanup(func() {
		relayUpdateInputIsTTY = oldInputTTY
		relayUpdateOutputIsTTY = oldOutputTTY
		relayUpdateInput = oldInput
		relayUpdateOutput = oldOutput
	})
}

func TestGuidedRelayUpdateInstallsThroughTheRestartAndVerifies(t *testing.T) {
	paths := guidedUpdatePaths(t)
	shrinkGuidedUpdatePolling(t, time.Second)
	var installed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			version := "0.2.6"
			if installed.Load() {
				version = "0.4.0"
			}
			fmt.Fprintf(w, `{"ok":true,"data":{"version":%q}}`, version)
		case "/core":
			var req struct {
				Path string `json:"path"`
				Body struct {
					EntityID string `json:"entity_id"`
					Backup   bool   `json:"backup"`
					Version  string `json:"version"`
				} `json:"body"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode core payload: %v", err)
			}
			if req.Path == "/api/states" {
				w.Write(statesEnvelope(t, []map[string]interface{}{
					relayUpdateEntity("update.nova_relay_update", "NOVA Relay"),
				}))
				return
			}
			if req.Path != "/api/services/update/install" {
				t.Errorf("unexpected core path %q", req.Path)
				return
			}
			if req.Body.EntityID != "update.nova_relay_update" || !req.Body.Backup || req.Body.Version != "" {
				t.Errorf("install payload = %+v, want resolved entity, backup:true, and no unsupported version binding", req.Body)
			}
			installed.Store(true)
			// The install restarts the relay mid-call: drop the connection
			// instead of answering — the flow must treat this as normal.
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			conn.Close()
		case "/ws":
			w.Write(registryEnvelope(t, []map[string]interface{}{novaRegistryEntry("update.nova_relay_update")}))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	var out bytes.Buffer
	verified := runGuidedRelayUpdate(paths, config{RelayBaseURL: server.URL}, "token", bufio.NewReader(strings.NewReader("\n")), &out)
	if !verified {
		t.Fatalf("verified install must report success; output: %s", out.String())
	}
	if !installed.Load() {
		t.Fatalf("update/install was never called; output: %s", out.String())
	}
	if !strings.Contains(out.String(), "Relay updated and verified: v0.4.0 is running.") {
		t.Fatalf("missing verified-version report in output: %s", out.String())
	}
	previewIndex := strings.Index(out.String(), "NOVA Relay App update preview: v0.2.6 → v0.4.0")
	confirmationIndex := strings.Index(out.String(), "Install the latest available NOVA Relay update now?")
	if previewIndex < 0 || confirmationIndex < 0 || previewIndex > confirmationIndex {
		t.Fatalf("exact preview must precede confirmation: %s", out.String())
	}
}

func TestGuidedRelayUpdatePollTimeoutStaysHonest(t *testing.T) {
	paths := guidedUpdatePaths(t)
	shrinkGuidedUpdatePolling(t, 20*time.Millisecond)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			// The relay never comes back with a new version.
			fmt.Fprint(w, `{"ok":true,"data":{"version":"0.2.6"}}`)
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
			w.Write([]byte(`{"ok":true,"data":{"status":200}}`))
		case "/ws":
			w.Write(registryEnvelope(t, []map[string]interface{}{novaRegistryEntry("update.nova_relay_update")}))
		}
	}))
	defer server.Close()

	var out bytes.Buffer
	if runGuidedRelayUpdate(paths, config{RelayBaseURL: server.URL}, "token", bufio.NewReader(strings.NewReader("y\n")), &out) {
		t.Fatalf("timeout must not report success")
	}
	if !strings.Contains(out.String(), "did not report a new version") {
		t.Fatalf("missing honest timeout message: %s", out.String())
	}
	if !strings.Contains(out.String(), "Manual path:") {
		t.Fatalf("timeout must include the manual path: %s", out.String())
	}
}

func TestGuidedRelayUpdateRejectedInstallSkipsTheWait(t *testing.T) {
	paths := guidedUpdatePaths(t)
	shrinkGuidedUpdatePolling(t, time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			t.Errorf("an explicit rejection must not start the health poll")
			return
		}
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
		w.Write([]byte(`{"ok":true,"data":{"status":403}}`))
	}))
	defer server.Close()

	var out bytes.Buffer
	if runGuidedRelayUpdate(paths, config{RelayBaseURL: server.URL}, "token", bufio.NewReader(strings.NewReader("y\n")), &out) {
		t.Fatalf("rejection must not report success")
	}
	if !strings.Contains(out.String(), "Home Assistant rejected the install call.") {
		t.Fatalf("missing rejection message: %s", out.String())
	}
	if !strings.Contains(out.String(), "Manual path:") {
		t.Fatalf("rejection must include the manual path: %s", out.String())
	}
}

func TestGuidedRelayUpdateRelayTimeoutEnvelopeStillPolls(t *testing.T) {
	// The relay's upstream client times out at 10s and maps that to an
	// ok:false envelope while the App update keeps installing — that is a
	// restart candidate, not a rejection, and must continue into the poll.
	paths := guidedUpdatePaths(t)
	shrinkGuidedUpdatePolling(t, time.Second)
	var installed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			version := "0.2.6"
			if installed.Load() {
				version = "0.4.0"
			}
			fmt.Fprintf(w, `{"ok":true,"data":{"version":%q}}`, version)
			return
		}
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
		installed.Store(true)
		w.Write([]byte(`{"ok":false,"error":{"code":"UPSTREAM_TIMEOUT","message":"HA REST call timed out"}}`))
	}))
	defer server.Close()

	var out bytes.Buffer
	if !runGuidedRelayUpdate(paths, config{RelayBaseURL: server.URL}, "token", bufio.NewReader(strings.NewReader("y\n")), &out) {
		t.Fatalf("relay-side timeout must poll and verify; output: %s", out.String())
	}
	if !strings.Contains(out.String(), "Relay updated and verified: v0.4.0 is running.") {
		t.Fatalf("missing verified report: %s", out.String())
	}
}
