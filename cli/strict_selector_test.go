package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestRelayRejectsEmptyServerBeforeEnvironmentFallback(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	t.Setenv("HA_NOVA_SERVER", defaultServerProfileName)

	exitCode, output := captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "ws", []string{"--server=", "--data", `{"type":"ping"}`})
	})
	if exitCode != 1 || !strings.Contains(output, "--server requires a non-empty profile name") || !strings.Contains(output, "nothing was sent") {
		t.Fatalf("exit/output = %d, %q", exitCode, output)
	}
	if got := capture.requests.Load(); got != 0 {
		t.Fatalf("request count = %d, want 0", got)
	}
}

func TestHealthRejectsEmptyServerAndPositionals(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"empty server": {args: []string{"--server="}, want: "--server requires a non-empty profile name"},
		"positional":   {args: []string{"unexpected", "--server", "cabin"}, want: "does not accept positional arguments"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseHealthFlags(tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseHealthFlags() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestPairRejectsExplicitEmptySelectorsBeforeFallback(t *testing.T) {
	original := runSecurePairingForPairCmd
	pairCalls := 0
	runSecurePairingForPairCmd = func(string, string, *runtimeConfig, func(*runtimeConfig) error, pairingClientInfo) (string, error) {
		pairCalls++
		return "unexpected", nil
	}
	t.Cleanup(func() { runSecurePairingForPairCmd = original })

	paths := runtimePaths{ConfigFile: filepath.Join(t.TempDir(), "missing-config.json")}
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"server":    {args: []string{"--server="}, want: "--server requires a non-empty profile name"},
		"relay URL": {args: []string{"--relay-url="}, want: "--relay-url requires a non-empty URL"},
		"code":      {args: []string{"--code="}, want: "--code requires a non-empty pairing code"},
	} {
		t.Run(name, func(t *testing.T) {
			exitCode, output := captureCommandOutput(t, func() int { return runPairCommand(paths, tc.args) })
			if exitCode != 1 || !strings.Contains(output, tc.want) || !strings.Contains(output, "nothing was paired") {
				t.Fatalf("exit/output = %d, %q", exitCode, output)
			}
			if pairCalls != 0 {
				t.Fatalf("pairing calls = %d, want 0", pairCalls)
			}
		})
	}
}

func TestPairPreflightsExplicitInputsBeforeCredentialMigration(t *testing.T) {
	invalidUTF8URL := string(append([]byte("http://relay/"), 0xdc))
	invalidUTF8Server := string([]byte{'c', 'a', 0xdc})
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"invalid UTF-8 URL": {
			args: []string{"--credential-store=file", "--relay-url", invalidUTF8URL, "--code", "123456"},
			want: "relay URL is not valid UTF-8",
		},
		"malformed URL": {
			args: []string{"--credential-store=file", "--relay-url", "http://", "--code", "123456"},
			want: "relay URL",
		},
		"URL with path": {
			args: []string{"--credential-store=file", "--relay-url", "http://relay.test/not-a-base", "--code", "123456"},
			want: "without a path",
		},
		"empty URL query": {
			args: []string{"--credential-store=file", "--relay-url", "http://relay.test:8791?", "--code", "123456"},
			want: "without a path",
		},
		"out-of-range URL port": {
			args: []string{"--credential-store=file", "--relay-url", "http://relay.test:65536", "--code", "123456"},
			want: "port must be between 1 and 65535",
		},
		"empty URL port": {
			args: []string{"--credential-store=file", "--relay-url", "http://relay.test:", "--code", "123456"},
			want: "port must not be empty",
		},
		"invalid decoded host UTF-8": {
			args: []string{"--credential-store=file", "--relay-url", "http://%DC:8791", "--code", "123456"},
			want: "relay URL host is not valid UTF-8",
		},
		"invalid code": {
			args: []string{"--credential-store=file", "--relay-url", "http://relay.test:8791", "--code", "bad"},
			want: "exactly six digits",
		},
		"invalid UTF-8 server": {
			args: []string{"--credential-store=file", "--server", invalidUTF8Server, "--relay-url", "http://relay.test:8791", "--code", "123456"},
			want: "server profile name is not valid UTF-8",
		},
	} {
		t.Run(name, func(t *testing.T) {
			withDeviceStorageTestHome(t)
			resetKeyringDeviceSlots(t)
			keyringCredential := generateTestDeviceCredential(t)
			if err := writeDeviceCredential(keyringCredential); err != nil {
				t.Fatalf("seed keyring credential: %v", err)
			}
			configFile := filepath.Join(t.TempDir(), "config.json")
			if err := saveConfig(runtimePaths{ConfigFile: configFile}, runtimeConfig{RelayBaseURL: "http://saved.test:8791"}); err != nil {
				t.Fatal(err)
			}

			pairCalls := 0
			original := runSecurePairingForPairCmd
			runSecurePairingForPairCmd = func(string, string, *runtimeConfig, func(*runtimeConfig) error, pairingClientInfo) (string, error) {
				pairCalls++
				return "unexpected", nil
			}
			t.Cleanup(func() { runSecurePairingForPairCmd = original })

			exitCode, output := captureCommandOutput(t, func() int {
				return runPairCommand(runtimePaths{ConfigFile: configFile}, tc.args)
			})
			if exitCode != 1 || !strings.Contains(output, tc.want) || !strings.Contains(output, "nothing was paired") {
				t.Fatalf("exit/output = %d, %q", exitCode, output)
			}
			if pairCalls != 0 || deviceCredentialFileModeForced || deviceFileBackendMarkerExists() {
				t.Fatalf("preflight mutated pairing/storage state: calls=%d forced=%v marker=%v", pairCalls, deviceCredentialFileModeForced, deviceFileBackendMarkerExists())
			}
			got, err := keyring.Get(deviceCredentialService, secretUser())
			if err != nil || got != keyringCredential {
				t.Fatalf("keyring credential changed: got=%q err=%v", got, err)
			}
			if deviceSecretFileExists(deviceCredentialService) {
				t.Fatal("credential was copied to file before input validation")
			}
			if _, err := keyring.Get(deviceCredentialPendingService, secretUser()); !errors.Is(err, keyring.ErrNotFound) {
				t.Fatalf("pending keyring slot changed: %v", err)
			}
		})
	}
}

func TestSnapshotRejectsEmptyOrAmbiguousSelectionBeforeNewestFallback(t *testing.T) {
	paths := testSnapshotPaths(t)
	for _, record := range []string{
		`{"op":"update","domain":"automation","target_id":"t1","before_config":{"v":0},"expected_after":{"v":1}}`,
		`{"op":"update","domain":"automation","target_id":"t2","before_config":{"v":1},"expected_after":{"v":2}}`,
	} {
		if err := saveUndoSnapshotBytes(paths, []byte(record)); err != nil {
			t.Fatal(err)
		}
	}
	livePath := filepath.Join(t.TempDir(), "live.json")
	if err := os.WriteFile(livePath, []byte(`{"v":2}`), 0o600); err != nil {
		t.Fatal(err)
	}

	showCases := map[string]struct {
		args []string
		want string
	}{
		"empty target":  {args: []string{"--target="}, want: "--target requires a non-empty target_id"},
		"empty domain":  {args: []string{"--domain="}, want: "--domain requires a non-empty domain"},
		"positional":    {args: []string{"unexpected", "--target", "t1"}, want: "does not accept positional arguments"},
		"list selector": {args: []string{"--list", "--target", "t1"}, want: "--list cannot be combined"},
	}
	for name, tc := range showCases {
		t.Run("show "+name, func(t *testing.T) {
			exitCode, output := captureCommandOutput(t, func() int { return runSnapshotShow(paths, tc.args) })
			if exitCode != 1 || !strings.Contains(output, tc.want) || strings.Contains(output, `"target_id": "t2"`) {
				t.Fatalf("exit/output = %d, %q", exitCode, output)
			}
		})
	}

	verifyCases := map[string]struct {
		args []string
		want string
	}{
		"empty target": {args: []string{"--against", livePath, "--target="}, want: "--target requires a non-empty target_id"},
		"empty domain": {args: []string{"--against", livePath, "--target", "t1", "--domain="}, want: "--domain requires a non-empty domain"},
		"positional":   {args: []string{"--against", livePath, "unexpected", "--target", "t1"}, want: "does not accept positional arguments"},
	}
	for name, tc := range verifyCases {
		t.Run("verify "+name, func(t *testing.T) {
			exitCode, output := captureCommandOutput(t, func() int { return runSnapshotVerify(paths, tc.args) })
			if exitCode != 1 || !strings.Contains(output, tc.want) || strings.TrimSpace(output) == "match" {
				t.Fatalf("exit/output = %d, %q", exitCode, output)
			}
		})
	}
}

func TestSnapshotRejectsDomainWithoutTargetBeforeStoreAccess(t *testing.T) {
	paths := testSnapshotPaths(t)
	for name, run := range map[string]func() int{
		"show": func() int {
			return runSnapshotShow(paths, []string{"--domain", "automation"})
		},
		"verify": func() int {
			return runSnapshotVerify(paths, []string{"--against", filepath.Join(t.TempDir(), "missing.json"), "--domain", "automation"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			exitCode, output := captureCommandOutput(t, run)
			if exitCode != 1 || !strings.Contains(output, "--domain requires --target") || strings.Contains(output, "no undo snapshot") {
				t.Fatalf("exit/output = %d, %q", exitCode, output)
			}
		})
	}
}

func TestDiffRejectsEmptyOutputAndPositionalsBeforeRendering(t *testing.T) {
	dir := t.TempDir()
	beforePath := filepath.Join(dir, "before.json")
	afterPath := filepath.Join(dir, "after.json")
	if err := os.WriteFile(beforePath, []byte(`{"mode":"single"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(afterPath, []byte(`{"mode":"restart"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"empty output": {
			args: []string{"--before", beforePath, "--after", afterPath, "--out="},
			want: "--out requires a non-empty path",
		},
		"positional": {
			args: []string{"--before", beforePath, "--after", afterPath, "unexpected"},
			want: "does not accept positional arguments",
		},
	} {
		t.Run(name, func(t *testing.T) {
			exitCode, output := captureCommandOutput(t, func() int { return runDiffCommand(runtimePaths{}, tc.args) })
			if exitCode != 1 || !strings.Contains(output, tc.want) || strings.Contains(output, "| mode |") {
				t.Fatalf("exit/output = %d, %q", exitCode, output)
			}
		})
	}
}
