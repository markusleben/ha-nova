package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/brutella/dnssd"
)

// Fixture: a fully completed default-profile install, mirroring
// TestInteractiveSetupAlreadyDoneUsesResumeBanner. Returns everything needed
// to drive interactiveSetup into the already-done screen.
func setupCompletedInstallFixture(t *testing.T) (runtimePaths, runtimeConfig, installState, [][]byte) {
	t.Helper()
	withClientRuntimeAvailability(t, map[string]bool{"claude": true})
	withClientAttachmentPresence(t, map[string]bool{"claude": true})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	stubCensusTTY(t, true, true)
	resetServerProfileSelection(t)

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	t.Cleanup(relayServer.Close)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	// A modern secure-paired install carries the install-wide
	// client_install_id — the add flow must seed it, or the pairing stage
	// mints a fresh one and the immutability guard kills the flow.
	cfg := runtimeConfig{
		HAHost:          "192.168.1.5",
		HAURL:           "http://192.168.1.5:8123",
		RelayBaseURL:    relayServer.URL,
		ClientInstallID: "inst-0123456789abcdef0123456789abcdef",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}
	state := loadStateOrDefault(paths)
	mergeStateClients(&state, []string{"claude"})
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	writeInstalledClaudePluginFixture(t, home)
	writeClaudeMarketplaceRegistrationFixture(t, home, filepath.Join(paths.ConfigDir, "claude-marketplace"))
	if err := markCensusLifecycleStopped(paths); err != nil {
		t.Fatalf("mark census lifecycle stopped: %v", err)
	}
	marker := [][]byte{
		captureInstallLifecycleGeneration(paths),
		captureCensusLifecycleMarker(paths),
	}
	return paths, cfg, state, marker
}

func stubAddServerTTY(t *testing.T) {
	t.Helper()
	originalTTY := writerSupportsTTYForSetup
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	t.Cleanup(func() { writerSupportsTTYForSetup = originalTTY })
	originalStdin := addServerOfferStdinIsTTY
	addServerOfferStdinIsTTY = func() bool { return true }
	t.Cleanup(func() { addServerOfferStdinIsTTY = originalStdin })
}

// captureInteractiveSetupIO installs its own no-candidate discovery stub, so
// per-test candidates must be injected INSIDE the capture callback.
func withAddServerDiscovery(candidates []setupDiscoveryCandidate, fn func() int) func() int {
	return func() int {
		discoverReachableHAHostsForSetup = func(runtimeConfig) ([]setupDiscoveryCandidate, string) {
			return candidates, ""
		}
		return fn()
	}
}

func TestAddServerOfferDeclineKeepsConfig(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	stubAddServerTTY(t)
	before, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatalf("read config before: %v", err)
	}

	// Enter declines the offer (default No); "2" answers the census ask.
	stdout, stderr := captureInteractiveSetupIO(t, "\n2\n", func() int {
		return interactiveSetup(paths, cfg, state, "claude", "", "", "", "", false, marker...)
	})
	output := stdout + stderr
	if !strings.Contains(output, "Add another Home Assistant server?") {
		t.Fatalf("expected add-server offer on the completed screen:\n%s", output)
	}
	if !strings.Contains(output, "Everything is already set up!") {
		t.Fatalf("declining the offer keeps the done banner:\n%s", output)
	}
	after, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatalf("read config after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("declining the offer must not change config.json:\nbefore: %s\nafter: %s", before, after)
	}
}

