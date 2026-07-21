package main

import (
	"encoding/json"
	"os"
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

	if !strings.Contains(captureStdout(t, printUsage), "ha-nova server <list|default|rename|remove>") {
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
