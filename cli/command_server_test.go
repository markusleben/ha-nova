package main

import (
	"bytes"
	"encoding/json"
	"os"
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

func TestServerRemoveRequiresTypedNameConfirmation(t *testing.T) {
	cases := []struct {
		name  string
		stdin string
	}{
		{"wrong name typed", "nope\n"},
		{"stdin closed", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			paths := setupServerCommandTest(t, testV2TwoProfileConfig)
			if err := secretSet(deviceCredentialServiceForProfile("cabin"), testProfileCredentialB); err != nil {
				t.Fatal(err)
			}
			revokedAt := stubServerRevoke(t)
			stubServerCommandStdin(t, tc.stdin)
			before, _ := os.ReadFile(paths.ConfigFile)

			exit, out := captureCommandOutput(t, func() int { return runServerCommand(paths, []string{"remove", "cabin"}) })
			if exit != 1 {
				t.Fatalf("exit = %d, want 1\n%s", exit, out)
			}
			if len(*revokedAt) != 0 {
				t.Fatalf("nothing may be revoked without a matching confirmation, got %v", *revokedAt)
			}
			if _, ok, err := readCredentialSlot(deviceCredentialServiceForProfile("cabin")); err != nil || !ok {
				t.Fatalf("cabin slot must survive a failed confirmation: ok=%v err=%v", ok, err)
			}
			after, _ := os.ReadFile(paths.ConfigFile)
			if string(before) != string(after) {
				t.Fatal("a failed confirmation must not touch config.json")
			}
		})
	}
}

func TestServerRemoveRevokesDeletesAndResetsDefault(t *testing.T) {
	config := strings.Replace(testV2TwoProfileConfig, `"default_server": "default"`, `"default_server": "cabin"`, 1)
	paths := setupServerCommandTest(t, config)
	if err := secretSet(deviceCredentialServiceForProfile("cabin"), testProfileCredentialB); err != nil {
		t.Fatal(err)
	}
	if err := secretSet(deviceCredentialPendingServiceForProfile("cabin"), testProfileCredentialB); err != nil {
		t.Fatal(err)
	}
	revokedAt := stubServerRevoke(t)
	stubServerCommandStdin(t, "cabin\n")
	topBefore := readTestConfigTopLevel(t, paths)
	var serversBefore map[string]json.RawMessage
	if err := json.Unmarshal(topBefore["servers"], &serversBefore); err != nil {
		t.Fatal(err)
	}

	exit, out := captureCommandOutput(t, func() int { return runServerCommand(paths, []string{"remove", "cabin"}) })
	if exit != 0 {
		t.Fatalf("remove exit = %d, want 0\n%s", exit, out)
	}
	// The revoke went to CABIN's pinned endpoint (from the test config).
	if len(*revokedAt) != 1 || (*revokedAt)[0] != "https://cabin:18792" {
		t.Fatalf("revoke endpoints = %v, want [https://cabin:18792]", *revokedAt)
	}
	for _, service := range []string{deviceCredentialServiceForProfile("cabin"), deviceCredentialPendingServiceForProfile("cabin")} {
		if _, ok, _ := readCredentialSlot(service); ok {
			t.Fatalf("slot %s must be deleted", service)
		}
	}
	top := readTestConfigTopLevel(t, paths)
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(top["servers"], &servers); err != nil {
		t.Fatal(err)
	}
	if _, ok := servers["cabin"]; ok {
		t.Fatal("servers entry must be gone")
	}
	if compactTestJSON(t, servers["default"]) != compactTestJSON(t, serversBefore["default"]) {
		t.Fatalf("sibling profile changed by remove:\n before: %s\n after:  %s", serversBefore["default"], servers["default"])
	}
	if string(top["default_server"]) != `"default"` {
		t.Fatalf("default_server = %s, want reset to \"default\"", top["default_server"])
	}
}

