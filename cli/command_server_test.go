package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `ha-nova server` coverage: list markers, default switching, rename (config
// entry + credential slots + default_server pointer), remove (typed-name
// confirmation, per-profile revoke, default_server reset), and the refusal
// rules protecting the literal default profile.

const testV2ThreeProfileConfig = `{
  "schema_version": 2,
  "default_server": "default",
  "client_install_id": "inst-abc",
  "ha_host": "ha",
  "ha_url": "http://ha:8123",
  "relay_base_url": "http://ha:8791",
  "servers": {
    "default": {"ha_host": "ha", "ha_url": "http://ha:8123", "relay_base_url": "http://ha:8791"},
    "cabin": {"ha_host": "cabin", "ha_url": "http://cabin:8123", "relay_base_url": "http://cabin:8791", "relay_secure_base_url": "https://cabin:18792", "relay_spki_pin": "PINB"},
    "lake": {"ha_host": "lake", "ha_url": "http://lake:8123", "relay_base_url": "http://lake:8791"}
  }
}`

func setupServerCommandTest(t *testing.T, config string) runtimePaths {
	t.Helper()
	resetServerProfileSelection(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	return writeTestConfigFile(t, config)
}

// stubServerCommandStdin feeds input to os.Stdin; an empty input yields an
// immediately closed (EOF) stdin.
func stubServerCommandStdin(t *testing.T, input string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if input != "" {
		if _, err := w.WriteString(input); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()
	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = oldStdin })
}

func stubServerRevoke(t *testing.T) *[]string {
	t.Helper()
	var revokedAt []string
	original := revokeSelfDeviceV1ForUninstall
	revokeSelfDeviceV1ForUninstall = func(base, _, _ string) error {
		revokedAt = append(revokedAt, base)
		return nil
	}
	t.Cleanup(func() { revokeSelfDeviceV1ForUninstall = original })
	return &revokedAt
}

// compactTestJSON normalizes whitespace: the document save path re-indents
// raw sibling entries without changing their content.
func compactTestJSON(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		t.Fatalf("compact %s: %v", raw, err)
	}
	return buf.String()
}

func serverListRow(t *testing.T, output, name string) []string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == name {
			return fields
		}
	}
	t.Fatalf("list output has no row for %q:\n%s", name, output)
	return nil
}

func TestServerListShowsProfilesPairedAndMarkers(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	if err := secretSet(deviceCredentialServiceForProfile("cabin"), testProfileCredentialB); err != nil {
		t.Fatal(err)
	}

	exit, out := captureCommandOutput(t, func() int { return runServerCommand(paths, []string{"list"}) })
	if exit != 0 {
		t.Fatalf("list exit = %d, want 0\n%s", exit, out)
	}
	defaultRow := serverListRow(t, out, "default")
	if defaultRow[1] != "ha" || defaultRow[2] != "http://ha:8791" || defaultRow[3] != "no" {
		t.Fatalf("default row = %v", defaultRow)
	}
	if !strings.Contains(strings.Join(defaultRow, " "), "default") || len(defaultRow) < 5 {
		t.Fatalf("default row must carry the default marker: %v", defaultRow)
	}
	cabinRow := serverListRow(t, out, "cabin")
	if cabinRow[2] != "http://cabin:8791" || cabinRow[3] != "yes" {
		t.Fatalf("cabin row = %v", cabinRow)
	}
	if len(cabinRow) > 4 {
		t.Fatalf("cabin row must carry no markers without a selection: %v", cabinRow)
	}

	// An explicit selection shows the active marker on the selected profile.
	t.Setenv(serverSelectionEnvVar, "cabin")
	exit, out = captureCommandOutput(t, func() int { return runServerCommand(paths, []string{"list"}) })
	if exit != 0 {
		t.Fatalf("list exit = %d, want 0\n%s", exit, out)
	}
	cabinRow = serverListRow(t, out, "cabin")
	if !strings.Contains(strings.Join(cabinRow, " "), "active") {
		t.Fatalf("cabin row must carry the active marker under HA_NOVA_SERVER=cabin: %v", cabinRow)
	}
}

