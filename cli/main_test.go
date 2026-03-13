package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "ha-nova")
	os.MkdirAll(configDir, 0o755)

	content := "RELAY_BASE_URL=http://192.168.1.5:8791\nHA_HOST=192.168.1.5\n"
	os.WriteFile(filepath.Join(configDir, "onboarding.env"), []byte(content), 0o644)

	t.Setenv("HOME", home)
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}
	if cfg.RelayBaseURL != "http://192.168.1.5:8791" {
		t.Errorf("RelayBaseURL = %q, want %q", cfg.RelayBaseURL, "http://192.168.1.5:8791")
	}
}

func TestLoadConfigQuotedValues(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "ha-nova")
	os.MkdirAll(configDir, 0o755)

	content := "RELAY_BASE_URL='http://10.0.0.1:8791'\n"
	os.WriteFile(filepath.Join(configDir, "onboarding.env"), []byte(content), 0o644)

	t.Setenv("HOME", home)
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}
	if cfg.RelayBaseURL != "http://10.0.0.1:8791" {
		t.Errorf("RelayBaseURL = %q, want %q", cfg.RelayBaseURL, "http://10.0.0.1:8791")
	}
}

func TestLoadConfigMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int // -1 = a<b, 0 = a==b, 1 = a>b
	}{
		{"0.1.0", "0.1.0", 0},
		{"0.1.0", "0.2.0", -1},
		{"0.2.0", "0.1.0", 1},
		{"1.0.0", "0.9.9", 1},
		{"0.1.0", "0.1.1", -1},
	}
	for _, tt := range tests {
		got := compareSemver(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("compareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestParseVersionJSON(t *testing.T) {
	dir := t.TempDir()
	content := `{"skill_version":"0.1.11","min_relay_version":"0.1.0"}`
	os.WriteFile(filepath.Join(dir, "version.json"), []byte(content), 0o644)

	minV := readMinRelayVersion(dir)
	if minV != "0.1.0" {
		t.Errorf("minRelayVersion = %q, want %q", minV, "0.1.0")
	}
}
