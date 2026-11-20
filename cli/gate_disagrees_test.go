package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// GATE_DISAGREES names an App update only for an App older than
// min_relay_version, never for a current one or one whose version is
// unknown, and names a registry this computer couldn't read.
func TestGateDisagreesNamesAppUpdateOnlyWhenOlder(t *testing.T) {
	paths := runtimePaths{VersionFile: filepath.Join(t.TempDir(), "version.json")}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.34.0","min_relay_version":"0.13.30"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		version  string
		registry bool
		update   bool
	}{
		{"", false, false},
		{"0.13.30", false, false},
		{"0.14.0", true, false},
		{"not-a-version", false, false},
		{"0.13.29", false, true},
		{"0.13.29", true, true},
	} {
		var stop *verbFailure
		if !errors.As(gateDisagrees(paths, tc.version, false, tc.registry), &stop) || stop.code != "GATE_DISAGREES" || stop.exit != verbExitRefused || !errors.Is(stop.cause, errNothingSent) {
			t.Fatalf("%+v: %+v", tc, stop)
		}
		update := strings.Contains(stop.next, relayAppUpdatePath) && strings.Contains(stop.next, "it has "+tc.version)
		registry := strings.Contains(stop.message, "couldn't read a Home Assistant registry")
		if update != tc.update || registry != tc.registry || !strings.HasSuffix(stop.message, "; nothing was sent") || tc.update == strings.Contains(stop.next, "it again; if it happens again") {
			t.Fatalf("%+v: message %q next %q", tc, stop.message, stop.next)
		}
	}
}

// A save typed because the entity registry couldn't be read, which the
// App doesn't hold, sends nothing and says why this computer held it.
func TestGateDisagreesNamesUnreadRegistry(t *testing.T) {
	f := newFakeHA()
	f.ent("light.frame", "hue", "off", nil)
	f.storeConfig("/api/config/automation/config/frame_stairs", map[string]any{
		"alias": "Frame on stairs motion", "triggers": []any{}, "actions": []any{step("light.turn_on", "light.frame")},
	})
	f.fail["config/entity_registry/list_for_display"] = true
	g := &fakeGate{notNeeded: true}
	h := gatedHarness(t, f, g)
	draft := map[string]any{
		"alias":      "Frame on stairs motion",
		"triggers":   []any{map[string]any{"id": "stairs", "trigger": "state", "entity_id": "binary_sensor.stairs_motion", "to": "on"}},
		"conditions": []any{map[string]any{"condition": "template", "value_template": "{{ trigger.id == 'stairs' }}"}},
		"actions":    []any{step("light.turn_on", "light.frame")},
	}
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "frame_stairs", "--file", draftFile(t, draft))
	if env.Plan == nil || env.Tier != "typed" || !registryUnread(storedPlan{Reasons: env.Reasons}) {
		t.Fatalf("plan tier %s reasons %+v err %+v", env.Tier, env.Reasons, env.Error)
	}
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if _, _, sends := g.snapshot(); exit != verbExitRefused || applied.Error == nil || applied.Error.Code != "GATE_DISAGREES" || len(sends) != 0 || len(h.planFiles()) != 1 ||
		!strings.Contains(applied.Error.Message, "couldn't read a Home Assistant registry") || strings.Contains(applied.Error.Next, relayAppUpdatePath) {
		t.Fatalf("exit %d %+v sends %+v", exit, applied.Error, sends)
	}
}

// Through apply, the App's version comes from the ask's own answer: an
// older App is told to update, though apply asks before any other read.
func TestGateDisagreesThroughApplyNamesOlderApp(t *testing.T) {
	g := &fakeGate{notNeeded: true}
	h := gatedHarness(t, newFakeHA(), g)
	plan := h.plan("lock.unlock", "lock.front").Plan
	cfg, err := loadConfig(h.paths)
	if err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(cfg.RelayBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ModifyResponse = func(r *http.Response) error {
		r.Header.Set(relayVersionHeader, "0.13.29")
		return nil
	}
	server := httptest.NewServer(proxy)
	t.Cleanup(server.Close)
	if err := saveConfig(h.paths, runtimeConfig{HAHost: "127.0.0.1", HAURL: "http://127.0.0.1:8123", RelayBaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(h.paths.VersionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.paths.VersionFile, []byte(`{"skill_version":"0.34.0","min_relay_version":"0.13.30"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	exit, env := h.run(runApplyCommand, "--plan", plan.ID)
	if _, _, sends := g.snapshot(); exit != verbExitRefused || env.Error == nil || env.Error.Code != "GATE_DISAGREES" || len(sends) != 0 ||
		!strings.Contains(env.Error.Next, "it has 0.13.29") || !strings.Contains(env.Error.Next, relayAppUpdatePath) {
		t.Fatalf("exit %d %+v sends %+v", exit, env.Error, sends)
	}
}
