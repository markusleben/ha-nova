package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// Regression: `ha-nova pair --relay-url X` on an existing config must persist X,
// not just use it for this one pairing while leaving the old bootstrap URL in
// config.json (which would drive later functional calls to the wrong host).
func TestRunPairCommandPersistsRelayURLFlagOnExistingConfig(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.json")
	paths := runtimePaths{ConfigDir: dir, ConfigFile: configFile}
	if err := saveConfig(paths, runtimeConfig{RelayBaseURL: "http://old:8791"}); err != nil {
		t.Fatal(err)
	}

	var seededURL string
	orig := runSecurePairingForPairCmd
	runSecurePairingForPairCmd = func(bootstrapURL, code string, cfg *runtimeConfig, saveCfg func(*runtimeConfig) error, info pairingClientInfo) (string, error) {
		seededURL = cfg.RelayBaseURL
		_ = saveCfg(cfg) // the real flow persists cfg at the end of pairing
		return "dev-1", nil
	}
	defer func() { runSecurePairingForPairCmd = orig }()

	if rc := runPairCommand(paths, []string{"--relay-url", "http://new:8791", "--code", "123456"}); rc != 0 {
		t.Fatalf("runPairCommand rc=%d, want 0", rc)
	}
	if seededURL != "http://new:8791" {
		t.Fatalf("explicit --relay-url not seeded into cfg: got %q", seededURL)
	}

	saved, err := loadConfig(paths)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if saved.RelayBaseURL != "http://new:8791" {
		t.Fatalf("persisted config kept the old URL: got %q", saved.RelayBaseURL)
	}
}

func TestRunPairCommandRejectsCloudBeforeMigrationOrPrompt(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := pendingCloudOnlyCommandConfig(cloudStateAuthorizing)
	cfg.RelayBaseURL = "http://relay:8791"
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	current := validCredential(28)
	if err := writeDeviceCredential(current); err != nil {
		t.Fatal(err)
	}

	exit, output := captureCommandOutput(t, func() int {
		return runPairCommand(
			paths,
			[]string{"--credential-store", "file"},
		)
	})
	if exit != 1 ||
		!strings.Contains(output, "Cloud access is configured") ||
		strings.Contains(output, "Six-digit code") {
		t.Fatalf("Cloud-guarded pair exit=%d output=%s", exit, output)
	}
	if deviceFileBackendMarkerExists() {
		t.Fatal("blocked pair changed the credential backend")
	}
	got, exists, err := readDeviceCredential()
	if err != nil || !exists || got != current {
		t.Fatalf(
			"blocked pair migrated credential=%q exists=%v err=%v",
			got,
			exists,
			err,
		)
	}
}
