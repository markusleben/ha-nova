package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Schema-v2 (multi-server) coverage: v1→v2 migration, save-path un-flatten,
// sibling preservation, the legacy downgrade mirror, selection order, and the
// issue-#200 raw default-profile loader.

// resetServerProfileSelection isolates the process-global selection seam and
// the HA_NOVA_SERVER env var for one test.
func resetServerProfileSelection(t *testing.T) {
	t.Helper()
	prevOverride, prevActive := serverSelectionOverride, activeServerProfileName
	t.Setenv(serverSelectionEnvVar, "")
	t.Cleanup(func() {
		serverSelectionOverride, activeServerProfileName = prevOverride, prevActive
	})
	serverSelectionOverride = ""
	activeServerProfileName = defaultServerProfileName
}

func writeTestConfigFile(t *testing.T, content string) runtimePaths {
	t.Helper()
	dir := t.TempDir()
	paths := runtimePaths{ConfigDir: dir, ConfigFile: filepath.Join(dir, "config.json")}
	if err := os.WriteFile(paths.ConfigFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return paths
}

func readTestConfigTopLevel(t *testing.T, paths runtimePaths) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatal(err)
	}
	return top
}

const testV1FlatConfig = `{"schema_version":1,"ha_host":"ha","ha_url":"http://ha:8123","relay_base_url":"http://ha:8791","relay_token_file":"relay-token","client_install_id":"inst-abc","relay_secure_base_url":"https://ha:18792","relay_spki_pin":"PIN"}`

func TestV1FlatConfigMigratesToProfilesOnSaveWithLegacyMirror(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, testV1FlatConfig)

	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatalf("loadConfig(v1): %v", err)
	}
	if cfg.RelayBaseURL != "http://ha:8791" || cfg.RelayTokenFile != "relay-token" || cfg.ClientInstallID != "inst-abc" {
		t.Fatalf("v1 flat fields must load as the default profile, got %+v", cfg)
	}
	if activeServerProfile() != defaultServerProfileName {
		t.Fatalf("active profile = %q, want default", activeServerProfile())
	}

	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}
	top := readTestConfigTopLevel(t, paths)
	if string(top["schema_version"]) != "2" {
		t.Fatalf("schema_version = %s, want 2", top["schema_version"])
	}
	if string(top["default_server"]) != `"default"` {
		t.Fatalf("default_server = %s, want \"default\"", top["default_server"])
	}
	if string(top["client_install_id"]) != `"inst-abc"` {
		t.Fatalf("client_install_id must stay install-wide, got %s", top["client_install_id"])
	}
	var servers map[string]serverProfileConfig
	if err := json.Unmarshal(top["servers"], &servers); err != nil {
		t.Fatalf("servers map: %v", err)
	}
	if servers["default"].RelayBaseURL != "http://ha:8791" || servers["default"].RelayTokenFile != "relay-token" {
		t.Fatalf("servers.default = %+v", servers["default"])
	}
	// Downgrade floor: an old binary unmarshals the flat mirror and keeps
	// working against the default profile.
	var oldShape runtimeConfig
	data, _ := os.ReadFile(paths.ConfigFile)
	if err := json.Unmarshal(data, &oldShape); err != nil {
		t.Fatalf("old-binary shape unreadable: %v", err)
	}
	if oldShape.RelayBaseURL != "http://ha:8791" || oldShape.RelaySpkiPin != "PIN" || oldShape.RelayTokenFile != "relay-token" || oldShape.ClientInstallID != "inst-abc" {
		t.Fatalf("legacy mirror incomplete: %+v", oldShape)
	}

	// Idempotent: load+save of the migrated file changes nothing.
	reloaded, err := loadConfig(paths)
	if err != nil {
		t.Fatalf("loadConfig(v2): %v", err)
	}
	cfg.SchemaVersion = configSchemaVersion
	if reloaded != cfg {
		t.Fatalf("v2 reload differs from v1 load:\n  got  %+v\n  want %+v", reloaded, cfg)
	}
	before, _ := os.ReadFile(paths.ConfigFile)
	if err := saveConfig(paths, reloaded); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(paths.ConfigFile)
	if string(before) != string(after) {
		t.Fatalf("second save must be byte-identical:\n before: %s\n after:  %s", before, after)
	}
}

