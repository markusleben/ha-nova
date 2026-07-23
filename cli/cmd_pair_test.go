package main

import (
	"path/filepath"
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
