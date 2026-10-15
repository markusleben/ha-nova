package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brutella/dnssd"
)

const pairInfoV1Body = `{"ok":true,"data":{"relay_version":"0.26.0","protocol_version":"v1","available":true}}`

func TestHASupportsCombinedAppLinkNeeds2026Point2(t *testing.T) {
	for version, want := range map[string]bool{
		"2026.2.0":      true,
		"2026.10.1":     true,
		"2026.11.0b2":   true,
		"2027.1.0":      true,
		" 2026.2.3 ":    true,
		"2026.1.3":      false,
		"2025.12.4":     false,
		"":              false,
		"2026":          false,
		"dev":           false,
		"2026.x.0":      false,
		"version 2026.": false,
	} {
		if got := haSupportsCombinedAppLink(version); got != want {
			t.Errorf("haSupportsCombinedAppLink(%q) = %v, want %v", version, got, want)
		}
	}
}

func TestRelayInstallGuideUsesCombinedLinkOnlyWhenSupported(t *testing.T) {
	haURL := "http://192.168.1.5:8123"
	want := "http://192.168.1.5:8123/_my_redirect/supervisor_app?app=2368fcfa_ha_nova_relay&repository_url=https%3A%2F%2Fgithub.com%2Fmarkusleben%2Fha-nova"
	if got := haInstallRelayAppURL(haURL); got != want {
		t.Fatalf("haInstallRelayAppURL() = %q", got)
	}

	combined := relayInstallGuideFor(haURL, "2026.10.0")
	if combined.Link != want || len(combined.Steps) != 2 ||
		strings.Contains(strings.Join(combined.Steps, "\n"), "Search") {
		t.Fatalf("combined guide = %+v", combined)
	}
	for _, version := range []string{"", "2026.1.0"} {
		fallback := relayInstallGuideFor(haURL, version)
		if fallback.Link != haAddRepositoryURL(haURL) || fallback.Combined ||
			!strings.Contains(strings.Join(fallback.Steps, "\n"), `Search for "NOVA Relay"`) {
			t.Fatalf("version %q: fallback guide = %+v", version, fallback)
		}
	}
}

// The real probe (no stub): only the relay's own v1 envelope counts. Anything
// else — another web server, an error page, a proxy, a closed port — must
// show the install steps.
func TestRelayAppAlreadyRunningIsNotFooledByOtherServers(t *testing.T) {
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
			if relayAppAlreadyRunning(&bytes.Buffer{}, runtimeConfig{RelayBaseURL: server.URL}) {
				t.Fatal("detected a running relay where none is")
			}
		})
	}

	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	if relayAppAlreadyRunning(&bytes.Buffer{}, runtimeConfig{RelayBaseURL: closedURL}) {
		t.Fatal("detected a running relay on a closed port")
	}
	if relayAppAlreadyRunning(&bytes.Buffer{}, runtimeConfig{}) {
		t.Fatal("detected a running relay without any address")
	}

	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pair/v1/info" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(pairInfoV1Body))
	}))
	defer relay.Close()
	out := &bytes.Buffer{}
	if !relayAppAlreadyRunning(out, runtimeConfig{RelayBaseURL: relay.URL}) {
		t.Fatalf("missed a running relay:\n%s", out)
	}
	if !strings.Contains(out.String(), "already installed and running") {
		t.Fatalf("missing skip note:\n%s", out)
	}
}

func newSetupDetectionServers(t *testing.T, pairInfo http.HandlerFunc) (string, string) {
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
	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(haServer.Close)
	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/pair/v1/info" {
			pairInfo(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(relayServer.Close)
	return haServer.URL, relayServer.URL
}

// runSetupDetectionWizard drives a fresh computer: no saved address, the
// instance comes from discovery (with the given mDNS version), so the wizard
// walks host -> install -> pairing like every extra computer does.
func runSetupDetectionWizard(t *testing.T, haURL, relayURL, haVersion, input string) (int, string) {
	t.Helper()
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		discoverReachableHAHostsForSetup = func(runtimeConfig) ([]setupDiscoveryCandidate, string) {
			return []setupDiscoveryCandidate{{
				Host: normalizeHostInput(haURL), HAURL: haURL, Source: "mDNS: Home", Version: haVersion,
			}}, ""
		}
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "codex", "", "", relayURL, "", false)
		return exitCode
	})
	return exitCode, stdout + stderr
}

// The service-mode question comes first on this headless test box.
var setupDetectionLeadInputs = []string{"n"}