func TestServerDefaultSwitchesAndKeepsLiteralDefaultMirror(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	topBefore := readTestConfigTopLevel(t, paths)
	var serversBefore map[string]json.RawMessage
	if err := json.Unmarshal(topBefore["servers"], &serversBefore); err != nil {
		t.Fatal(err)
	}

	if exit := runServerCommand(paths, []string{"default", "cabin"}); exit != 0 {
		t.Fatalf("default cabin exit = %d, want 0", exit)
	}
	top := readTestConfigTopLevel(t, paths)
	if string(top["default_server"]) != `"cabin"` {
		t.Fatalf("default_server = %s, want \"cabin\"", top["default_server"])
	}
	// The legacy mirror stays the LITERAL default profile's data.
	if string(top["relay_base_url"]) != `"http://ha:8791"` {
		t.Fatalf("mirror relay_base_url = %s, want the literal default's http://ha:8791", top["relay_base_url"])
	}
	var serversAfter map[string]json.RawMessage
	if err := json.Unmarshal(top["servers"], &serversAfter); err != nil {
		t.Fatal(err)
	}
	for name := range serversBefore {
		if compactTestJSON(t, serversAfter[name]) != compactTestJSON(t, serversBefore[name]) {
			t.Fatalf("profile %q changed by server default:\n before: %s\n after:  %s", name, serversBefore[name], serversAfter[name])
		}
	}
}

func TestServerDefaultUnknownNameFailsListingProfiles(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	before, _ := os.ReadFile(paths.ConfigFile)

	exit, out := captureCommandOutput(t, func() int { return runServerCommand(paths, []string{"default", "nope"}) })
	if exit != 1 {
		t.Fatalf("default nope exit = %d, want 1", exit)
	}
	if !strings.Contains(out, `"nope"`) || !strings.Contains(out, "cabin, default") {
		t.Fatalf("error must name the typo and list known profiles:\n%s", out)
	}
	after, _ := os.ReadFile(paths.ConfigFile)
	if string(before) != string(after) {
		t.Fatal("a failed default switch must not touch config.json")
	}
}

func TestServerRenameMovesEntrySlotsAndDefaultPointer(t *testing.T) {
	config := strings.Replace(testV2TwoProfileConfig, `"default_server": "default"`, `"default_server": "cabin"`, 1)
	paths := setupServerCommandTest(t, config)
	if err := secretSet(deviceCredentialServiceForProfile("cabin"), testProfileCredentialB); err != nil {
		t.Fatal(err)
	}
	if err := secretSet(deviceCredentialPendingServiceForProfile("cabin"), testProfileCredentialB); err != nil {
		t.Fatal(err)
	}
	topBefore := readTestConfigTopLevel(t, paths)
	var serversBefore map[string]json.RawMessage
	if err := json.Unmarshal(topBefore["servers"], &serversBefore); err != nil {
		t.Fatal(err)
	}

	if exit := runServerCommand(paths, []string{"rename", "cabin", "seaside"}); exit != 0 {
		t.Fatalf("rename exit = %d, want 0", exit)
	}

	top := readTestConfigTopLevel(t, paths)
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(top["servers"], &servers); err != nil {
		t.Fatal(err)
	}
	if _, ok := servers["cabin"]; ok {
		t.Fatal("old servers entry must be gone")
	}
	if compactTestJSON(t, servers["seaside"]) != compactTestJSON(t, serversBefore["cabin"]) {
		t.Fatalf("renamed entry must keep the profile data unchanged:\n old: %s\n new: %s", serversBefore["cabin"], servers["seaside"])
	}
	if string(top["default_server"]) != `"seaside"` {
		t.Fatalf("default_server = %s, want \"seaside\"", top["default_server"])
	}
	// The mirror still carries the literal default profile.
	if string(top["relay_base_url"]) != `"http://ha:8791"` {
		t.Fatalf("mirror relay_base_url = %s", top["relay_base_url"])
	}
	// Both slots moved: present under the new name, gone under the old.
	for _, service := range []string{deviceCredentialServiceForProfile("seaside"), deviceCredentialPendingServiceForProfile("seaside")} {
		if got, ok, err := readCredentialSlot(service); err != nil || !ok || got != testProfileCredentialB {
			t.Fatalf("slot %s = %q ok=%v err=%v", service, got, ok, err)
		}
	}
	for _, service := range []string{deviceCredentialServiceForProfile("cabin"), deviceCredentialPendingServiceForProfile("cabin")} {
		if _, ok, _ := readCredentialSlot(service); ok {
			t.Fatalf("old slot %s must be gone", service)
		}
	}
}

