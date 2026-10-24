package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const pairInfoV1Body = `{"ok":true,"data":{"relay_version":"0.26.0","protocol_version":"v1","available":true}}`

// The real probe: only the relay's own v1 envelope counts. Anything else —
// another web server, an error page, a proxy, a closed port, a hang — must
// show the install steps.
func TestProbeInstalledRelayIsNotFooledByOtherServers(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"not found": http.NotFound,
		"html page": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<html>Home Assistant</html>"))
		},
		"not ok": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"ok":false,"data":{"protocol_version":"v1"}}`))
		},
		"other protocol": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"ok":true,"data":{"protocol_version":"v2"}}`))
		},
		"server error": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(pairInfoV1Body))
		},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			if running, _ := probeInstalledRelay(server.URL); running {
				t.Fatal("detected a running relay where none is")
			}
		})
	}

	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	if running, _ := probeInstalledRelay(closedURL); running {
		t.Fatal("detected a running relay on a closed port")
	}

	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pair/v1/info" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(pairInfoV1Body))
	}))
	defer relay.Close()
	if running, version := probeInstalledRelay(relay.URL); !running || version != "0.26.0" {
		t.Fatalf("probeInstalledRelay() = %v, %q", running, version)
	}
}

func TestProbeInstalledRelayIsBounded(t *testing.T) {
	original := installedRelayProbeTimeout
	installedRelayProbeTimeout = 100 * time.Millisecond
	t.Cleanup(func() { installedRelayProbeTimeout = original })
	release := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		_, _ = w.Write([]byte(pairInfoV1Body))
	}))
	defer hang.Close()
	defer close(release)
	start := time.Now()
	if running, _ := probeInstalledRelay(hang.URL); running {
		t.Fatal("a hanging server counted as a running relay")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("probe took %s, want it bounded by the timeout", elapsed)
	}
}

func stubInstalledRelayProbe(t *testing.T, running bool, version string) *[]string {
	t.Helper()
	probed := &[]string{}
	original := probeInstalledRelayForSetup
	probeInstalledRelayForSetup = func(relayURL string) (bool, string) {
		*probed = append(*probed, relayURL)
		return running, version
	}
	t.Cleanup(func() { probeInstalledRelayForSetup = original })
	return probed
}

// A --relay-url or a kept custom relay address may be another machine: never
// claim the App runs on the chosen Home Assistant from those.
func TestDetectInstalledRelayOnlyTrustsTheDerivedAddress(t *testing.T) {
	probed := stubInstalledRelayProbe(t, true, "")
	derived := applySelectedSetupHost(runtimeConfig{}, "192.168.1.5", "http://192.168.1.5:8123", "")
	if running, note := detectInstalledRelay(&strings.Builder{}, runtimePaths{}, derived, ""); !running ||
		note.plain() != "NOVA App is running" {
		t.Fatalf("derived address: running=%v note=%q", running, note)
	}
	if len(*probed) != 1 || (*probed)[0] != "http://192.168.1.5:8791" {
		t.Fatalf("probed %v, want the derived relay", *probed)
	}

	custom := derived
	custom.RelayBaseURL = "http://192.168.1.9:8791"
	if running, _ := detectInstalledRelay(&strings.Builder{}, runtimePaths{}, custom, ""); running {
		t.Fatal("a kept custom relay address skipped the install")
	}
	if running, _ := detectInstalledRelay(&strings.Builder{}, runtimePaths{}, derived, "http://192.168.1.5:8791"); running {
		t.Fatal("an explicit --relay-url skipped the install")
	}
	if running, _ := detectInstalledRelay(&strings.Builder{}, runtimePaths{}, runtimeConfig{}, ""); running {
		t.Fatal("no address skipped the install")
	}
	if len(*probed) != 1 {
		t.Fatalf("non-derived addresses must not be probed: %v", *probed)
	}
}