func TestAddServerFlowCreatesNamedProfileAndKeepsDefaultIntact(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	stubAddServerTTY(t)

	originalProbe := probePairingV1ForSetup
	probePairingV1ForSetup = func(string) bool { return true }
	t.Cleanup(func() { probePairingV1ForSetup = originalProbe })

	originalPair := securePairForSetup
	securePairForSetup = func(
		_ string,
		code string,
		pairCfg *runtimeConfig,
		save func(*runtimeConfig) error,
		_ pairingClientInfo,
	) (string, error) {
		if code != "123456" {
			t.Fatalf("unexpected pairing code: %q", code)
		}
		pairCfg.RelaySecureBaseURL = "https://192.168.1.7:8792"
		pairCfg.RelaySpkiPin = "test-pin"
		if err := save(pairCfg); err != nil {
			t.Fatalf("pairing save failed: %v", err)
		}
		return "", nil
	}
	t.Cleanup(func() { securePairForSetup = originalPair })

	originalVerify := verifyDeviceHealth
	verifyDeviceHealth = func(runtimeConfig) bool { return true }
	t.Cleanup(func() { verifyDeviceHealth = originalVerify })

	docBefore, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatalf("load config document before: %v", err)
	}
	defaultBefore, ok := docBefore.flatProfile(defaultServerProfileName)
	if !ok {
		t.Fatal("default profile missing before add-server flow")
	}

	// y (offer) → cabin (name) → 1 (discovery pick) → Enter (app running)
	// → Enter (open NOVA) → 123456 (code) → 2 (census).
	input := joinSetupInputs([]string{"y", "cabin", "1", "", "", "123456", "2"})
	stdout, stderr := captureInteractiveSetupIO(t, input, withAddServerDiscovery(
		[]setupDiscoveryCandidate{
			{Host: "192.168.1.7", HAURL: "http://192.168.1.7:8123", Via: "mDNS", Source: "mdns"},
		},
		func() int {
			return interactiveSetup(paths, cfg, state, "claude", "", "", "", "", false, marker...)
		},
	))
	output := stdout + stderr
	if !strings.Contains(output, `Server "cabin" is paired and verified`) {
		t.Fatalf("expected add-server success line:\n%s", output)
	}
	if !strings.Contains(output, "HA_NOVA_SERVER=cabin") {
		t.Fatalf("expected selection hint for the new profile:\n%s", output)
	}

	if got := activeServerProfile(); got != defaultServerProfileName {
		t.Fatalf("selection seam not restored after add flow: %q", got)
	}

	docAfter, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatalf("load config document after: %v", err)
	}
	cabin, ok := docAfter.flatProfile("cabin")
	if !ok {
		t.Fatalf("cabin profile missing after add-server flow; profiles: %v", docAfter.profileNames())
	}
	if cabin.HAHost != "192.168.1.7" {
		t.Fatalf("cabin profile host = %q, want 192.168.1.7", cabin.HAHost)
	}
	if cabin.RelaySecureBaseURL != "https://192.168.1.7:8792" {
		t.Fatalf("cabin secure endpoint = %q", cabin.RelaySecureBaseURL)
	}
	defaultAfter, ok := docAfter.flatProfile(defaultServerProfileName)
	if !ok {
		t.Fatal("default profile missing after add-server flow")
	}
	// The install-wide ClientInstallID is seeded from the document — a fresh
	// mint would be rejected by the immutability guard on every secure-paired
	// install.
	if cabin.ClientInstallID != "inst-0123456789abcdef0123456789abcdef" {
		t.Fatalf("cabin must carry the seeded install-wide id, got %q", cabin.ClientInstallID)
	}
	if !reflect.DeepEqual(defaultBefore, defaultAfter) {
		t.Fatalf("default profile changed by add-server flow:\nbefore: %+v\nafter: %+v", defaultBefore, defaultAfter)
	}
}

func TestAddServerOfferSkippedWithoutInteractiveStdin(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	stubAddServerTTY(t)
	addServerOfferStdinIsTTY = func() bool { return false }

	// Only the census answer is consumed — the offer must not read stdin.
	stdout, stderr := captureInteractiveSetupIO(t, "2\n", func() int {
		return interactiveSetup(paths, cfg, state, "claude", "", "", "", "", false, marker...)
	})
	output := stdout + stderr
	if strings.Contains(output, "Add another Home Assistant server?") {
		t.Fatalf("offer must not appear without interactive stdin:\n%s", output)
	}
	if !strings.Contains(output, "Everything is already set up!") {
		t.Fatalf("expected the done banner:\n%s", output)
	}
}

func TestAddServerRejectsInvalidAndDuplicateNames(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	stubAddServerTTY(t)

	// y → invalid name → duplicate name → exit.
	input := joinSetupInputs([]string{"y", "Bad Name!", defaultServerProfileName, "exit"})
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		return interactiveSetup(paths, cfg, state, "claude", "", "", "", "", false, marker...)
	})
	output := stdout + stderr
	if !strings.Contains(output, "already exists") {
		t.Fatalf("expected duplicate-name rejection:\n%s", output)
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatalf("load config document: %v", err)
	}
	if got := doc.profileNames(); len(got) != 1 {
		t.Fatalf("no profile may be created on name errors; profiles: %v", got)
	}
}