func TestServerRenameRefusals(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{"literal default", []string{"rename", "default", "home"}, "cannot be renamed"},
		{"to default", []string{"rename", "cabin", "default"}, "reserved for the legacy-token profile"},
		{"unknown old", []string{"rename", "nope", "home"}, "unknown server profile"},
		{"to existing", []string{"rename", "cabin", "lake"}, "already exists"},
		{"invalid new", []string{"rename", "cabin", "Bad_Name"}, "invalid server profile name"},
		{"reserved new", []string{"rename", "cabin", "pending"}, "reserved"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			paths := setupServerCommandTest(t, testV2ThreeProfileConfig)
			if err := secretSet(deviceCredentialServiceForProfile("cabin"), testProfileCredentialB); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(paths.ConfigFile)

			exit, out := captureCommandOutput(t, func() int { return runServerCommand(paths, tc.args) })
			if exit != 1 {
				t.Fatalf("exit = %d, want 1\n%s", exit, out)
			}
			if !strings.Contains(out, tc.wantMsg) {
				t.Fatalf("output missing %q:\n%s", tc.wantMsg, out)
			}
			after, _ := os.ReadFile(paths.ConfigFile)
			if string(before) != string(after) {
				t.Fatal("a refused rename must not touch config.json")
			}
			if got, ok, err := readCredentialSlot(deviceCredentialServiceForProfile("cabin")); err != nil || !ok || got != testProfileCredentialB {
				t.Fatalf("cabin slot touched by refused rename: %q ok=%v err=%v", got, ok, err)
			}
		})
	}
}

