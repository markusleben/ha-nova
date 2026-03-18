package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRuntimeConfigPrefersJSONConfig(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "ha-nova")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	configJSON := `{
  "schema_version": 1,
  "ha_host": "192.168.1.20",
  "ha_url": "http://192.168.1.20:8123",
  "relay_base_url": "http://192.168.1.20:8791"
}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(configJSON), 0o600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	t.Setenv("HOME", home)
	cfg, err := loadRuntimeConfig()
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error: %v", err)
	}

	if cfg.RelayBaseURL != "http://192.168.1.20:8791" {
		t.Fatalf("RelayBaseURL = %q", cfg.RelayBaseURL)
	}
	if cfg.HAURL != "http://192.168.1.20:8123" {
		t.Fatalf("HAURL = %q", cfg.HAURL)
	}
}

func TestLoadRuntimeConfigRejectsLegacyOnboardingEnv(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "ha-nova")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	legacy := "HA_HOST='192.168.1.5'\nHA_URL='http://192.168.1.5:8123'\nRELAY_BASE_URL='http://192.168.1.5:8791'\n"
	if err := os.WriteFile(filepath.Join(configDir, "onboarding.env"), []byte(legacy), 0o600); err != nil {
		t.Fatalf("write onboarding.env: %v", err)
	}

	t.Setenv("HOME", home)
	if _, err := loadRuntimeConfig(); err == nil {
		t.Fatal("expected legacy onboarding.env-only config to fail")
	}
}

func TestLoadBundleMetadataFromInstalledRoot(t *testing.T) {
	home := t.TempDir()
	installDir := filepath.Join(home, ".local", "share", "ha-nova")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("mkdir install dir: %v", err)
	}

	bundleJSON := `{
  "bundle_format_version": 1,
  "version": "0.2.0",
  "os": "macos",
  "arch": "arm64",
  "binary_name": "ha-nova"
}`
	if err := os.WriteFile(filepath.Join(installDir, "bundle.json"), []byte(bundleJSON), 0o600); err != nil {
		t.Fatalf("write bundle.json: %v", err)
	}

	t.Setenv("HOME", home)
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	meta, err := loadBundleMetadata(paths)
	if err != nil {
		t.Fatalf("loadBundleMetadata() error: %v", err)
	}

	if meta.Version != "0.2.0" {
		t.Fatalf("Version = %q", meta.Version)
	}
	if meta.OS != "macos" || meta.Arch != "arm64" {
		t.Fatalf("unexpected bundle metadata: %+v", meta)
	}
}

func TestResolveCommandSupportsOnlyTopLevelCLI(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		argv0   string
		wantCmd string
		wantArg string
	}{
		{
			name:    "ha-nova relay health",
			argv0:   "ha-nova",
			args:    []string{"relay", "health"},
			wantCmd: "relay",
			wantArg: "health",
		},
		{
			name:    "top-level doctor",
			argv0:   "ha-nova",
			args:    []string{"doctor"},
			wantCmd: "doctor",
			wantArg: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, args := resolveCommand(tt.argv0, tt.args)
			if cmd != tt.wantCmd {
				t.Fatalf("command = %q, want %q", cmd, tt.wantCmd)
			}
			gotArg := ""
			if len(args) > 0 {
				gotArg = args[0]
			}
			if gotArg != tt.wantArg {
				t.Fatalf("first arg = %q, want %q", gotArg, tt.wantArg)
			}
		})
	}
}

func TestExtractRelayPayloadSupportsNewFlags(t *testing.T) {
	dir := t.TempDir()
	payloadFile := filepath.Join(dir, "payload.json")
	if err := os.WriteFile(payloadFile, []byte(`{"type":"get_states"}`), 0o600); err != nil {
		t.Fatalf("write payload file: %v", err)
	}

	payload, err := extractRelayPayload([]string{"--data-file", payloadFile})
	if err != nil {
		t.Fatalf("extractRelayPayload() error: %v", err)
	}
	if payload != `{"type":"get_states"}` {
		t.Fatalf("payload = %q", payload)
	}

	inline, err := extractRelayPayload([]string{"-d", `{"type":"ping"}`})
	if err != nil {
		t.Fatalf("extractRelayPayload inline error: %v", err)
	}
	if inline != `{"type":"ping"}` {
		t.Fatalf("inline payload = %q", inline)
	}
}
