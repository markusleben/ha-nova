package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeDemo serves connect.json and the relay health like demo.hanova.app.
// relayURL "" means "this server's /relay".
func fakeDemo(t *testing.T, relayURL string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/connect.json":
			url := relayURL
			if url == "" {
				url = srv.URL + "/relay"
			}
			_, _ = w.Write([]byte(`{"ha_url":"` + srv.URL + `","relay_url":"` + url + `","relay_token":"demo-public-token"}`))
		case "/relay/health":
			if r.Header.Get("Authorization") != "Bearer demo-public-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"status":"ok"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	prev := demoConnectURL
	demoConnectURL = srv.URL + "/connect.json"
	t.Cleanup(func() { demoConnectURL = prev })
	return srv
}

func tryTestPaths(t *testing.T, config string) runtimePaths {
	t.Helper()
	resetServerProfileSelection(t)
	t.Cleanup(func() { activeDemo = false })
	t.Setenv("HOME", t.TempDir())
	if config == "" {
		dir := t.TempDir()
		return runtimePaths{ConfigDir: dir, ConfigFile: filepath.Join(dir, "config.json")}
	}
	return writeTestConfigFile(t, config)
}

func runTry(t *testing.T, paths runtimePaths, args ...string) (int, string) {
	t.Helper()
	exit := 0
	out := captureStdout(t, func() {
		errOut := captureStderr(t, func() { exit = runTryCommand(paths, args) })
		os.Stdout.WriteString(errOut)
	})
	return exit, out
}

func TestTryConnectsDemoOutsideConfigAndNeverAsDefault(t *testing.T) {
	fakeDemo(t, "")
	paths := tryTestPaths(t, "")

	exit, out := runTry(t, paths)
	if exit != 0 || !strings.Contains(out, "Demo house connected") || !strings.Contains(out, "ha-nova try --stop") {
		t.Fatalf("try exit=%d out=%s", exit, out)
	}
	if _, err := os.Stat(paths.ConfigFile); !os.IsNotExist(err) {
		t.Fatalf("try must not create config.json: %v", err)
	}
	info, err := os.Stat(demoProfilePath(paths))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("demo.json: %v %v", info, err)
	}

	// No selection: never the demo, but a hint that names it.
	_, err = loadConfig(paths)
	if err == nil || !strings.Contains(err.Error(), "--server demo") || activeServerIsDemo() {
		t.Fatalf("default load = %v (demo active %v)", err, activeServerIsDemo())
	}

	t.Setenv(serverSelectionEnvVar, demoProfileName)
	cfg, err := loadConfig(paths)
	if err != nil || !activeServerIsDemo() || cfg.demoToken != "demo-public-token" {
		t.Fatalf("demo load = %+v, %v", cfg, err)
	}
	base, _, token, device, err := relayFunctionalTransport(cfg)
	if err != nil || !device || token != "demo-public-token" || !strings.HasSuffix(base, "/relay") {
		t.Fatalf("demo transport = %q %q %v %v", base, token, device, err)
	}
	if got := verbNotices(paths, "0.1.0"); len(got) == 0 || got[0].Kind != "demo" {
		t.Fatalf("demo verbs must carry the demo notice: %+v", got)
	}
	if _, _, _, ok := relayNoticeTransport(cfg); ok {
		t.Fatal("the demo relay must get no update notices")
	}
}