func TestServerRenameMovesRawPendingFileWithoutMarker(t *testing.T) {
	// An interrupted explicit file pairing (pair --server cabin
	// --credential-store=file) leaves a pending FILE before the machine-wide
	// marker exists; the routed slot read cannot see it. Rename must move the
	// raw file so a later resume can still activate the consumed pairing.
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	oldPath, err := deviceSecretFilePath(deviceCredentialPendingServiceForProfile("cabin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte(testProfileCredentialB), 0o600); err != nil {
		t.Fatal(err)
	}

	if exit := runServerCommand(paths, []string{"rename", "cabin", "seaside"}); exit != 0 {
		t.Fatalf("rename exit = %d, want 0", exit)
	}
	if _, err := os.Lstat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old raw pending file must be gone, stat err = %v", err)
	}
	newPath, err := deviceSecretFilePath(deviceCredentialPendingServiceForProfile("seaside"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(newPath)
	if err != nil || strings.TrimSpace(string(data)) != testProfileCredentialB {
		t.Fatalf("raw pending file not moved: err=%v data=%q", err, data)
	}
}

func TestServerRenameHeadlessKeepsMarkerlessPendingFile(t *testing.T) {
	// The REAL headless path (no test secret dir): keyring preflight errors,
	// only a markerless raw pending file exists. Rename must succeed, move the
	// file, and warn about the unreachable routed layer.
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", "")
	prevPreflight := deviceCredentialPreflight
	deviceCredentialPreflight = func() error { return errDesktopKeyringSessionUnavailable }
	t.Cleanup(func() { deviceCredentialPreflight = prevPreflight })

	oldPath, err := deviceSecretFilePath(deviceCredentialPendingServiceForProfile("cabin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte(testProfileCredentialB), 0o600); err != nil {
		t.Fatal(err)
	}

	exit, out := captureCommandOutput(t, func() int { return runServerCommand(paths, []string{"rename", "cabin", "seaside"}) })
	if exit != 0 {
		t.Fatalf("headless rename exit = %d, want 0\n%s", exit, out)
	}
	if !strings.Contains(out, "not reachable") {
		t.Fatalf("headless rename must warn about the unreachable routed layer:\n%s", out)
	}
	newPath, err := deviceSecretFilePath(deviceCredentialPendingServiceForProfile("seaside"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(newPath); err != nil {
		t.Fatalf("raw pending file not moved: %v", err)
	}
}

func TestServerListLabelsLegacyTokenInstalls(t *testing.T) {
	// gsgxnet's scenario (issue #419): a working pre-pairing install shows
	// "PAIRED no", which reads as broken. The actual state — connected via the
	// shared legacy token, no device credential yet — must be labeled.
	paths := setupServerCommandTest(t, `{"schema_version":1,"ha_host":"ha","ha_url":"http://ha:8123","relay_base_url":"http://ha:8791"}`)
	exit, out := captureCommandOutput(t, func() int { return runServerCommand(paths, []string{"list"}) })
	if exit != 0 {
		t.Fatalf("list exit = %d\n%s", exit, out)
	}
	if !strings.Contains(out, "no (legacy token)") {
		t.Fatalf("legacy-token install must be labeled, got:\n%s", out)
	}
}

func TestSetupAlreadyDoneBannerOffersPairingSwitchForLegacyInstalls(t *testing.T) {
	// The already-done screen must not dead-end the pairing upgrade: doctor
	// sends legacy users to setup, so setup must name the switch (issue #419).
	var out strings.Builder
	renderSetupAlreadyDoneBanner(&out, true)
	for _, want := range []string{"shared legacy token", "ha-nova pair", "Connect a device"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("legacy banner must contain %q, got:\n%s", want, out.String())
		}
	}
	out.Reset()
	renderSetupAlreadyDoneBanner(&out, false)
	if strings.Contains(out.String(), "legacy token") {
		t.Fatalf("paired installs must not see the legacy hint:\n%s", out.String())
	}
}

func TestServerListLegacyLabelOnlyOnDefaultProfile(t *testing.T) {
	// Named profiles are device-credential-only: a half-paired named profile
	// (relay URL saved, no credential yet) must show a bare "no", never the
	// legacy-token label that only the default profile can earn.
	paths := setupServerCommandTest(t, `{"schema_version":2,"default_server":"default","servers":{"default":{"relay_base_url":"http://ha:8791"},"cabin":{"relay_base_url":"http://cabin:8791"}}}`)
	exit, out := captureCommandOutput(t, func() int { return runServerCommand(paths, []string{"list"}) })
	if exit != 0 {
		t.Fatalf("list exit = %d\n%s", exit, out)
	}
	row := serverListRow(t, out, "cabin")
	joined := strings.Join(row, " ")
	if strings.Contains(joined, "legacy") {
		t.Fatalf("named profile must not carry the legacy-token label: %v", row)
	}
	if !strings.Contains(strings.Join(serverListRow(t, out, "default"), " "), "legacy") {
		t.Fatalf("default profile must carry the legacy-token label:\n%s", out)
	}
}

func TestServerListLegacyLabelSurvivesUnreachableKeyring(t *testing.T) {
	// Headless legacy install (issue #419 follow-up): the device-credential
	// slot read may fail, but a legacy config has no meaningful device
	// credential anyway — the label must come from the config, not from
	// secure-storage reachability.
	paths := setupServerCommandTest(t, `{"schema_version":1,"ha_host":"ha","ha_url":"http://ha:8123","relay_base_url":"http://ha:8791"}`)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", "")
	prevPreflight := deviceCredentialPreflight
	deviceCredentialPreflight = func() error { return errDesktopKeyringSessionUnavailable }
	t.Cleanup(func() { deviceCredentialPreflight = prevPreflight })

	exit, out := captureCommandOutput(t, func() int { return runServerCommand(paths, []string{"list"}) })
	if exit != 0 {
		t.Fatalf("list exit = %d\n%s", exit, out)
	}
	if !strings.Contains(out, "no (legacy token)") {
		t.Fatalf("legacy label must not degrade to unknown on headless installs:\n%s", out)
	}
}