func TestDetectInstalledRelayHintsUpdateBelowMinimum(t *testing.T) {
	versionFile := filepath.Join(t.TempDir(), "version.json")
	if err := os.WriteFile(versionFile, []byte(`{"min_relay_version":"0.9.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := runtimePaths{VersionFile: versionFile}
	cfg := applySelectedSetupHost(runtimeConfig{}, "192.168.1.5", "http://192.168.1.5:8123", "")
	for version, wantHint := range map[string]bool{"0.8.0": true, "0.9.1": false, "1.0.0": false, "": false} {
		stubInstalledRelayProbe(t, true, version)
		_, note := detectInstalledRelay(&strings.Builder{}, paths, cfg, "")
		if got := strings.Contains(note.plain(), "update it to v0.9.1 or newer"); got != wantHint {
			t.Fatalf("relay %q: update hint = %v, want %v (note %q)", version, got, wantHint, note)
		}
	}
}

func newSetupDetectionHA(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))
	original := relayAuthTokenSetupPreflightForSetup
	t.Cleanup(func() { relayAuthTokenSetupPreflightForSetup = original })
	// Headless box: pairing is the only credential path, so the wizard
	// routes install -> pairing exactly like a fresh second computer.
	relayAuthTokenSetupPreflightForSetup = func() error {
		return desktopKeyringSessionUnavailableError("no session bus in this shell")
	}
	// The derived relay (:8791) is refused by the test dial guard; pairing
	// only needs to reach the code prompt.
	originalPairing := probePairingV1ForSetup
	probePairingV1ForSetup = func(string) bool { return true }
	t.Cleanup(func() { probePairingV1ForSetup = originalPairing })
	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(haServer.Close)
	return haServer.URL
}

// runSetupDetectionWizard drives a fresh computer: no saved address, the
// instance comes from discovery, so the wizard walks host -> install ->
// pairing like every extra computer does. The first input answers the
// headless service-mode question, the second keeps the found address.
func runSetupDetectionWizard(t *testing.T, haURL, relayURLFlag string, inputs ...string) (int, string) {
	t.Helper()
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, joinSetupInputs(append([]string{"n", "1"}, inputs...)), func() int {
		discoverReachableHAHostsForSetup = func(runtimeConfig) ([]setupDiscoveryCandidate, string) {
			return []setupDiscoveryCandidate{{Host: normalizeHostInput(haURL), HAURL: haURL, Source: "mDNS: Home"}}, ""
		}
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "codex", "", "", relayURLFlag, "", false)
		return exitCode
	})
	return exitCode, stdout + stderr
}

func TestInteractiveSetupSkipsInstallWhenRelayAlreadyRuns(t *testing.T) {
	haURL := newSetupDetectionHA(t)
	probed := stubInstalledRelayProbe(t, true, "")
	// Enter opens NOVA; exit at the six-digit code prompt.
	exitCode, output := runSetupDetectionWizard(t, haURL, "", "", "exit")
	if exitCode != 0 {
		t.Fatalf("exit = %d\n%s", exitCode, output)
	}
	if want := deriveRelayURLFromHA(haURL, normalizeHostInput(haURL)); len(*probed) != 1 || (*probed)[0] != want {
		t.Fatalf("probed %v, want [%s]", *probed, want)
	}
	// The note must sit under the pairing step header: every stage starts
	// with a screen clear, so anything printed before it is lost on a TTY.
	header := strings.LastIndex(output, "Connect this computer")
	note := strings.Index(output, "NOVA App is running")
	if header < 0 || note < header || !strings.Contains(output, "Six-digit code") {
		t.Fatalf("skip note must follow the pairing header (header=%d note=%d):\n%s", header, note, output)
	}
	for _, unwanted := range []string{"Install the NOVA App", "Your browser opens the NOVA App", "Press Enter when the App is running"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("install steps shown although the relay runs (%q):\n%s", unwanted, output)
		}
	}
}

func TestInteractiveSetupShowsOneInstallLinkWhenRelayIsMissing(t *testing.T) {
	haURL := newSetupDetectionHA(t)
	probed := stubInstalledRelayProbe(t, false, "")
	exitCode, output := runSetupDetectionWizard(t, haURL, "", "exit")
	if exitCode != 0 {
		t.Fatalf("exit = %d\n%s", exitCode, output)
	}
	if len(*probed) != 1 || strings.Contains(output, "NOVA App is running") ||
		!strings.Contains(output, "Your browser opens the NOVA App") ||
		!strings.Contains(output, haInstallRelayAppURL(haURL)) ||
		strings.Contains(output, "supervisor_add_addon_repository") {
		t.Fatalf("expected the install steps with the one App link:\n%s", output)
	}
}

func TestInteractiveSetupNeverSkipsInstallForExplicitRelayURL(t *testing.T) {
	haURL := newSetupDetectionHA(t)
	probed := stubInstalledRelayProbe(t, true, "")
	exitCode, output := runSetupDetectionWizard(t, haURL, "http://192.168.1.9:8791", "exit")
	if exitCode != 0 {
		t.Fatalf("exit = %d\n%s", exitCode, output)
	}
	if len(*probed) != 0 || !strings.Contains(output, "Your browser opens the NOVA App") {
		t.Fatalf("an explicit relay URL must show the install steps (probed %v):\n%s", *probed, output)
	}
}

func TestInteractiveSetupBackFromPairingAfterSkipReturnsToAddress(t *testing.T) {
	haURL := newSetupDetectionHA(t)
	stubInstalledRelayProbe(t, true, "")
	// back at "Press Enter to open NOVA" -> host stage (discovery again) ->
	// skip again -> pairing; exit there.
	exitCode, output := runSetupDetectionWizard(t, haURL, "", "back", "exit")
	if exitCode != 0 {
		t.Fatalf("exit = %d\n%s", exitCode, output)
	}
	if got := strings.Count(output, "Discovering Home Assistant on your network"); got != 2 {
		t.Fatalf("back must return to the address step, discovery ran %d times:\n%s", got, output)
	}
	if strings.Contains(output, "Your browser opens the NOVA App") {
		t.Fatalf("back must not land on install steps that were never shown:\n%s", output)
	}
}