const testV2TwoProfileConfig = `{
  "schema_version": 2,
  "default_server": "default",
  "client_install_id": "inst-abc",
  "future_flag": {"keep": true},
  "ha_host": "ha",
  "ha_url": "http://ha:8123",
  "relay_base_url": "http://ha:8791",
  "servers": {
    "default": {"ha_host": "ha", "ha_url": "http://ha:8123", "relay_base_url": "http://ha:8791"},
    "cabin": {"ha_host": "cabin", "ha_url": "http://cabin:8123", "relay_base_url": "http://cabin:8791", "relay_secure_base_url": "https://cabin:18792", "relay_spki_pin": "PINB"}
  }
}`

func TestSaveOnProfileBLeavesProfileAByteIdenticalAndKeepsUnknownFields(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, testV2TwoProfileConfig)

	setServerSelectionOverride("cabin")
	cfgB, err := loadConfig(paths)
	if err != nil {
		t.Fatalf("loadConfig(cabin): %v", err)
	}
	if cfgB.RelayBaseURL != "http://cabin:8791" || cfgB.ClientInstallID != "inst-abc" {
		t.Fatalf("cabin profile = %+v", cfgB)
	}
	// Normalizing save, then a real change — like a doctor-resume or re-pair on
	// profile B.
	if err := saveConfig(paths, cfgB); err != nil {
		t.Fatal(err)
	}
	topBefore := readTestConfigTopLevel(t, paths)
	var serversBefore map[string]json.RawMessage
	if err := json.Unmarshal(topBefore["servers"], &serversBefore); err != nil {
		t.Fatal(err)
	}
	cfgB.RelaySpkiPin = "PINB-ROTATED"
	if err := saveConfig(paths, cfgB); err != nil {
		t.Fatal(err)
	}
	topAfter := readTestConfigTopLevel(t, paths)
	var serversAfter map[string]json.RawMessage
	if err := json.Unmarshal(topAfter["servers"], &serversAfter); err != nil {
		t.Fatal(err)
	}
	if string(serversBefore["default"]) != string(serversAfter["default"]) {
		t.Fatalf("sibling profile changed:\n before: %s\n after:  %s", serversBefore["default"], serversAfter["default"])
	}
	if !strings.Contains(string(serversAfter["cabin"]), "PINB-ROTATED") {
		t.Fatalf("cabin profile not updated: %s", serversAfter["cabin"])
	}
	if string(topAfter["future_flag"]) != string(topBefore["future_flag"]) || topAfter["future_flag"] == nil {
		t.Fatalf("unknown top-level field lost: %s", topAfter["future_flag"])
	}
	if string(topAfter["default_server"]) != `"default"` {
		t.Fatalf("default_server changed: %s", topAfter["default_server"])
	}
	// The legacy mirror still carries the DEFAULT profile after a save on B.
	var oldShape runtimeConfig
	data, _ := os.ReadFile(paths.ConfigFile)
	if err := json.Unmarshal(data, &oldShape); err != nil {
		t.Fatal(err)
	}
	if oldShape.RelayBaseURL != "http://ha:8791" || oldShape.RelaySpkiPin != "" {
		t.Fatalf("legacy mirror must stay the default profile's data: %+v", oldShape)
	}
}

