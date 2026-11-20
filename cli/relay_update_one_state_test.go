package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
