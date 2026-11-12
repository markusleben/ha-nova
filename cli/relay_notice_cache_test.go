package main

import (
	"encoding/json"
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

// quietAppCheckSetup: a legacy-token install whose NOVA App reports
// relayVersion on /health and, unless registryFails, its one registry-proven
// update entity, with a fresh release cache. onHealth runs on every /health.
// It returns the count of App checks (/health requests).
func quietAppCheckSetup(t *testing.T, relayVersion string, registryFails bool, onHealth func()) (runtimePaths, *atomic.Int32) {
	t.Helper()
	paths, cfg := doctorTestSetup(t)
	var checks atomic.Int32
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			checks.Add(1)
			if onHealth != nil {
				onHealth()
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"version":"` + relayVersion + `"}}`))
		case "/ws":
			if registryFails {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			_, _ = w.Write(registryEnvelope(t, []map[string]interface{}{novaRegistryEntry("update.nova_relay_update")}))
		case "/core":
			var req struct {
				Path string `json:"path"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Path != "/api/states" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(statesEnvelope(t, []map[string]interface{}{relayUpdateEntity("update.nova_relay_update", "NOVA Relay")}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(relay.Close)
	cfg.RelayBaseURL = relay.URL
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	writeQuietCheckVersion(t, paths, "0.20.0")
	if err := writeJSONFile(paths.UpdateCacheFile, releaseInfo{Version: "0.20.0", HTMLURL: "https://example.invalid/releases/v0.20.0"}, 0o644); err != nil {
		t.Fatalf("write update cache: %v", err)
	}
	return paths, &checks
}

func writeQuietCheckVersion(t *testing.T, paths runtimePaths, version string) {
	t.Helper()
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"`+version+`","min_relay_version":"0.4.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}
}

func runQuietCheckUpdate(t *testing.T, paths runtimePaths) string {
	t.Helper()
	exit, output := captureCommandOutput(t, func() int { return runCheckUpdate(paths, []string{"--quiet"}) })
	if exit != 0 {
		t.Fatalf("quiet check-update exit %d:\n%s", exit, output)
	}
	return output
}

func TestQuietCheckUpdateSkipsASilentAppCheckWithinTheHour(t *testing.T) {
	paths, checks := quietAppCheckSetup(t, "9.9.9", false, nil)
	want := int32(0)
	expect := func(step string, again bool) {
		t.Helper()
		runQuietCheckUpdate(t, paths)
		if again {
			want++
		}
		if got := checks.Load(); got != want {
			t.Fatalf("%s: %d App checks, want %d", step, got, want)
		}
	}

	expect("first quiet check", true)
	expect("a silent App check within the hour", false)

	writeQuietCheckVersion(t, paths, "0.21.0")
	expect("a new HA NOVA version", true)

	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}
	cfg.HAURL += "/"
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	expect("a changed config", true)

	marker := filepath.Join(paths.CacheDir, relayNoticeSilentMarkerName)
	for _, at := range []time.Time{time.Now().Add(-relayNoticeSilentTTL - time.Minute), time.Now().Add(time.Hour)} {
		if err := os.Chtimes(marker, at, at); err != nil {
			t.Fatalf("set marker time: %v", err)
		}
		expect("a marker past the hour or from the future", true)
	}

	if err := os.Remove(marker); err != nil {
		t.Fatalf("remove marker: %v", err)
	}
	t.Setenv(serverSelectionEnvVar, "other")
	runQuietCheckUpdate(t, paths)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("another server's result must not be remembered as the default's")
	}
}

func TestQuietCheckUpdateNeverRemembersAFailedAppCheck(t *testing.T) {
	paths, checks := quietAppCheckSetup(t, "9.9.9", true, nil)
	for run := int32(1); run <= 2; run++ {
		runQuietCheckUpdate(t, paths)
		if got := checks.Load(); got != run {
			t.Fatalf("run %d: an App check that could not read the registry must run again: %d App checks", run, got)
		}
	}
	if _, err := os.Stat(filepath.Join(paths.CacheDir, relayNoticeSilentMarkerName)); err == nil {
		t.Fatal("a failed App check must not be remembered")
	}
}

func TestQuietCheckUpdateRechecksAnAppNoticeEverySession(t *testing.T) {
	paths, checks := quietAppCheckSetup(t, "0.1.0", false, nil)

	for run := int32(1); run <= 2; run++ {
		output := runQuietCheckUpdate(t, paths)
		if !strings.Contains(output, "The NOVA App in Home Assistant needs an update") {
			t.Fatalf("run %d: App below the floor not named:\n%s", run, output)
		}
		if got := checks.Load(); got != run {
			t.Fatalf("run %d: %d App checks, want %d", run, got, run)
		}
	}
}

// Each lookup waits for the other to start: run one after the other, the
// first gives up waiting and the test fails.
func TestQuietCheckUpdateChecksTheAppWhileTheReleaseLookupRuns(t *testing.T) {
	appStarted, releaseStarted := make(chan struct{}), make(chan struct{})
	var appSawRelease, releaseSawApp atomic.Bool
	meet := func(mine chan struct{}, other <-chan struct{}, saw *atomic.Bool) {
		close(mine)
		select {
		case <-other:
			saw.Store(true)
		case <-time.After(2 * time.Second):
		}
	}
	paths, _ := quietAppCheckSetup(t, "9.9.9", false, func() { meet(appStarted, releaseStarted, &appSawRelease) })
	stale := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(paths.UpdateCacheFile, stale, stale); err != nil {
		t.Fatalf("age release cache: %v", err)
	}
	originalHTTPClient := httpClient
	t.Cleanup(func() { httpClient = originalHTTPClient })
	httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == latestReleaseURL {
			meet(releaseStarted, appStarted, &releaseSawApp)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"tag_name":"v0.20.0","html_url":"https://example.invalid/releases/v0.20.0"}`)),
				Header:     make(http.Header),
			}, nil
		}
		return http.DefaultTransport.RoundTrip(req)
	})}

	runQuietCheckUpdate(t, paths)
	if !appSawRelease.Load() || !releaseSawApp.Load() {
		t.Fatalf("the App check and the release lookup did not overlap (app saw release: %v, release saw app: %v)", appSawRelease.Load(), releaseSawApp.Load())
	}
}
