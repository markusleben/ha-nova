package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeRelayCall struct {
	endpoint string
	payload  map[string]any
}

// fakeRelayLog records every request the fake Relay received.
type fakeRelayLog struct {
	mu    sync.Mutex
	calls []fakeRelayCall
}

func (l *fakeRelayLog) all() []fakeRelayCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]fakeRelayCall(nil), l.calls...)
}

func (l *fakeRelayLog) wsTypes() []string {
	var types []string
	for _, c := range l.all() {
		if c.endpoint == "ws" {
			types = append(types, fmt.Sprint(c.payload["type"]))
		}
	}
	return types
}

func (l *fakeRelayLog) corePaths() []string {
	var paths []string
	for _, c := range l.all() {
		if c.endpoint == "core" {
			paths = append(paths, fmt.Sprint(c.payload["path"]))
		}
	}
	return paths
}

// coreReply is the Relay /core data shape: upstream status plus body.
func coreReply(status int, body any) any {
	return map[string]any{"status": status, "body": body}
}

// setupVerbRelayTest mirrors setupTraceRelayTest for /ws and /core. reply
// returns the envelope `data`; every request is logged before replying.
func setupVerbRelayTest(t *testing.T, reply func(r *http.Request, endpoint string, payload map[string]any) any) (runtimePaths, *fakeRelayLog) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".test-relay-token"))
	t.Setenv(updateNudgeOptOutEnv, "1")

	log := &fakeRelayLog{}
	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint := strings.TrimPrefix(r.URL.Path, "/")
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		log.mu.Lock()
		log.calls = append(log.calls, fakeRelayCall{endpoint: endpoint, payload: payload})
		log.mu.Unlock()
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": reply(r, endpoint, payload)})
	}))
	t.Cleanup(relayServer.Close)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := saveConfig(paths, runtimeConfig{HAHost: "127.0.0.1", HAURL: "http://127.0.0.1:8123", RelayBaseURL: relayServer.URL}); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := writeRelayAuthToken("test-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}
	return paths, log
}

type verbTestEnvelope struct {
	verbEnvelope
	Result json.RawMessage `json:"result"`
}

func runVerbForTest(t *testing.T, run func() int) (int, verbTestEnvelope) {
	t.Helper()
	exit := 0
	out := captureStdout(t, func() { exit = run() })
	var env verbTestEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not one JSON envelope: %v\n%s", err, out)
	}
	if env.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want 1", env.SchemaVersion)
	}
	return exit, env
}

// registryReply serves the area, device, compact, and full registries.
func registryReply(compact []map[string]any, full []map[string]any) func(*http.Request, string, map[string]any) any {
	return func(_ *http.Request, endpoint string, payload map[string]any) any {
		switch payload["type"] {
		case "config/area_registry/list":
			return []map[string]any{{"area_id": "kuche", "name": "Küche"}, {"area_id": "office", "name": "Office"}}
		case "config/device_registry/list":
			return []map[string]any{{"id": "dev1", "area_id": "kuche"}}
		case "config/entity_registry/list_for_display":
			return map[string]any{"entities": compact}
		case "config/entity_registry/list":
			return full
		}
		return nil
	}
}

func TestFindReportsExactTruncationCounts(t *testing.T) {
	compact := []map[string]any{{"ei": "switch.office_fan", "en": "Fan"}}
	for i := 25; i >= 1; i-- {
		compact = append(compact, map[string]any{"ei": fmt.Sprintf("light.kitchen_%02d", i), "en": "Spot"})
	}
	paths, log := setupVerbRelayTest(t, registryReply(compact, nil))

	exit, env := runVerbForTest(t, func() int { return runFindCommand(paths, []string{"kitchen", "--limit", "5", "--domain", "light"}) })
	if exit != 0 {
		t.Fatalf("exit = %d, env %+v", exit, env)
	}
	var result findResult
	_ = json.Unmarshal(env.Result, &result)
	if result.Total != 25 || result.Returned != 5 || len(result.Matches) != 5 {
		t.Fatalf("total/returned = %d/%d (%d rows), want 25/5", result.Total, result.Returned, len(result.Matches))
	}
	if result.Matches[0].EntityID != "light.kitchen_01" || result.Matches[4].EntityID != "light.kitchen_05" {
		t.Fatalf("matches not sorted by entity_id: %+v", result.Matches)
	}
	for _, wsType := range log.wsTypes() {
		if wsType == "config/entity_registry/list" {
			t.Fatal("full registry read although the compact registry matched")
		}
	}
}

func TestFindReadsAliasesOnlyAfterCompactMiss(t *testing.T) {
	compact := []map[string]any{{"ei": "light.floor_lamp_living", "en": "Floor lamp"}}
	full := []map[string]any{{"entity_id": "light.floor_lamp_living", "name": nil, "original_name": "Floor lamp", "aliases": []any{nil, "Leselampe"}}}
	paths, log := setupVerbRelayTest(t, registryReply(compact, full))

	exit, env := runVerbForTest(t, func() int { return runFindCommand(paths, []string{"Leselampe"}) })
	var result findResult
	_ = json.Unmarshal(env.Result, &result)
	if exit != 0 || result.Total != 1 || result.Matches[0].By != "alias" {
		t.Fatalf("exit %d, result %+v", exit, result)
	}
	got := strings.Join(log.wsTypes(), ",")
	if !strings.HasSuffix(got, "config/entity_registry/list_for_display,config/entity_registry/list") {
		t.Fatalf("ws order = %s, want compact before full", got)
	}
}

