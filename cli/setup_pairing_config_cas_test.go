package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteJSONFileIfAbsentSnapshotPreservesConcurrentFile(
	t *testing.T,
) {
	path := filepath.Join(t.TempDir(), "config.json")
	concurrent := []byte("{\"owner\":\"other\"}\n")
	if err := os.WriteFile(path, concurrent, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := writeJSONFileIfAbsentSnapshot(
		path,
		map[string]string{"owner": "setup"},
		0o600,
	); !errors.Is(err, errConditionalJSONConflictRestored) {
		t.Fatalf("absent CAS err=%v, want conflict", err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, concurrent) {
		t.Fatalf("concurrent file changed: %q", current)
	}
}

func TestSetupSecurePairingCreatesAbsentConfigBeforeCode(
	t *testing.T,
) {
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	originalProbe := probePairingV1ForSetup
	originalBrowser := openBrowserForSetup
	probePairingV1ForSetup = func(string) bool { return true }
	openBrowserForSetup = func(string) error { return nil }
	t.Cleanup(func() {
		probePairingV1ForSetup = originalProbe
		openBrowserForSetup = originalBrowser
	})

	dir := t.TempDir()
	paths := runtimePaths{
		ConfigDir:  dir,
		ConfigFile: filepath.Join(dir, "config.json"),
	}
	cfg := &runtimeConfig{
		HAURL:        "http://ha:8123",
		RelayBaseURL: "http://relay:8791",
	}
	snapshot, err := readSetupConfigSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot) != 1 || snapshot[0] != 0 {
		t.Fatalf("fresh config snapshot=%v", snapshot)
	}
	lifecycle := [][]byte{nil, nil, snapshot}

	_, err = runSetupPairingFlow(
		bufio.NewReader(strings.NewReader("\nmanual\n")),
		io.Discard,
		paths,
		cfg,
		false,
		lifecycle...,
	)
	if !errors.Is(err, errSetupRelayTokenStep) {
		t.Fatalf("fresh setup err=%v, want manual token step", err)
	}
	saved, loadErr := loadConfig(paths)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if saved.ClientInstallID == "" ||
		saved.RelayBaseURL != cfg.RelayBaseURL {
		t.Fatalf("fresh setup config=%+v", saved)
	}
	if len(lifecycle[2]) < 2 || lifecycle[2][0] != 1 {
		t.Fatalf("committed config snapshot=%v", lifecycle[2])
	}
}

func TestSetupSecurePairingRejectsConfigDriftBeforeSuccessfulSave(
	t *testing.T,
) {
	originalProbe := probePairingV1ForSetup
	originalPair := securePairForSetup
	originalBrowser := openBrowserForSetup
	probePairingV1ForSetup = func(string) bool { return true }
	openBrowserForSetup = func(string) error { return nil }
	t.Cleanup(func() {
		probePairingV1ForSetup = originalProbe
		securePairForSetup = originalPair
		openBrowserForSetup = originalBrowser
	})

	dir := t.TempDir()
	paths := runtimePaths{
		ConfigDir:  dir,
		ConfigFile: filepath.Join(dir, "config.json"),
	}
	cfg := &runtimeConfig{RelayBaseURL: "http://relay:8791"}
	if err := saveConfig(paths, *cfg); err != nil {
		t.Fatal(err)
	}
	snapshot, err := readSetupConfigSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := [][]byte{nil, nil, snapshot}
	securePairForSetup = func(
		_, _ string,
		cfg *runtimeConfig,
		save func(*runtimeConfig) error,
		_ pairingClientInfo,
	) (string, error) {
		concurrent := *cfg
		concurrent.RelayBaseURL = "http://concurrent:8791"
		if err := saveConfig(paths, concurrent); err != nil {
			t.Fatal(err)
		}
		cfg.PendingSecureBaseURL = "https://relay:8792"
		cfg.PendingSpkiPin = strings.Repeat("A", 43)
		if err := save(cfg); err != nil {
			return "", err
		}
		return "device-id", nil
	}

	_, err = runSetupPairingFlow(
		bufio.NewReader(strings.NewReader("\n473921\n")),
		io.Discard,
		paths,
		cfg,
		false,
		lifecycle...,
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"server configuration changed during setup",
		) {
		t.Fatalf("pairing drift err=%v", err)
	}
	saved, loadErr := loadConfig(paths)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if saved.RelayBaseURL != "http://concurrent:8791" ||
		saved.PendingSecureBaseURL != "" ||
		saved.RelaySecureBaseURL != "" {
		t.Fatalf("concurrent config was overwritten: %+v", saved)
	}
}

func TestSetupInstallIDCommitRejectsEveryConfigRaceWindow(
	t *testing.T,
) {
	for _, testCase := range []struct {
		name        string
		installHook func(func(string))
		restoreHook func()
	}{
		{
			name: "before conditional swap",
			installHook: func(hook func(string)) {
				conditionalJSONBeforeSwap = hook
			},
			restoreHook: func() {
				conditionalJSONBeforeSwap = func(string) {}
			},
		},
		{
			name: "after committed marker retirement",
			installHook: func(hook func(string)) {
				conditionalJSONAfterMarkerRetirement = func(
					path string,
				) error {
					hook(path)
					return nil
				}
			},
			restoreHook: func() {
				conditionalJSONAfterMarkerRetirement = func(
					string,
				) error {
					return nil
				}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			originalProbe := probePairingV1ForSetup
			originalPair := securePairForSetup
			originalBrowser := openBrowserForSetup
			originalBeforeSwap := conditionalJSONBeforeSwap
			originalAfterRetirement :=
				conditionalJSONAfterMarkerRetirement
			probePairingV1ForSetup = func(string) bool { return true }
			openBrowserForSetup = func(string) error { return nil }
			pairCalls := 0
			securePairForSetup = func(
				_, _ string,
				_ *runtimeConfig,
				_ func(*runtimeConfig) error,
				_ pairingClientInfo,
			) (string, error) {
				pairCalls++
				return "device-id", nil
			}
			t.Cleanup(func() {
				probePairingV1ForSetup = originalProbe
				securePairForSetup = originalPair
				openBrowserForSetup = originalBrowser
				conditionalJSONBeforeSwap = originalBeforeSwap
				conditionalJSONAfterMarkerRetirement =
					originalAfterRetirement
			})

			dir := t.TempDir()
			paths := runtimePaths{
				ConfigDir:  dir,
				ConfigFile: filepath.Join(dir, "config.json"),
			}
			cfg := &runtimeConfig{
				RelayBaseURL: "http://relay:8791",
			}
			if err := saveConfig(paths, *cfg); err != nil {
				t.Fatal(err)
			}
			source, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			concurrent := bytes.ReplaceAll(
				source,
				[]byte("http://relay:8791"),
				[]byte("http://concurrent:8791"),
			)
			snapshot, err := readSetupConfigSnapshot(paths)
			if err != nil {
				t.Fatal(err)
			}
			lifecycle := [][]byte{nil, nil, snapshot}
			fired := false
			testCase.installHook(func(path string) {
				if fired || path != paths.ConfigFile {
					return
				}
				fired = true
				testCase.restoreHook()
				if err := os.WriteFile(
					paths.ConfigFile,
					concurrent,
					0o600,
				); err != nil {
					t.Fatal(err)
				}
			})

			_, err = runSetupPairingFlow(
				bufio.NewReader(strings.NewReader("")),
				io.Discard,
				paths,
				cfg,
				false,
				lifecycle...,
			)
			if !fired ||
				err == nil ||
				!strings.Contains(
					err.Error(),
					"server configuration changed during setup",
				) {
				t.Fatalf("install-ID race fired=%v err=%v", fired, err)
			}
			if pairCalls != 0 {
				t.Fatalf("pair calls=%d, want 0", pairCalls)
			}
			saved, loadErr := loadConfig(paths)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if saved.RelayBaseURL != "http://concurrent:8791" ||
				saved.ClientInstallID != "" {
				t.Fatalf("race generation was not preserved: %+v", saved)
			}
		})
	}
}
