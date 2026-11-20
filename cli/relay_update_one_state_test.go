package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// CLI-03: the App update check reads the registry, then only the state of
// the registry-proven NOVA App update entity, never every state.
func TestRelayUpdateCandidateReadsOnlyTheAppsState(t *testing.T) {
	var mu sync.Mutex
	var reads []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/core":
			var req struct {
				Path string `json:"path"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			reads = append(reads, req.Path)
			mu.Unlock()
			w.Write(statesReply(t, req.Path, []map[string]interface{}{
				relayUpdateEntity("update.nova_relay_update", "NOVA Relay"),
				{"entity_id": "light.hall", "state": "on"},
			}))
		case "/ws":
			w.Write(registryEnvelope(t, []map[string]interface{}{
				novaRegistryEntry("update.nova_relay_update"),
				{"entity_id": "update.other_app", "platform": "hassio", "unique_id": "other_app_version"},
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	candidate, reason := resolveRelayUpdateCandidateWithTransport(context.Background(), server.URL, server.Client(), "token")
	if reason != "" || candidate.EntityID != "update.nova_relay_update" || strings.Join(reads, ",") != "/api/states/update.nova_relay_update" {
		t.Fatalf("candidate %+v, reason %q, reads %v", candidate, reason, reads)
	}
}

// Each registry-proven entity's state is read once, but every matching
// registry row counts: duplicate live rows are several, never one; a row
// without a state doesn't hide a live one; a malformed entity_id fails
// closed before any request.
func TestRelayUpdateCandidateCountsRowsReadsEachIDOnce(t *testing.T) {
	live := relayUpdateEntity("update.nova_relay_update", "NOVA Relay")
	for name, tc := range map[string]struct {
		registry []map[string]interface{}
		reads    string
		entity   string
		reason   string
	}{
		"duplicate live rows": {[]map[string]interface{}{novaRegistryEntry("update.nova_relay_update"), novaRegistryEntry("update.nova_relay_update")}, "/api/states/update.nova_relay_update", "", "several"},
		"missing beside live": {[]map[string]interface{}{novaRegistryEntry("update.nova_relay_gone"), novaRegistryEntry("update.nova_relay_update"), novaRegistryEntry("update.nova_relay_gone")}, "/api/states/update.nova_relay_gone,/api/states/update.nova_relay_update", "update.nova_relay_update", ""},
		"malformed entity_id": {[]map[string]interface{}{novaRegistryEntry("update.nova_relay_update"), novaRegistryEntry("update.x/../../config")}, "", "", "malformed"},
	} {
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			var reads []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/ws" {
					w.Write(registryEnvelope(t, tc.registry))
					return
				}
				var req struct {
					Path string `json:"path"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				mu.Lock()
				reads = append(reads, req.Path)
				mu.Unlock()
				w.Write(statesReply(t, req.Path, []map[string]interface{}{live}))
			}))
			defer server.Close()
			candidate, reason := resolveRelayUpdateCandidateWithTransport(context.Background(), server.URL, server.Client(), "token")
			if candidate.EntityID != tc.entity || !strings.Contains(reason, tc.reason) || (tc.reason == "") != (reason == "") || strings.Join(reads, ",") != tc.reads {
				t.Fatalf("candidate %+v, reason %q, reads %v", candidate, reason, reads)
			}
		})
	}
}

// A refresh or install reads the registry again after the state: an
// entity_id handed to another integration meanwhile is refused, not joined
// with the NOVA row read before it.
func TestRelayUpdateRouteRefusesProvenanceChangedDuringRead(t *testing.T) {
	var registryReads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			row := novaRegistryEntry("update.nova_relay_update")
			if registryReads.Add(1) > 1 {
				row = relayRegistryEntry("update.nova_relay_update", "mqtt", "other")
			}
			w.Write(registryEnvelope(t, []map[string]interface{}{row}))
			return
		}
		w.Write(coreStatesReply(t, r, []map[string]interface{}{relayUpdateEntity("update.nova_relay_update", "NOVA Relay")}))
	}))
	defer server.Close()
	candidate, reason := homeUpdateRoute(config{RelayBaseURL: server.URL}, "token").candidate()
	if candidate.EntityID != "" || !strings.Contains(reason, "changed while it was read") || registryReads.Load() != 2 {
		t.Fatalf("candidate %+v, reason %q, registry reads %d", candidate, reason, registryReads.Load())
	}
	registryReads.Store(-10)
	if candidate, reason := homeUpdateRoute(config{RelayBaseURL: server.URL}, "token").candidate(); candidate.EntityID != "update.nova_relay_update" || reason != "" {
		t.Fatalf("unchanged provenance: candidate %+v, reason %q", candidate, reason)
	}
}