func TestAddServerDiscoveryFiltersConfiguredInstances(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	stubAddServerTTY(t)

	// y → cabin → 1 (only the unconfigured instance) → exit at the app prompt.
	input := joinSetupInputs([]string{"y", "cabin", "1", "exit"})
	stdout, stderr := captureInteractiveSetupIO(t, input, withAddServerDiscovery(
		[]setupDiscoveryCandidate{
			// Already configured on the default profile — must be hidden.
			{Host: "192.168.1.5", HAURL: "http://192.168.1.5:8123", Source: "mdns"},
			{Host: "192.168.1.7", HAURL: "http://192.168.1.7:8123", Source: "mdns"},
			// Same HOST as the configured instance but a different HA port is
			// a DIFFERENT instance and must survive the filter.
			{Host: "192.168.1.5", HAURL: "http://192.168.1.5:8124", Source: "mdns"},
		},
		func() int {
			return interactiveSetup(paths, cfg, state, "claude", "", "", "", "", false, marker...)
		},
	))
	output := stdout + stderr
	if !strings.Contains(output, "Found 2 Home Assistant instance(s) that are not configured yet.") {
		t.Fatalf("expected the configured instance to be filtered from discovery:\n%s", output)
	}
	if !strings.Contains(output, "was saved but is not paired yet") {
		t.Fatalf("expected the partial-profile note after cancelling:\n%s", output)
	}
	// A ran add flow replaces the done banner — ending a cancelled add with
	// "Everything is already set up!" would bury the partial-profile state.
	if strings.Contains(output, "Everything is already set up!") {
		t.Fatalf("done banner must not follow a ran add flow:\n%s", output)
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatalf("load config document: %v", err)
	}
	cabin, ok := doc.flatProfile("cabin")
	if !ok {
		t.Fatalf("cabin profile missing; profiles: %v", doc.profileNames())
	}
	if cabin.HAHost != "192.168.1.7" {
		t.Fatalf("cabin picked the wrong instance: %q", cabin.HAHost)
	}
}

func TestAddServerHostPortIdentityKeys(t *testing.T) {
	// A .local-configured profile matches the same instance rediscovered as a
	// resolved IPv4 through the candidate's Via spelling.
	doc, err := parseConfigDocument([]byte(`{
		"schema_version": 3,
		"default_server": "default",
		"servers": {
			"default": {"ha_host": "homeassistant.local", "ha_url": "http://homeassistant.local:8123"},
			"relayonly": {"relay_base_url": "http://192.168.1.9:8791"},
			"proxied": {"ha_host": "ha2.example", "ha_url": "https://ha2.example"}
		}
	}`))
	if err != nil {
		t.Fatalf("parse fixture document: %v", err)
	}
	configured := configuredServerHostPortKeys(doc)
	viaCandidate := setupDiscoveryCandidate{
		Host:  "192.168.1.5",
		HAURL: "http://192.168.1.5:8123",
		Via:   "homeassistant.local",
	}
	if !configured[addServerViaHostPortKey(viaCandidate)] {
		t.Fatal("resolved-IPv4 candidate with a configured .local Via must be recognized as configured")
	}
	if !configured[addServerHostPortKey("http://192.168.1.9:8123")] {
		t.Fatal("relay-URL-only profiles must still claim their host at the default HA port")
	}
	if configured[addServerHostPortKey("http://homeassistant.local:8124")] {
		t.Fatal("a different HA port on the same host is a different instance")
	}
	// Scheme-default ports: an explicit URL without a port means 80/443,
	// never the HA default — only bare hosts imply 8123.
	if got := addServerHostPortKey("https://ha.example"); got != "ha.example:443" {
		t.Fatalf("https default port key = %q", got)
	}
	if got := addServerHostPortKey("http://ha.example"); got != "ha.example:80" {
		t.Fatalf("http default port key = %q", got)
	}
	if got := addServerHostPortKey("ha.example"); got != "ha.example:8123" {
		t.Fatalf("bare host key = %q", got)
	}
	// A profile whose URL pins a non-default port must NOT also claim the
	// host at 8123 through its bare HAHost — that would hide a genuinely
	// separate instance there.
	if configured["ha2.example:8123"] {
		t.Fatal("bare HAHost must not add a default-port key when the URL pins another port")
	}
	if !configured["ha2.example:443"] {
		t.Fatal("the URL's scheme-default port key is the profile's identity")
	}
	// The Via alias inherits the candidate URL's EFFECTIVE port, including
	// scheme defaults for portless URLs.
	portlessVia := setupDiscoveryCandidate{
		Host:  "192.168.1.6",
		HAURL: "http://192.168.1.6",
		Via:   "portless.local",
	}
	if got := addServerViaHostPortKey(portlessVia); got != "portless.local:80" {
		t.Fatalf("portless Via key = %q", got)
	}
}

