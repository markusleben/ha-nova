package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFullPurgeKeepsConfigTargetUntilServiceTokenIsRemoved(
	t *testing.T,
) {
	if runtime.GOOS == "windows" {
		t.Skip("service token files are not supported on native Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg := runtimeConfig{
		HAHost:         "192.0.2.5",
		HAURL:          "http://192.0.2.5:8123",
		RelayBaseURL:   "http://192.0.2.5:8791",
		RelayTokenFile: defaultRelayAuthTokenFile(paths),
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	if err := writeRelayAuthTokenFile(
		cfg.RelayTokenFile,
		"service-token",
	); err != nil {
		t.Fatal(err)
	}
	stop := errors.New("stop before config cleanup")
	err = finalizeLocalUninstallWithProgress(
		paths,
		installState{},
		&uninstallReport{},
		uninstallModePurge,
		func(step string) error {
			if step == "config_cleanup" {
				return stop
			}
			return nil
		},
		false,
	)
	if !errors.Is(err, stop) {
		t.Fatalf("full purge error = %v", err)
	}
	if _, statErr := os.Stat(paths.ConfigFile); statErr != nil {
		t.Fatalf("config cleanup target was lost: %v", statErr)
	}
	if _, statErr := os.Stat(cfg.RelayTokenFile); !isNotExist(statErr) {
		t.Fatalf("service token survived before config cleanup: %v", statErr)
	}
}

func TestFullPurgeRejectsServiceTokenPathOverlappingConfig(
	t *testing.T,
) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg := runtimeConfig{
		HAHost:         "192.0.2.5",
		HAURL:          "http://192.0.2.5:8123",
		RelayBaseURL:   "http://192.0.2.5:8791",
		RelayTokenFile: paths.ConfigFile,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}

	err = finalizeLocalUninstallWithProgress(
		paths,
		installState{},
		&uninstallReport{},
		uninstallModePurge,
		nil,
		false,
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"is not the managed service-token path",
		) {
		t.Fatalf("full purge error = %v", err)
	}
	if _, statErr := os.Stat(paths.ConfigFile); statErr != nil {
		t.Fatalf("overlapping config target was removed: %v", statErr)
	}
}

func TestFullPurgeRejectsServiceTokenPathThroughSymlinkAncestor(
	t *testing.T,
) {
	if runtime.GOOS == "windows" {
		t.Skip("service token files are not supported on native Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	externalDir := t.TempDir()
	externalToken := filepath.Join(externalDir, "service-token")
	if err := os.WriteFile(
		externalToken,
		[]byte("external-secret"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(paths.ConfigDir, "alias")
	if err := os.Symlink(externalDir, alias); err != nil {
		t.Fatal(err)
	}
	cfg := runtimeConfig{
		HAHost:         "192.0.2.5",
		HAURL:          "http://192.0.2.5:8123",
		RelayBaseURL:   "http://192.0.2.5:8791",
		RelayTokenFile: filepath.Join(alias, "service-token"),
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}

	err = finalizeLocalUninstallWithProgress(
		paths,
		installState{},
		&uninstallReport{},
		uninstallModePurge,
		nil,
		false,
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"is not the managed service-token path",
		) {
		t.Fatalf("full purge error = %v", err)
	}
	actual, readErr := os.ReadFile(externalToken)
	if readErr != nil || string(actual) != "external-secret" {
		t.Fatalf(
			"external token=%q error=%v",
			actual,
			readErr,
		)
	}
	if _, statErr := os.Stat(paths.ConfigFile); statErr != nil {
		t.Fatalf("config cleanup target was removed: %v", statErr)
	}
}