func TestServerRemoveRefusalRules(t *testing.T) {
	t.Run("default with other profiles", func(t *testing.T) {
		paths := setupServerCommandTest(t, testV2TwoProfileConfig)
		stubServerCommandStdin(t, "default\n")
		exit, out := captureCommandOutput(t, func() int { return runServerCommand(paths, []string{"remove", "default"}) })
		if exit != 1 || !strings.Contains(out, "cannot be removed while other server profiles exist") {
			t.Fatalf("exit = %d, output:\n%s", exit, out)
		}
	})
	t.Run("default as only profile points at uninstall", func(t *testing.T) {
		paths := setupServerCommandTest(t, `{"schema_version":2,"default_server":"default","servers":{"default":{"ha_host":"ha","ha_url":"http://ha:8123","relay_base_url":"http://ha:8791"}}}`)
		stubServerCommandStdin(t, "default\n")
		exit, out := captureCommandOutput(t, func() int { return runServerCommand(paths, []string{"remove", "default"}) })
		if exit != 1 || !strings.Contains(out, "ha-nova uninstall") {
			t.Fatalf("exit = %d, output:\n%s", exit, out)
		}
	})
	t.Run("v1 flat config counts as only-default", func(t *testing.T) {
		paths := setupServerCommandTest(t, testV1FlatConfig)
		stubServerCommandStdin(t, "default\n")
		exit, out := captureCommandOutput(t, func() int { return runServerCommand(paths, []string{"remove", "default"}) })
		if exit != 1 || !strings.Contains(out, "ha-nova uninstall") {
			t.Fatalf("exit = %d, output:\n%s", exit, out)
		}
	})
	t.Run("default_server without literal default fallback", func(t *testing.T) {
		paths := setupServerCommandTest(t, `{"schema_version":2,"default_server":"cabin","servers":{"cabin":{"relay_base_url":"http://cabin:8791"},"lake":{"relay_base_url":"http://lake:8791"}}}`)
		stubServerCommandStdin(t, "cabin\n")
		before, _ := os.ReadFile(paths.ConfigFile)
		exit, out := captureCommandOutput(t, func() int { return runServerCommand(paths, []string{"remove", "cabin"}) })
		if exit != 1 || !strings.Contains(out, "ha-nova server default") || !strings.Contains(out, "lake") {
			t.Fatalf("exit = %d, output:\n%s", exit, out)
		}
		after, _ := os.ReadFile(paths.ConfigFile)
		if string(before) != string(after) {
			t.Fatal("the refusal must run before any change")
		}
	})
}

func TestServerCommandUsageAndHelp(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)

	exit, out := captureCommandOutput(t, func() int { return runServerCommand(paths, nil) })
	if exit != 1 {
		t.Fatalf("bare server exit = %d, want 1", exit)
	}
	for _, want := range []string{"list", "default <name>", "rename <old> <new>", "remove <name>"} {
		if !strings.Contains(out, want) {
			t.Fatalf("usage missing %q:\n%s", want, out)
		}
	}

	helpCases := [][]string{
		{"--help"},
		{"list", "--help"},
		{"default", "-h"},
		{"rename", "--help"},
		{"remove", "--help"},
	}
	for _, args := range helpCases {
		exit, out := captureCommandOutput(t, func() int { return runServerCommand(paths, args) })
		if exit != 0 || !strings.Contains(out, "Usage: ha-nova server") {
			t.Fatalf("server %v help exit = %d, output:\n%s", args, exit, out)
		}
	}

	exit, out = captureCommandOutput(t, func() int { return runServerCommand(paths, []string{"bogus"}) })
	if exit != 1 || !strings.Contains(out, "unknown server subcommand") {
		t.Fatalf("bogus subcommand exit = %d, output:\n%s", exit, out)
	}

	if !strings.Contains(captureStdout(t, printUsage), "ha-nova server <list|default|rename|remove>") {
		t.Fatal("global usage must list the server command")
	}
}
