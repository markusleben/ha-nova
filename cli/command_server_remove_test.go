package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `ha-nova server remove` coverage — split from command_server_test.go per
// the <~400 LOC file guideline.

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
	var remaining serverProfileConfig
	if err := json.Unmarshal(servers["default"], &remaining); err != nil {
		t.Fatal(err)
	}
	if remaining.RelayBaseURL != "http://ha:8791" || remaining.ProfileID == "" || remaining.RoutePolicy != routePolicyLocal {
		t.Fatalf("sibling profile lost data during v3 migration: before=%s after=%s", serversBefore["default"], servers["default"])
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
	t.Run("only named profile points at uninstall", func(t *testing.T) {
		// Multi-server-first install: pair --server cabin before any literal
		// default exists. The last profile routes to uninstall regardless of
		// its name — it must never be unremovable.
		paths := setupServerCommandTest(t, `{"schema_version":2,"default_server":"cabin","servers":{"cabin":{"relay_base_url":"http://cabin:8791"}}}`)
		stubServerCommandStdin(t, "cabin\n")
		exit, out := captureCommandOutput(t, func() int { return runServerCommand(paths, []string{"remove", "cabin"}) })
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

	if !strings.Contains(captureStdout(t, printUsage), "ha-nova server <list|default|rename|remove|route>") {
		t.Fatal("global usage must list the server command")
	}
}

func TestServerRemovePreflightsSecureStorageBeforeConfirmation(t *testing.T) {
	// Locked/unreachable secure storage must abort BEFORE the confirmation and
	// before any config change — otherwise the profile entry disappears while
	// its credentials stay stranded under an unselectable name.
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", "")
	prevPreflight := deviceCredentialPreflight
	deviceCredentialPreflight = func() error { return errDesktopKeyringSessionUnavailable }
	t.Cleanup(func() { deviceCredentialPreflight = prevPreflight })
	stubServerCommandStdin(t, "cabin\n")
	before, _ := os.ReadFile(paths.ConfigFile)

	exit, out := captureCommandOutput(t, func() int { return runServerCommand(paths, []string{"remove", "cabin"}) })
	if exit != 1 || !strings.Contains(out, "secure storage is not reachable") {
		t.Fatalf("exit = %d, output:\n%s", exit, out)
	}
	after, _ := os.ReadFile(paths.ConfigFile)
	if string(before) != string(after) {
		t.Fatal("the preflight abort must not touch config.json")
	}
}

func TestServerRemoveDeletesMalformedCredentialSlot(t *testing.T) {
	// A corrupted stored value must not block removal: the preflight checks
	// reachability only, and the purge deletes the slot without parsing.
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	if err := secretSet(deviceCredentialServiceForProfile("cabin"), "garbage-not-a-credential"); err != nil {
		t.Fatal(err)
	}
	stubServerRevoke(t)
	stubServerCommandStdin(t, "cabin\n")

	exit, out := captureCommandOutput(t, func() int { return runServerCommand(paths, []string{"remove", "cabin"}) })
	if exit != 0 {
		t.Fatalf("remove exit = %d, want 0\n%s", exit, out)
	}
	if _, err := secretGet(deviceCredentialServiceForProfile("cabin")); err != errSecretNotFound {
		t.Fatalf("malformed slot must be deleted, got err=%v", err)
	}
}

func TestServerRemoveHeadlessRetainsMarkerlessPendingFileAndConfig(t *testing.T) {
	// A markerless pending file does not prove that no keyring slot exists.
	// Removing the profile while the keyring is unreachable could therefore
	// strand a credential under a profile name that no longer exists.
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", "")
	prevPreflight := deviceCredentialPreflight
	deviceCredentialPreflight = func() error { return errDesktopKeyringSessionUnavailable }
	t.Cleanup(func() { deviceCredentialPreflight = prevPreflight })
	pendingPath, err := deviceSecretFilePath(deviceCredentialPendingServiceForProfile("cabin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(pendingPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pendingPath, []byte(testProfileCredentialB), 0o600); err != nil {
		t.Fatal(err)
	}
	configBefore, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	stubServerRevoke(t)
	stubServerCommandStdin(t, "cabin\n")

	exit, out := captureCommandOutput(t, func() int { return runServerCommand(paths, []string{"remove", "cabin"}) })
	if exit != 1 {
		t.Fatalf("headless remove exit = %d, want 1\n%s", exit, out)
	}
	if !strings.Contains(out, "nothing was removed") {
		t.Fatalf("headless remove must stop before confirmation:\n%s", out)
	}
	if _, err := os.Lstat(pendingPath); err != nil {
		t.Fatalf("raw pending file was removed, stat err = %v", err)
	}
	configAfter, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(configBefore, configAfter) {
		t.Fatal("server config changed while secure storage was unreachable")
	}
}

func TestServerRemoveRejectsRawCredentialReplacementDuringConfirmation(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", "")
	previousPreflight := deviceCredentialPreflight
	deviceCredentialPreflight = func() error { return nil }
	t.Cleanup(func() { deviceCredentialPreflight = previousPreflight })

	pendingPath, err := deviceSecretFilePath(deviceCredentialPendingServiceForProfile("cabin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(pendingPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pendingPath, []byte("old-credential"), 0o600); err != nil {
		t.Fatal(err)
	}
	beforeConfig, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	revokedAt := stubServerRevoke(t)
	previousConfirm := readServerRemoveConfirmationForCommand
	readServerRemoveConfirmationForCommand = func(string) (string, error) {
		if err := os.WriteFile(pendingPath, []byte("replacement-credential"), 0o600); err != nil {
			t.Fatal(err)
		}
		return "cabin", nil
	}
	t.Cleanup(func() { readServerRemoveConfirmationForCommand = previousConfirm })

	exit, output := captureCommandOutput(t, func() int {
		return runServerCommand(paths, []string{"remove", "cabin"})
	})
	if exit != 1 || !strings.Contains(output, "stored credentials changed while awaiting confirmation") {
		t.Fatalf("remove did not reject credential replacement: exit=%d\n%s", exit, output)
	}
	if len(*revokedAt) != 0 {
		t.Fatalf("replacement credential was revoked: %v", *revokedAt)
	}
	afterConfig, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterConfig) != string(beforeConfig) {
		t.Fatal("config changed after credential identity drift")
	}
	raw, err := os.ReadFile(pendingPath)
	if err != nil || string(raw) != "replacement-credential" {
		t.Fatalf("replacement credential was changed: %q err=%v", raw, err)
	}
}

func TestServerRemoveRejectsCredentialBackendChangeDuringConfirmation(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	revokedAt := stubServerRevoke(t)
	markerPath, err := deviceFileBackendMarkerPath()
	if err != nil {
		t.Fatal(err)
	}
	previousConfirm := readServerRemoveConfirmationForCommand
	readServerRemoveConfirmationForCommand = func(string) (string, error) {
		if err := os.MkdirAll(filepath.Dir(markerPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(markerPath, []byte("file\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return "cabin", nil
	}
	t.Cleanup(func() { readServerRemoveConfirmationForCommand = previousConfirm })

	exit, output := captureCommandOutput(t, func() int {
		return runServerCommand(paths, []string{"remove", "cabin"})
	})
	if exit != 1 || !strings.Contains(output, "stored credentials changed while awaiting confirmation") {
		t.Fatalf("remove did not reject backend change: exit=%d\n%s", exit, output)
	}
	if len(*revokedAt) != 0 {
		t.Fatalf("credential was revoked after backend change: %v", *revokedAt)
	}
	if !fileExists(markerPath) {
		t.Fatal("replacement backend marker was removed")
	}
}