func TestFindFoldsCaseAndDiacriticsAcrossDeviceArea(t *testing.T) {
	compact := []map[string]any{{"ei": "light.ceiling", "en": "Decke", "di": "dev1"}, {"ei": "light.cafe_bar", "en": "Café"}}
	paths, _ := setupVerbRelayTest(t, registryReply(compact, nil))

	cases := map[string]string{"kuche decke": "light.ceiling", "KUECHE": "light.ceiling", "Küche": "light.ceiling", "cafe": "light.cafe_bar", "CAFÉ bar": "light.cafe_bar"}
	for phrase, want := range cases {
		exit, env := runVerbForTest(t, func() int { return runFindCommand(paths, []string{phrase, "--area", areaFor(want)}) })
		var result findResult
		_ = json.Unmarshal(env.Result, &result)
		if exit != 0 || result.Total != 1 || result.Matches[0].EntityID != want {
			t.Fatalf("find %q: exit %d, result %+v, want %s", phrase, exit, result, want)
		}
	}
}

func areaFor(entityID string) string {
	if entityID == "light.ceiling" {
		return "kuche" // inherited from the device, not set on the entity
	}
	return ""
}

func TestFindExcludesDisabledUnlessRequested(t *testing.T) {
	full := []map[string]any{{"entity_id": "light.old_lamp", "name": "Old lamp", "disabled_by": "user"}}
	paths, _ := setupVerbRelayTest(t, registryReply(nil, full))

	exit, env := runVerbForTest(t, func() int { return runFindCommand(paths, []string{"old lamp"}) })
	if exit != verbExitNotFound || env.Error == nil || env.Error.Code != "NOT_FOUND" || env.Status != "error" {
		t.Fatalf("disabled entity found without --include-disabled: exit %d %+v", exit, env)
	}
	exit, env = runVerbForTest(t, func() int { return runFindCommand(paths, []string{"old lamp", "--include-disabled"}) })
	var result findResult
	_ = json.Unmarshal(env.Result, &result)
	if exit != 0 || result.Total != 1 || !result.Matches[0].Disabled {
		t.Fatalf("--include-disabled: exit %d, result %+v", exit, result)
	}
}

func TestRelaySessionRefusesNonReadRequestsBeforeSending(t *testing.T) {
	paths, log := setupVerbRelayTest(t, func(*http.Request, string, map[string]any) any { return nil })
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		t.Fatalf("newRelaySession: %v", err)
	}
	defer s.close()
	refused := []struct {
		endpoint string
		payload  map[string]any
	}{
		{"core", map[string]any{"method": "POST", "path": "/api/services/light/turn_off"}},
		{"core", map[string]any{"method": "DELETE", "path": "/api/config/automation/config/1"}},
		{"core", map[string]any{"method": "GET", "path": "/api/states", "body": map[string]any{}}},
		{"core", map[string]any{"method": "GET", "path": "/health"}},
		{"ws", map[string]any{"type": "call_service", "domain": "light", "service": "turn_off"}},
		{"ws", map[string]any{"type": "config/entity_registry/update", "entity_id": "light.x"}},
		{"ws", map[string]any{"type": "execute_script"}},
		{"files", map[string]any{"op": "read"}},
		{"backups", map[string]any{"op": "list"}},
	}
	for _, r := range refused {
		if _, err := s.read(r.endpoint, r.payload); err == nil || !strings.Contains(err.Error(), "nothing was sent") {
			t.Fatalf("%s %v was not refused: %v", r.endpoint, r.payload, err)
		}
	}
	if calls := log.all(); len(calls) != 0 {
		t.Fatalf("refused requests reached the Relay: %+v", calls)
	}
}

func TestRelaySessionSendsIdenticalReadsOnce(t *testing.T) {
	paths, log := setupVerbRelayTest(t, func(_ *http.Request, endpoint string, _ map[string]any) any {
		if endpoint == "core" {
			return coreReply(200, map[string]any{"state": "on"})
		}
		return map[string]any{"entities": []any{}}
	})
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		t.Fatalf("newRelaySession: %v", err)
	}
	defer s.close()
	var wg sync.WaitGroup
	for i := 0; i < 2*relaySessionMaxConcurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.ws(map[string]any{"type": "config/entity_registry/list_for_display"}); err != nil {
				t.Errorf("ws: %v", err)
			}
			if _, _, err := s.coreGET("/api/states/light.x"); err != nil {
				t.Errorf("core: %v", err)
			}
		}()
	}
	wg.Wait()
	if calls := log.all(); len(calls) != 2 {
		t.Fatalf("relay saw %d requests, want 2 (one per distinct read): %+v", len(calls), calls)
	}
}

func TestRelaySessionBoundsEachRequest(t *testing.T) {
	previous := relaySessionRequestTimeout
	relaySessionRequestTimeout = 50 * time.Millisecond
	t.Cleanup(func() { relaySessionRequestTimeout = previous })
	paths, _ := setupVerbRelayTest(t, func(r *http.Request, _ string, _ map[string]any) any {
		<-r.Context().Done() // hang until the client gives up
		return nil
	})
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		t.Fatalf("newRelaySession: %v", err)
	}
	defer s.close()
	if _, err := s.ws(map[string]any{"type": "config/area_registry/list"}); err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("hung read did not time out: %v", err)
	}
}
