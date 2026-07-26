package main

import (
	"path/filepath"
	"testing"
)

func TestDetectPathsConfigDirOverrideKeepsLoginHome(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "cloud-e2e-config")
	t.Setenv("HOME", home)
	t.Setenv(configDirEnv, configDir)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if paths.Home != home {
		t.Fatalf("Home = %q, want %q", paths.Home, home)
	}
	if paths.ConfigDir != configDir {
		t.Fatalf("ConfigDir = %q, want %q", paths.ConfigDir, configDir)
	}
	for name, got := range map[string]string{
		"ConfigFile": paths.ConfigFile,
		"StateFile":  paths.StateFile,
		"CensusFile": paths.CensusFile,
	} {
		if filepath.Dir(got) != configDir {
			t.Errorf("%s = %q, want parent %q", name, got, configDir)
		}
	}
}

func TestDetectPathsRejectsUnsafeConfigDirOverride(t *testing.T) {
	for _, value := range []string{"relative/path", string(filepath.Separator)} {
		t.Run(value, func(t *testing.T) {
			t.Setenv(configDirEnv, value)
			if _, err := detectPaths(); err == nil {
				t.Fatalf("detectPaths() accepted %q", value)
			}
		})
	}
}