func TestTryLeavesRealHomeUntouched(t *testing.T) {
	fakeDemo(t, "")
	paths := tryTestPaths(t, testV2TwoProfileConfig)
	before, _ := os.ReadFile(paths.ConfigFile)

	if exit, out := runTry(t, paths); exit != 0 {
		t.Fatalf("try exit=%d out=%s", exit, out)
	}
	after, _ := os.ReadFile(paths.ConfigFile)
	if string(before) != string(after) {
		t.Fatal("try changed config.json")
	}
	cfg, err := loadConfig(paths)
	if err != nil || activeServerProfile() != defaultServerProfileName || activeServerIsDemo() || cfg.demoToken != "" {
		t.Fatalf("default must stay the real home: %+v %v", cfg, err)
	}
	if got := verbNotices(paths, ""); len(got) == 0 || got[0].Kind != "home" {
		t.Fatalf("home verbs must say they are not the demo while it is connected: %+v", got)
	}

	if exit, out := runTry(t, paths, "--stop"); exit != 0 || !strings.Contains(out, "Demo house disconnected") {
		t.Fatalf("stop exit=%d out=%s", exit, out)
	}
	if _, err := os.Stat(demoProfilePath(paths)); !os.IsNotExist(err) {
		t.Fatalf("stop left demo.json: %v", err)
	}
	after, _ = os.ReadFile(paths.ConfigFile)
	if string(before) != string(after) {
		t.Fatal("try --stop changed config.json")
	}
	if exit, out := runTry(t, paths, "--stop"); exit != 0 || !strings.Contains(out, "was not connected") {
		t.Fatalf("second stop exit=%d out=%s", exit, out)
	}
}

func TestTryUnreachableOrForeignRelaySavesNothing(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T){
		"unreachable": func(t *testing.T) {
			srv := fakeDemo(t, "")
			srv.Close()
		},
		"foreign relay": func(t *testing.T) { fakeDemo(t, "https://evil.example/relay") },
	} {
		t.Run(name, func(t *testing.T) {
			setup(t)
			paths := tryTestPaths(t, "")
			exit, out := runTry(t, paths)
			if exit != 1 || !strings.Contains(out, demoUnreachableText) || strings.Count(strings.TrimSpace(out), "\n") != 0 {
				t.Fatalf("exit=%d, want one plain line: %q", exit, out)
			}
			if _, err := os.Stat(demoProfilePath(paths)); !os.IsNotExist(err) {
				t.Fatalf("nothing may be saved: %v", err)
			}
		})
	}
}

func TestDemoNameIsReservedButExistingDemoServerKeepsWorking(t *testing.T) {
	paths := tryTestPaths(t, testV2TwoProfileConfig)
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(serverSelectionEnvVar, demoProfileName)
	if _, err := saveTargetProfileName(doc); !errors.Is(err, errDemoNameReserved) {
		t.Fatalf("creating a demo profile: %v", err)
	}
	if !demoSelected(paths, []string{"--non-interactive"}) {
		t.Fatal("setup with HA_NOVA_SERVER=demo must be refused")
	}
	t.Setenv(serverSelectionEnvVar, "")
	for _, args := range [][]string{{"--server", "demo"}, {"--server=demo"}, {"-server", "demo"}} {
		if !demoSelected(paths, args) {
			t.Fatalf("%v must be refused", args)
		}
	}
	if demoSelected(paths, []string{"--server", "cabin"}) {
		t.Fatal("other servers stay allowed")
	}
	t.Setenv(serverSelectionEnvVar, demoProfileName)
	if demoSelected(paths, []string{"--server=default"}) || demoSelected(paths, []string{"--server", "cabin"}) {
		t.Fatal("an explicit --server beats HA_NOVA_SERVER=demo")
	}
	if _, err := loadConfig(paths); err == nil || !strings.Contains(err.Error(), "ha-nova try") {
		t.Fatalf("demo selected but not connected: %v", err)
	}
	t.Setenv(serverSelectionEnvVar, "")

	// A real server already called "demo" keeps its name and its config;
	// try refuses instead of shadowing it.
	fakeDemo(t, "")
	legacy := tryTestPaths(t, `{"schema_version":2,"default_server":"default","servers":{"default":{"relay_base_url":"http://ha:8791"},"demo":{"relay_base_url":"http://cabin:8791"}}}`)
	doc, _ = loadConfigDocument(legacy.ConfigFile)
	t.Setenv(serverSelectionEnvVar, demoProfileName)
	if _, err := saveTargetProfileName(doc); err != nil {
		t.Fatalf("existing demo profile: %v", err)
	}
	if demoSelected(legacy, nil) {
		t.Fatal("an existing real demo server must not be refused")
	}
	if exit, out := runTry(t, legacy); exit != 1 || !strings.Contains(out, "server rename demo") {
		t.Fatalf("try over a real demo server: exit=%d out=%s", exit, out)
	}
}