func TestServerSelectionOrderFlagOverEnvOverDefault(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, testV2TwoProfileConfig)

	t.Setenv(serverSelectionEnvVar, "cabin")
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatalf("env selection: %v", err)
	}
	if cfg.RelayBaseURL != "http://cabin:8791" || activeServerProfile() != "cabin" {
		t.Fatalf("env must select cabin, got %q / active %q", cfg.RelayBaseURL, activeServerProfile())
	}

	setServerSelectionOverride("default")
	cfg, err = loadConfig(paths)
	if err != nil {
		t.Fatalf("flag selection: %v", err)
	}
	if cfg.RelayBaseURL != "http://ha:8791" || activeServerProfile() != defaultServerProfileName {
		t.Fatalf("--server must beat the env, got %q / active %q", cfg.RelayBaseURL, activeServerProfile())
	}

	setServerSelectionOverride("")
	t.Setenv(serverSelectionEnvVar, "")
	cfg, err = loadConfig(paths)
	if err != nil {
		t.Fatalf("default selection: %v", err)
	}
	if cfg.RelayBaseURL != "http://ha:8791" {
		t.Fatalf("default_server must apply, got %q", cfg.RelayBaseURL)
	}

	// A configured non-default default_server is honored too.
	pathsB := writeTestConfigFile(t, strings.Replace(testV2TwoProfileConfig, `"default_server": "default"`, `"default_server": "cabin"`, 1))
	cfg, err = loadConfig(pathsB)
	if err != nil {
		t.Fatalf("default_server=cabin: %v", err)
	}
	if cfg.RelayBaseURL != "http://cabin:8791" || activeServerProfile() != "cabin" {
		t.Fatalf("default_server=cabin must select cabin, got %q", cfg.RelayBaseURL)
	}
}

func TestUnknownServerSelectionFailsLoudListingProfiles(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, testV2TwoProfileConfig)
	t.Setenv(serverSelectionEnvVar, "nope")

	_, err := loadConfig(paths)
	if !errors.Is(err, errUnknownServerProfile) {
		t.Fatalf("expected errUnknownServerProfile, got %v", err)
	}
	message := err.Error()
	if !strings.Contains(message, `"nope"`) || !strings.Contains(message, "cabin, default") {
		t.Fatalf("error must name the typo and list known profiles, got: %s", message)
	}
	// A typo must not select anything: the seam stays on the default.
	if activeServerProfile() != defaultServerProfileName {
		t.Fatalf("active profile changed on a failed selection: %q", activeServerProfile())
	}
}

func TestValidateServerProfileName(t *testing.T) {
	valid := []string{"a", "cabin", "cabin-2", "0", strings.Repeat("x", 32), "default"}
	for _, name := range valid {
		if err := validateServerProfileName(name); err != nil {
			t.Errorf("validateServerProfileName(%q) = %v, want nil", name, err)
		}
	}
	invalid := []string{"", "Cabin", "ha_nova", "with space", "ä", strings.Repeat("x", 33), "pending", "probe"}
	for _, name := range invalid {
		if err := validateServerProfileName(name); err == nil {
			t.Errorf("validateServerProfileName(%q) = nil, want error", name)
		}
	}
}

func TestSelectedServerProfileStatusCountsProfiles(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, testV2TwoProfileConfig)
	name, count := selectedServerProfileStatus(paths)
	if name != defaultServerProfileName || count != 2 {
		t.Fatalf("status = %q/%d, want default/2", name, count)
	}
	pathsV1 := writeTestConfigFile(t, testV1FlatConfig)
	if _, count := selectedServerProfileStatus(pathsV1); count != 1 {
		t.Fatalf("v1 config must count as one profile, got %d", count)
	}
}