func TestInteractiveSetupSkipsInstallWhenRelayAlreadyRuns(t *testing.T) {
	haURL, relayURL := newSetupDetectionServers(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(pairInfoV1Body))
	})
	// Enter opens NOVA; exit at the six-digit code prompt.
	exitCode, output := runSetupDetectionWizard(t, haURL, relayURL, "", joinSetupInputs(setupDetectionLeadInputs, []string{"", "exit"}))
	if exitCode != 0 {
		t.Fatalf("exit = %d\n%s", exitCode, output)
	}
	for _, want := range []string{"Checking whether NOVA Relay is already running", "already installed and running", "Pair this device", "Six-digit code"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q:\n%s", want, output)
		}
	}
	for _, unwanted := range []string{"Next, install the NOVA Relay App", "Press Enter when the app is running"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("install steps shown although the relay runs (%q):\n%s", unwanted, output)
		}
	}
}

func TestInteractiveSetupShowsInstallWhenSomethingElseAnswers(t *testing.T) {
	haURL, relayURL := newSetupDetectionServers(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>not a relay</html>"))
	})
	exitCode, output := runSetupDetectionWizard(t, haURL, relayURL, "", joinSetupInputs(setupDetectionLeadInputs, []string{"exit"}))
	if exitCode != 0 {
		t.Fatalf("exit = %d\n%s", exitCode, output)
	}
	if !strings.Contains(output, "Checking whether NOVA Relay is already running") ||
		strings.Contains(output, "already installed and running") ||
		!strings.Contains(output, "Next, install the NOVA Relay App") ||
		!strings.Contains(output, haAddRepositoryURL(haURL)) {
		t.Fatalf("expected the (fallback) install steps:\n%s", output)
	}
}

func TestInteractiveSetupBackFromPairingAfterSkipShowsInstallSteps(t *testing.T) {
	haURL, relayURL := newSetupDetectionServers(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(pairInfoV1Body))
	})
	// back at "Press Enter to open NOVA" -> install stage, which must show the
	// steps instead of skipping forward again; exit there.
	exitCode, output := runSetupDetectionWizard(t, haURL, relayURL, "", joinSetupInputs(setupDetectionLeadInputs, []string{"back", "exit"}))
	if exitCode != 0 {
		t.Fatalf("exit = %d\n%s", exitCode, output)
	}
	if got := strings.Count(output, "Checking whether NOVA Relay is already running"); got != 1 {
		t.Fatalf("detection ran %d times, want 1:\n%s", got, output)
	}
	skipped := strings.Index(output, "already installed and running")
	steps := strings.Index(output, "Next, install the NOVA Relay App")
	if skipped < 0 || steps < skipped {
		t.Fatalf("expected skip first, then the install steps after back:\n%s", output)
	}
}

func TestInteractiveSetupUsesCombinedLinkForDiscoveredNewHA(t *testing.T) {
	haURL, relayURL := newSetupDetectionServers(t, http.NotFound)
	exitCode, output := runSetupDetectionWizard(t, haURL, relayURL, "2026.10.1", joinSetupInputs(setupDetectionLeadInputs, []string{"exit"}))
	if exitCode != 0 {
		t.Fatalf("exit = %d\n%s", exitCode, output)
	}
	if !strings.Contains(output, haInstallRelayAppURL(haURL)) || strings.Contains(output, haAddRepositoryURL(haURL)) {
		t.Fatalf("expected only the combined install link:\n%s", output)
	}
}

func TestDiscoveryKeepsMDNSVersionAcrossDuplicates(t *testing.T) {
	originalLookup := lookupDNSSDForDiscovery
	t.Cleanup(func() { lookupDNSSDForDiscovery = originalLookup })
	lookupDNSSDForDiscovery = func(_ context.Context, _ string, add dnssd.AddFunc, _ dnssd.RmvFunc) error {
		// First announcement without a version, duplicate with one.
		add(dnssd.BrowseEntry{Text: map[string]string{
			"uuid": "556851affd194582ad3f150856f13a05", "internal_url": "http://192.168.1.5:8123",
		}})
		add(dnssd.BrowseEntry{Text: map[string]string{
			"uuid": "556851affd194582ad3f150856f13a05", "internal_url": "http://192.168.1.5:8123", "version": "2026.10.1",
		}})
		return nil
	}
	got := discoverHAViaMDNS()
	if len(got) != 1 || got[0].Version != "2026.10.1" {
		t.Fatalf("discoverHAViaMDNS() = %+v", got)
	}

	originalMDNS := discoverHAViaMDNSForDiscovery
	t.Cleanup(func() { discoverHAViaMDNSForDiscovery = originalMDNS })
	discoverHAViaMDNSForDiscovery = func() []setupDiscoveryProbe { return got }
	// The saved address is listed first; the mDNS duplicate still adds its version.
	probes := collectDiscoveryProbes(runtimeConfig{HAURL: "http://192.168.1.5:8123"})
	if len(probes) != 1 || probes[0].Version != "2026.10.1" || probes[0].Source != "saved Home Assistant address" {
		t.Fatalf("collectDiscoveryProbes() = %+v", probes)
	}
}
