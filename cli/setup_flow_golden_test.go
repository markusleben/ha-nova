package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Golden transcripts of the text setup path. They pin today's terminal UX
// across the setupFlow engine/renderer split: a refactor must keep them
// byte-identical. Regenerate on purpose only:
//
//	HA_NOVA_UPDATE_GOLDEN=1 go test -run TestSetupGolden ./...
var setupGoldenVolatile = []struct {
	pattern *regexp.Regexp
	repl    string
}{
	{regexp.MustCompile(`\b[a-f0-9]{64}\b`), "<token>"},
	{regexp.MustCompile(`127\.0\.0\.1:\d+`), "127.0.0.1:<port>"},
}

// literals are ordered (from, to) pairs; longer literals go first.
func assertSetupGolden(t *testing.T, name, output string, literals ...string) {
	t.Helper()
	for i := 0; i+1 < len(literals); i += 2 {
		if literals[i] != "" {
			output = strings.ReplaceAll(output, literals[i], literals[i+1])
		}
	}
	for _, v := range setupGoldenVolatile {
		output = v.pattern.ReplaceAllString(output, v.repl)
	}
	path := filepath.Join("testdata", "setup-golden", name+".txt")
	if os.Getenv("HA_NOVA_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(output), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (regenerate with HA_NOVA_UPDATE_GOLDEN=1)", path, err)
	}
	if string(want) != output {
		t.Fatalf("setup transcript %s changed.\n--- want\n%s\n--- got\n%s", name, want, output)
	}
}

func setupGoldenHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))
	return home
}

func setupGoldenHAServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)
	return server
}

func setupGoldenRelayServer(t *testing.T, pairedToken string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pair":
			_, _ = w.Write([]byte(`{"ok":true,"data":{"relay_token":"` + pairedToken + `"}}`))
		case "/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
		case "/ws":
			var payload struct {
				Type string `json:"type"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload.Type != "ping" {
				http.Error(w, "first look not served", http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"type":"pong"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestSetupGoldenFreshLocal(t *testing.T) {
	home := setupGoldenHome(t)
	haServer := setupGoldenHAServer(t)
	relayServer := setupGoldenRelayServer(t, "unused")
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	input := joinSetupInputs(
		[]string{"4", haServer.URL},
		setupWizardRelayInstallPrompts(),
		setupWizardGenerateRelayTokenPrompts(),
	)
	code := -1
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		code = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "", "", "", relayServer.URL, "", false)
		return code
	})
	if code != 0 {
		t.Fatalf("exit = %d\n%s%s", code, stdout, stderr)
	}
	assertSetupGolden(t, "fresh-local", "exit 0\n--- stdout\n"+stdout+"--- stderr\n"+stderr,
		haServer.URL, "<ha-url>", relayServer.URL, "<relay-url>", home, "<home>",
	)
}

func TestSetupGoldenCodePath(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"codex": true})
	home := setupGoldenHome(t)
	haServer := setupGoldenHAServer(t)
	relayServer := setupGoldenRelayServer(t, "paired-token-never-render")
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	code := -1
	stdout, stderr := captureInteractiveSetupIO(t, joinSetupInputs(setupWizardPairingPrompts("123 456")), func() int {
		code = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "codex",
			normalizeHostInput(haServer.URL), haServer.URL, relayServer.URL, "", false)
		return code
	})
	if code != 0 {
		t.Fatalf("exit = %d\n%s%s", code, stdout, stderr)
	}
	assertSetupGolden(t, "code-path", "exit 0\n--- stdout\n"+stdout+"--- stderr\n"+stderr,
		haServer.URL, "<ha-url>", relayServer.URL, "<relay-url>", home, "<home>",
		normalizeHostInput(haServer.URL), "<ha-host>",
	)
}

func TestSetupGoldenAlreadyInstalled(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"claude": true})
	withClientAttachmentPresence(t, map[string]bool{"claude": true})
	home := setupGoldenHome(t)
	previousExecutable := executablePathForInstallSource
	executablePathForInstallSource = func() (string, error) { return filepath.Join(home, "ha-nova"), nil }
	t.Cleanup(func() { executablePathForInstallSource = previousExecutable })
	relayServer := setupGoldenRelayServer(t, "unused")
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg := runtimeConfig{HAHost: "192.168.1.5", HAURL: "http://192.168.1.5:8123", RelayBaseURL: relayServer.URL}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatal(err)
	}
	state := loadStateOrDefault(paths)
	mergeStateClients(&state, []string{"claude"})
	if err := saveState(paths, state); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeInstalledClaudePluginFixture(t, home)
	writeClaudeMarketplaceRegistrationFixture(t, home, filepath.Join(paths.ConfigDir, "claude-marketplace"))
	previousProbe := probePairingV1ForSetup // its App can pair: the switch is named, nobody to ask here
	probePairingV1ForSetup = func(string) bool { return true }
	t.Cleanup(func() { probePairingV1ForSetup = previousProbe })
	code := -1
	stdout, stderr := captureInteractiveSetupIO(t, "\n", func() int {
		code = interactiveSetup(paths, cfg, state, "claude", "", "", "", "", false)
		return code
	})
	if code != 0 {
		t.Fatalf("exit = %d\n%s%s", code, stdout, stderr)
	}
	assertSetupGolden(t, "already-installed", "exit 0\n--- stdout\n"+stdout+"--- stderr\n"+stderr,
		relayServer.URL, "<relay-url>", home, "<home>",
	)
}

func TestSetupGoldenLinkOnly(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	installCloudCommandCoordinator(t, newSelectingCloudCoordinator())
	code := -1
	stdout, stderr := captureInteractiveSetupIO(t, "3\nhttp://unsafe.example\nexit\n", func() int {
		code = interactiveSetup(paths, runtimeConfig{}, installState{}, "claude", "", "", "", "", false)
		return code
	})
	assertSetupGolden(t, "link-only", "exit "+strconv.Itoa(code)+"\n--- stdout\n"+stdout+"--- stderr\n"+stderr,
		os.Getenv("HOME"), "<home>",
	)
}

func TestSetupGoldenNonInteractive(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"antigravity": true})
	home := setupGoldenHome(t)
	haServer := setupGoldenHAServer(t)
	relayServer := setupGoldenRelayServer(t, "unused")
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	code, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{
			"antigravity",
			"--host", normalizeHostInput(haServer.URL),
			"--ha-url", haServer.URL,
			"--relay-url", relayServer.URL,
			"--relay-token", "test-relay-token",
			"--non-interactive",
		})
	})
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, output)
	}
	assertSetupGolden(t, "non-interactive", "exit 0\n--- output\n"+output,
		haServer.URL, "<ha-url>", relayServer.URL, "<relay-url>", home, "<home>",
		normalizeHostInput(haServer.URL), "<ha-host>",
	)
}

func TestSetupGoldenFirstRunIntroExit(t *testing.T) {
	home := setupGoldenHome(t)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	code := -1
	stdout, stderr := captureInteractiveSetupIO(t, "exit\n", func() int {
		code = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "", "", "", "", "", false)
		return code
	})
	assertSetupGolden(t, "first-run-intro-exit", "exit "+strconv.Itoa(code)+"\n--- stdout\n"+stdout+"--- stderr\n"+stderr,
		home, "<home>",
	)
}