func TestUninstallRemovesDemoConnection(t *testing.T) {
	paths := runtimePaths{ConfigDir: "/cfg"}
	found := false
	for _, path := range managedConfigArtifactPaths(paths, false) {
		found = found || path == demoProfilePath(paths)
	}
	if !found {
		t.Fatal("uninstall must remove demo.json")
	}
}

func TestTrySaveFailureIsReported(t *testing.T) {
	fakeDemo(t, "")
	paths := tryTestPaths(t, "")
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	paths.ConfigDir = blocker
	if exit, out := runTry(t, paths); exit != 1 || !strings.Contains(out, "cannot save the demo house connection") {
		t.Fatalf("exit=%d out=%s", exit, out)
	}
}

func TestDemoNeverRevertsToAnUntaggedHomeSnapshot(t *testing.T) {
	paths := tryTestPaths(t, "")
	stack := undoSnapshotStack{SchemaVersion: undoSnapshotStackSchemaVersion, Snapshots: []undoSnapshot{{
		SchemaVersion: undoSnapshotSchemaVersion, Op: "update", Domain: "automation", TargetID: "kitchen",
		BeforeConfig: json.RawMessage(`{}`), ExpectedAfter: json.RawMessage(`{}`),
	}}}
	if err := writeJSONFile(undoSnapshotStackPath(paths), stack, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := selectServerUndoSnapshot(paths, defaultServerProfileName, "kitchen", "automation"); err != nil {
		t.Fatalf("a real home keeps its untagged records: %v", err)
	}
	setActiveDemoProfile()
	if _, err := selectServerUndoSnapshot(paths, demoProfileName, "kitchen", "automation"); err == nil {
		t.Fatal("the demo house must not pick an untagged record")
	}
}

func TestDemoCheckpointsNeverEvictTheHomes(t *testing.T) {
	paths := tryTestPaths(t, "")
	save := func(server, target string) {
		r := `{"op":"update","domain":"automation","target_id":"` + target + `","server":"` + server + `","before_config":{"a":1},"expected_after":{"a":2}}`
		if _, err := saveUndoSnapshotBytes(paths, []byte(r)); err != nil {
			t.Fatal(err)
		}
	}
	save(defaultServerProfileName, "home")
	for i := 0; i < undoSnapshotStackLimit+2; i++ {
		save(demoProfileName, "d"+string(rune('a'+i)))
	}
	if _, err := selectServerUndoSnapshot(paths, defaultServerProfileName, "home", "automation"); err != nil {
		t.Fatalf("demo saves evicted the home's checkpoint: %v", err)
	}
	stack, _ := loadUndoSnapshotStack(paths)
	if len(stack.Snapshots) != undoSnapshotStackLimit+1 {
		t.Fatalf("want %d demo + 1 home records, got %d", undoSnapshotStackLimit, len(stack.Snapshots))
	}
}

func TestTryRefusesARelayThatIsNotHealthy(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/connect.json" {
			_, _ = w.Write([]byte(`{"relay_url":"` + srv.URL + `/relay","relay_token":"t"}`))
			return
		}
		_, _ = w.Write([]byte(`<html>landing page</html>`)) // HTTP 200, but no Relay
	}))
	t.Cleanup(srv.Close)
	prev := demoConnectURL
	demoConnectURL = srv.URL + "/connect.json"
	t.Cleanup(func() { demoConnectURL = prev })
	paths := tryTestPaths(t, "")
	if exit, out := runTry(t, paths); exit != 1 || !strings.Contains(out, demoUnreachableText) {
		t.Fatalf("exit=%d out=%s", exit, out)
	}
	if _, err := os.Stat(demoProfilePath(paths)); !os.IsNotExist(err) {
		t.Fatalf("nothing may be saved: %v", err)
	}
}