func TestRawDefaultProfileLoaderResolvesTokenFileFromV2Config(t *testing.T) {
	// Issue-#200 regression: relay-token storage reads the raw config so an
	// incomplete setup (no relay fields at all) still routes token reads to the
	// configured file instead of hanging in a headless Secret Service unlock.
	// The raw loader must understand the v2 shape WITHOUT the legacy mirror.
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{"schema_version":2,"default_server":"default","servers":{"default":{"relay_token_file":"relay-token"}}}`)

	cfg, err := loadRawDefaultProfileConfig(paths.ConfigFile)
	if err != nil {
		t.Fatalf("loadRawDefaultProfileConfig: %v", err)
	}
	if cfg.RelayTokenFile != "relay-token" {
		t.Fatalf("relay_token_file = %q, want relay-token", cfg.RelayTokenFile)
	}
	if cfg.RelayBaseURL != "" {
		t.Fatalf("raw loader must not require relay fields, got %q", cfg.RelayBaseURL)
	}
	// loadConfig itself refuses this config (no relay_base_url) — the raw
	// loader must stay independent of that check.
	if _, err := loadConfig(paths); err == nil {
		t.Fatal("loadConfig must still fail on an incomplete profile")
	}

	// v1 flat shape stays readable through the same loader.
	pathsV1 := writeTestConfigFile(t, `{"schema_version":1,"relay_token_file":"legacy-token"}`)
	cfgV1, err := loadRawDefaultProfileConfig(pathsV1.ConfigFile)
	if err != nil || cfgV1.RelayTokenFile != "legacy-token" {
		t.Fatalf("v1 raw read = %q err=%v", cfgV1.RelayTokenFile, err)
	}
}

func TestRawDefaultProfileLoaderIgnoresDefaultServerRedirect(t *testing.T) {
	// The legacy token belongs to the LITERAL default profile (the migrated v1
	// install). Pointing default_server at another profile must not make the
	// raw loader read that profile's relay_token_file — the legacy token would
	// travel to the wrong server.
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{"schema_version":2,"default_server":"cabin","servers":{"default":{"relay_token_file":"default-token"},"cabin":{"relay_token_file":"cabin-token","relay_base_url":"http://cabin:8791"}}}`)

	cfg, err := loadRawDefaultProfileConfig(paths.ConfigFile)
	if err != nil {
		t.Fatalf("loadRawDefaultProfileConfig: %v", err)
	}
	if cfg.RelayTokenFile != "default-token" {
		t.Fatalf("relay_token_file = %q, want the literal default profile's default-token", cfg.RelayTokenFile)
	}
}

func TestSetupRejectsAnyNonDefaultServerSelection(t *testing.T) {
	// Setup administers only the default profile: under a named selection the
	// legacy-token flow would retire that profile's device credential while
	// pairing nothing in its place. This holds for fresh installs AND existing
	// named profiles — named profiles are managed via pair --server.
	resetServerProfileSelection(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv(serverSelectionEnvVar, "cabin")
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	// Fresh install.
	if code := runSetup(paths, nil); code == 0 {
		t.Fatal("fresh setup with a non-default server selection must fail loud")
	}
	// Existing named profile: still rejected before the token flow.
	pathsExisting := writeTestConfigFile(t, testV2TwoProfileConfig)
	if code := runSetup(pathsExisting, []string{"--relay-token", "tok", "--non-interactive"}); code == 0 {
		t.Fatal("setup on an existing named profile must fail loud")
	}
}

func TestLegacyMirrorFollowsLiteralDefaultNotDefaultServer(t *testing.T) {
	// The flat mirror pairs with the machine-wide legacy token in old binaries,
	// so it must always carry the LITERAL default profile — never the profile
	// default_server points at.
	resetServerProfileSelection(t)
	doc, err := parseConfigDocument([]byte(`{"schema_version":2,"default_server":"cabin","servers":{"default":{"relay_base_url":"http://home:8791"},"cabin":{"relay_base_url":"http://cabin:8791"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	top, err := doc.withProfile("cabin", runtimeConfig{RelayBaseURL: "http://cabin:8791", HAHost: "cabin.local"})
	if err != nil {
		t.Fatal(err)
	}
	var mirror serverProfileConfig
	raw, _ := json.Marshal(top)
	if err := json.Unmarshal(raw, &mirror); err != nil {
		t.Fatal(err)
	}
	if mirror.RelayBaseURL != "http://home:8791" {
		t.Fatalf("mirror relay_base_url = %q, want the literal default profile's http://home:8791", mirror.RelayBaseURL)
	}

	// Without a literal default profile there is no mirror at all: an old
	// binary honestly reports "not set up yet" instead of pairing the legacy
	// token with a named profile's server.
	fresh := &configDocument{top: map[string]json.RawMessage{}}
	top, err = fresh.withProfile("cabin", runtimeConfig{RelayBaseURL: "http://cabin:8791"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := top["relay_base_url"]; ok {
		t.Fatal("no literal default profile: the flat mirror must be absent")
	}
}