func TestExpandConfiguredLocalAliasesClaimsResolvedIPv4(t *testing.T) {
	originalResolve := resolveHostToIPv4ForDiscovery
	resolveHostToIPv4ForDiscovery = func(host string, _ time.Duration) string {
		if host == "homeassistant.local" {
			return "192.168.1.5"
		}
		return ""
	}
	t.Cleanup(func() { resolveHostToIPv4ForDiscovery = originalResolve })

	configured := map[string]bool{
		"homeassistant.local:8123": true,
		"192.168.1.9:8123":         true,
	}
	expandConfiguredLocalAliases(configured)
	if !configured["192.168.1.5:8123"] {
		t.Fatal("the .local profile must claim its resolved IPv4 at the same port")
	}
	if len(configured) != 3 {
		t.Fatalf("only the .local key may expand, got %v", configured)
	}
	// The reverse direction: a typed .local hostname matches a profile
	// configured by its resolved IPv4.
	ipOnly := map[string]bool{"192.168.1.5:8123": true}
	if !manualHostIsConfigured(ipOnly, "http://homeassistant.local:8123") {
		t.Fatal("a typed .local hostname must match its IPv4-configured profile")
	}
	if manualHostIsConfigured(ipOnly, "http://other.local:8123") {
		t.Fatal("an unresolvable .local hostname must not match")
	}
}

func TestDNSSDAdvertisedLocalHostSurvivesIPv4Rewrite(t *testing.T) {
	entry := dnssd.BrowseEntry{
		Text: map[string]string{
			"uuid":         "0123456789abcdef0123456789abcdef",
			"internal_url": "http://homeassistant.local:8123",
		},
		IPs: []net.IP{net.ParseIP("192.168.1.5")},
	}
	if got := homeAssistantDNSSDURL(entry); got != "http://192.168.1.5:8123" {
		t.Fatalf("expected the IPv4 rewrite, got %q", got)
	}
	if got := dnssdAdvertisedLocalHost(entry); got != "homeassistant.local" {
		t.Fatalf("advertised .local host lost in rewrite: %q", got)
	}
	entry.Text["internal_url"] = "https://ha.example.com"
	if got := dnssdAdvertisedLocalHost(entry); got != "" {
		t.Fatalf("non-.local internal_url must not produce a Via: %q", got)
	}
	// Without an internal_url the record's own .local Host is the advertised
	// name the IPv4 rewrite replaced.
	noInternal := dnssd.BrowseEntry{
		Host: "0123456789abcdef0123456789abcdef.local.",
		Text: map[string]string{"uuid": "0123456789abcdef0123456789abcdef"},
	}
	if got := dnssdAdvertisedLocalHost(noInternal); got != "0123456789abcdef0123456789abcdef.local" {
		t.Fatalf("entry.Host .local name lost without internal_url: %q", got)
	}
}

func TestCollectDiscoveryProbesKeepsAdvertisedVia(t *testing.T) {
	originalMDNS := discoverHAViaMDNSForDiscovery
	discoverHAViaMDNSForDiscovery = func() []setupDiscoveryProbe {
		return []setupDiscoveryProbe{{
			Host:   "http://192.168.1.5:8123",
			Source: "mDNS",
			Via:    "homeassistant.local",
		}}
	}
	t.Cleanup(func() { discoverHAViaMDNSForDiscovery = originalMDNS })

	probes := collectDiscoveryProbes(runtimeConfig{})
	if len(probes) != 1 || probes[0].Via != "homeassistant.local" {
		t.Fatalf("advertised Via flattened away in probe collection: %+v", probes)
	}
}