func TestTryAfterUninstallKeepingTheHomeAsksForSetup(t *testing.T) {
	fakeDemo(t, "")
	paths := tryTestPaths(t, testV2TwoProfileConfig)
	if err := markInstallLifecycleStopped(paths); err != nil {
		t.Fatal(err)
	}
	if exit, out := runTry(t, paths); exit != 1 || !strings.Contains(out, "Run ha-nova setup first") {
		t.Fatalf("exit=%d out=%s", exit, out)
	}
	if _, err := os.Stat(demoProfilePath(paths)); !os.IsNotExist(err) {
		t.Fatalf("nothing may be saved: %v", err)
	}
}

func TestDemoResetIsOnePlainLine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"demo_resetting"}`))
	}))
	t.Cleanup(srv.Close)
	if _, err := demoHTTPClient().Get(srv.URL); err == nil || !strings.Contains(err.Error(), demoUnreachableText) {
		t.Fatalf("503 must read as the demo reset: %v", err)
	}
}

// Lane E7: the text names the AI apps that have HA NOVA, not a guess.
func TestTryNamesTheConfiguredAIApp(t *testing.T) {
	fakeDemo(t, "")
	withClientRuntimeAvailability(t, map[string]bool{"opencode": true})
	paths := tryTestPaths(t, "")
	paths.StateFile = filepath.Join(paths.ConfigDir, "state.json")
	state := defaultInstallState()
	state.InstalledClients = []string{"opencode"}
	if err := saveState(paths, state); err != nil {
		t.Fatal(err)
	}
	exit, out := runTry(t, paths)
	if exit != 0 || !strings.Contains(out, "Open OpenCode in a new chat") || strings.Contains(out, "Claude Code") {
		t.Fatalf("try exit=%d out=%s", exit, out)
	}
	// The whole text is pinned: each prompt's premise is checked against the
	// seeded house (bed light references, kitchen lights on, the entrance
	// door asks first), so a change here needs that check again.
	want := `
Demo house connected. It is a shared demo home, not yours: it resets every hour.
Never type real names or passwords there. Your own home is not touched.

Open OpenCode in a new chat. Say these, one at a time:
  1. "In the demo house, where is the bed light used?"
  2. "In the demo house, turn off the kitchen lights."
  3. "In the demo house, unlock the entrance door."   It asks you first.
  4. "In the demo house, lock the entrance door every night at 10 pm."
  5. "Undo that."
You can ask in any language.

Done?            ha-nova try --stop
Your own home?   ha-nova setup
`
	if out != want {
		t.Fatalf("try output changed:\n%s", out)
	}
}

// 0.27 recheck lane: Claude Code's allow rule is `ha-nova apply --plan *`,
// which a HA_NOVA_SERVER=demo prefix breaks, so the skills run apply bare.
// That works only while apply takes its plan's server with no selection.
func TestApplyTakesTheDemoPlansServerWithoutASelection(t *testing.T) {
	fakeDemo(t, "")
	paths := tryTestPaths(t, "")
	paths.CacheDir = t.TempDir()
	if exit, out := runTry(t, paths); exit != 0 {
		t.Fatalf("try exit=%d out=%s", exit, out)
	}
	id := "p_0123456789ab"
	if err := os.MkdirAll(planDir(paths), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writePlanFile(filepath.Join(planDir(paths), id+".json"), storedPlan{ID: id, Server: demoProfileName}); err != nil {
		t.Fatal(err)
	}
	if env, _ := applyPlan(paths, verbOptions{}, id, "", false); env.Server != demoProfileName {
		t.Fatalf("apply without a selection must use the plan's demo server: %+v %+v", env, env.Error)
	}
}
