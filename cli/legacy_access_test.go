package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Each AI session's first HA task runs check-update --quiet: a computer on
// the old shared access hears, once, that the next release won't accept it.
// Only when its App can pair; never for a paired computer.
func TestCheckUpdateNamesTheSharedAccessSwitch(t *testing.T) {
	paths, cfg := doctorTestSetup(t)
	previous := probePairingV1ForNotice
	t.Cleanup(func() { probePairingV1ForNotice = previous })
	canPair, probed := true, false
	probePairingV1ForNotice = func(context.Context, string) bool { probed = true; return canPair }

	_, out := captureCommandOutput(t, func() int { return runCheckUpdate(paths, []string{"--quiet"}) })
	if !strings.Contains(out, "Tell the user once, after the result: "+legacyAccessNoticeText) {
		t.Fatalf("quiet check-update must hand the notice to the assistant:\n%s", out)
	}
	_, out = captureCommandOutput(t, func() int { return runCheckUpdate(paths, nil) })
	if !strings.Contains(out, "WARNING: "+legacyAccessNoticeText) || strings.Contains(out, "Tell the user") {
		t.Fatalf("check-update must say it plainly to a person:\n%s", out)
	}

	canPair = false // a standalone container
	_, out = captureCommandOutput(t, func() int { return runCheckUpdate(paths, []string{"--quiet"}) })
	if strings.Contains(out, "shared access") {
		t.Fatalf("a relay without pairing must not get the notice:\n%s", out)
	}

	canPair, probed = true, false
	cfg.RelaySecureBaseURL, cfg.RelaySpkiPin = "https://relay.test:8792", "pin"
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	_, out = captureCommandOutput(t, func() int { return runCheckUpdate(paths, []string{"--quiet"}) })
	if strings.Contains(out, "shared access") || probed {
		t.Fatalf("a paired computer must not be probed or told (probed=%v):\n%s", probed, out)
	}
}

// `ha-nova update` names it too, also when already current (Codex review).
func TestUpdateAlreadyCurrentNamesTheSharedAccessSwitch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	resetServerProfileSelection(t)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.2.2","min_relay_version":"0.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(paths, runtimeConfig{HAHost: "ha", HAURL: "http://ha:8123", RelayBaseURL: "http://ha:8791"}); err != nil {
		t.Fatal(err)
	}
	previous := probePairingV1ForNotice
	t.Cleanup(func() { probePairingV1ForNotice = previous })
	probePairingV1ForNotice = func(context.Context, string) bool { return true }
	code, out := captureCommandOutput(t, func() int { return runUpdate(paths, []string{"--version", "0.2.2"}) })
	if code != 0 || !strings.Contains(out, "WARNING: "+legacyAccessNoticeText) {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

// A service install is told the file-backed pair.
func TestServiceInstallGetsTheFileBackedPair(t *testing.T) {
	if got := legacyAccessNoticeFor(runtimeConfig{RelayTokenFile: "/etc/ha-nova/token"}); got != legacyAccessServiceNoticeText {
		t.Fatalf("service notice = %q", got)
	}
}
