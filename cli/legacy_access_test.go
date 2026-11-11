package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Each AI session's first HA task runs check-update --quiet: a computer on
// the old shared access hears, once, how to pair again. Only when its App can
// pair; never for a paired computer.
func TestCheckUpdateNamesTheSharedAccessSwitch(t *testing.T) {
	paths, cfg := doctorTestSetup(t)
	previous := probePairingV1ForNotice
	t.Cleanup(func() { probePairingV1ForNotice = previous })
	canPair, probed := true, false
	probePairingV1ForNotice = func(context.Context, string) bool { probed = true; return canPair }

	notice := legacyAccessNoticeFor(cfg)
	_, out := captureCommandOutput(t, func() int { return runCheckUpdate(paths, []string{"--quiet"}) })
	if !strings.Contains(out, "Tell the user once, after the result: "+notice) {
		t.Fatalf("quiet check-update must hand the notice to the assistant:\n%s", out)
	}
	_, out = captureCommandOutput(t, func() int { return runCheckUpdate(paths, nil) })
	if !strings.Contains(out, "WARNING: "+notice) || strings.Contains(out, "Tell the user") {
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
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".test-relay-auth-token"))
	resetServerProfileSelection(t)
	if err := writeRelayAuthToken("shared"); err != nil {
		t.Fatal(err)
	}
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
	if code != 0 || !strings.Contains(out, "WARNING: "+legacyAccessNoticeFor(runtimeConfig{})) {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

// A service install is told the file-backed pair.
func TestServiceInstallGetsTheFileBackedPair(t *testing.T) {
	if got := legacyAccessNoticeFor(runtimeConfig{RelayTokenFile: "/etc/ha-nova/token"}); !strings.Contains(got, "Run ha-nova pair --credential-store=file in a terminal") {
		t.Fatalf("service notice = %q", got)
	}
}

// An App that no longer takes the old shared access (401 with its mark) is
// answered with the way back for this computer, never a bare sign-in error.
func TestSharedAccessRefusalSaysHowToPairAgain(t *testing.T) {
	ended := http.Header{}
	ended.Set(relayPairingRequiredHeader, "pairing-required")
	for _, c := range []struct {
		name    string
		profile string
		cfg     runtimeConfig
		want    string
	}{
		{"desktop", defaultServerProfileName, runtimeConfig{RelayBaseURL: "http://ha.local:8791"}, "Run ha-nova setup in a terminal once"},
		{"service", defaultServerProfileName, runtimeConfig{RelayBaseURL: "http://ha.local:8791", RelayTokenFile: "/etc/ha-nova/token"}, "Run ha-nova pair --credential-store=file in a terminal once"},
		{"named server", "cabin", runtimeConfig{RelayBaseURL: "http://cabin.local:8791"}, `Run ha-nova pair --server cabin --relay-url "http://cabin.local:8791" in a terminal once`},
		{"Link saved", defaultServerProfileName, runtimeConfig{RelayBaseURL: "http://ha.local:8791", Cloud: &cloudLifecycleMetadata{}}, "Remove Home Assistant Link first (ha-nova cloud remove --server default), then run ha-nova setup"},
	} {
		got := localRelayAuthRepairMessage(http.StatusUnauthorized, ended, c.profile, c.cfg)
		if !strings.Contains(got, "old shared access, which the updated NOVA App no longer accepts") || !strings.Contains(got, c.want) {
			t.Fatalf("%s: %q", c.name, got)
		}
	}
	if got := localRelayAuthRepairMessage(http.StatusUnauthorized, nil, defaultServerProfileName, runtimeConfig{}); strings.Contains(got, "shared access") {
		t.Fatalf("an unmarked 401 is the usual repair: %q", got)
	}
}

// The health check tells the refusal apart by its mark, before any body.
func TestRelayHealthNamesTheEndedSharedAccessBeforeReadingBody(t *testing.T) {
	body := &diagnosticAuthBody{}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		header := http.Header{}
		header.Set(relayPairingRequiredHeader, "pairing-required")
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: body, Header: header}, nil
	})}
	if _, err := fetchRelayHealthWithContext(context.Background(), client, "http://relay.test", "shared"); !errors.Is(err, errSharedAccessEnded) || body.read {
		t.Fatalf("err = %v, body read = %v", err, body.read)
	}
	if !relayHealthIssueLooksLikeRelayAuth(errSharedAccessEnded) {
		t.Fatal("setup must still read it as a sign-in problem")
	}
}

// Doctor against an App that refuses the old shared access: one line that
// says how to pair again, a failed check, and no second warning.
func TestDoctorNamesTheWayBackWhenTheAppRefusesTheSharedToken(t *testing.T) {
	paths, cfg := doctorTestSetup(t)
	previousHealth, previousProbe := fetchRelayHealthForReadiness, probePairingV1ForDoctor
	t.Cleanup(func() { fetchRelayHealthForReadiness, probePairingV1ForDoctor = previousHealth, previousProbe })
	fetchRelayHealthForReadiness = func(string, string) ([]byte, error) { return nil, errSharedAccessEnded }
	probePairingV1ForDoctor = func(context.Context, string) bool { return true }

	code, out := captureCommandOutput(t, func() int { return runDoctor(paths, nil) })
	notice := legacyAccessNoticeFor(cfg)
	if code != 1 || strings.Count(out, notice) != 1 || strings.Contains(out, "WARNING: "+notice) || strings.Contains(out, "couldn't check the NOVA App") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

// The notice needs the shared token: without one, doctor names that problem
// instead (Codex review).
func TestNoSharedAccessNoticeWithoutTheToken(t *testing.T) {
	paths, _ := doctorTestSetup(t)
	previous, previousRead := probePairingV1ForNotice, readRelayAuthTokenForDoctor
	t.Cleanup(func() { probePairingV1ForNotice, readRelayAuthTokenForDoctor = previous, previousRead })
	probePairingV1ForNotice = func(context.Context, string) bool { return true }
	readRelayAuthTokenForDoctor = func() (string, error) { return "", nil }
	if notice := legacyAccessNotice(context.Background(), paths); notice != "" {
		t.Fatalf("notice without a token = %q", notice)
	}
}
