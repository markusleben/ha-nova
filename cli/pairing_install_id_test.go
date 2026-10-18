package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Regression: the Relay drops every other active device with the same
// client_install_id on activation, so a second profile paired against the same
// Relay silently disconnected the first one.
func TestPairingClientInstallIDIsPerProfile(t *testing.T) {
	const install = "inst-0123456789abcdef0123456789abcdef"
	plain, err := pairingClientInstallID(install, defaultServerProfileName)
	if err != nil || plain != install {
		t.Fatalf("default = %q, %v; want the plain install id", plain, err)
	}
	cabin, err := pairingClientInstallID(install, "cabin")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := pairingClientInstallID(install, "cabin")
	smoke, _ := pairingClientInstallID(install, "smoke-1")
	if cabin == plain || cabin == smoke || cabin != again {
		t.Fatalf("ids must be distinct per profile and stable: default=%q cabin=%q again=%q smoke=%q",
			plain, cabin, again, smoke)
	}
	longest, err := pairingClientInstallID(install, strings.Repeat("a", 32))
	if err != nil || !validIdentifier(longest, 128) {
		t.Fatalf("longest profile name: %q, %v", longest, err)
	}
	if _, err := pairingClientInstallID(strings.Repeat("i", 128), "cabin"); err == nil || errors.Is(err, errInvalidClientInstallID) {
		t.Fatalf("over-long derived id must fail loud, got %v", err)
	}
	if _, err := pairingClientInstallID(install, "Bad Name"); err == nil {
		t.Fatal("invalid profile name must be rejected")
	}
}

func TestLocalPairingSendsProfileScopedInstallID(t *testing.T) {
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	resetServerProfileSelection(t)

	var sent []string
	origPair, origActivate := pairDeviceV1ForPairing, activateDeviceV1ForPairing
	pairDeviceV1ForPairing = func(_ *http.Client, _, _ string, metadata deviceMetadata) (*provisionedCredential, error) {
		sent = append(sent, metadata.ClientInstallID)
		return nil, errors.New("stop after metadata")
	}
	t.Cleanup(func() {
		pairDeviceV1ForPairing = origPair
		activateDeviceV1ForPairing = origActivate
	})

	for _, profile := range []string{defaultServerProfileName, "cabin"} {
		setActiveServerProfile(profile)
		cfg := runtimeConfig{ClientInstallID: "inst-abc"}
		_, _ = runSecurePairing("http://relay:8791", "123456", &cfg,
			func(*runtimeConfig) error { return nil }, defaultPairingClientInfo())
	}
	if len(sent) != 2 || sent[0] != "inst-abc" || sent[1] != "inst-abc-cabin" {
		t.Fatalf("sent install ids = %q", sent)
	}
}

func TestLinkOwnerPairingSendsProfileScopedInstallID(t *testing.T) {
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	resetServerProfileSelection(t)
	setActiveServerProfile("cabin")

	sent := ""
	previous := pairDeviceV2ForCloudSetup
	pairDeviceV2ForCloudSetup = func(_ context.Context, _ *CloudIngressClient, _ string,
		metadata deviceMetadata, _ string) (*cloudProvisionedCredential, error) {
		sent = metadata.ClientInstallID
		return nil, errors.New("stop after metadata")
	}
	t.Cleanup(func() { pairDeviceV2ForCloudSetup = previous })

	request := cloudRemoteSetupRequest{
		PairingCode: func(cloudRemotePairingPrompt) (string, error) { return "123456", nil },
	}
	request.Config.ClientInstallID = "inst-abc"
	_, _ = provisionRemoteCloudDeviceWithOwnerCode(context.Background(),
		cloudVerifiedSession{}, request, "relay-1", "")
	if sent != "inst-abc-cabin" {
		t.Fatalf("Link owner pairing sent install id %q, want inst-abc-cabin", sent)
	}
}

// Real Relay registry: a profile paired by an older binary (plain install id)
// re-pairs without leaving its old device active, and pairing `default`
// afterwards keeps the second profile connected. Runs in the pairing-e2e
// workflow.
func TestSecondProfilePairingKeepsFirstDeviceEndToEnd(t *testing.T) {
	harness := os.Getenv("HA_NOVA_PAIR_E2E_HARNESS")
	if harness == "" {
		t.Skip("set HA_NOVA_PAIR_E2E_HARNESS to run the pairing e2e")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	resetServerProfileSelection(t)

	cmd := exec.Command("node", harness)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	lines := bufio.NewReader(stdout)
	readInfo := func(into any) {
		t.Helper()
		line, err := lines.ReadString('\n')
		if err != nil || json.Unmarshal([]byte(strings.TrimSpace(line)), into) != nil {
			t.Fatalf("bad harness line %q: %v", line, err)
		}
	}
	var info struct {
		BootstrapPort int    `json:"bootstrapPort"`
		Code          string `json:"code"`
	}
	readInfo(&info)
	bootstrapURL := fmt.Sprintf("http://127.0.0.1:%d", info.BootstrapPort)
	nextCode := func() string {
		t.Helper()
		if _, err := io.WriteString(stdin, "next\n"); err != nil {
			t.Fatal(err)
		}
		var next struct {
			Code string `json:"code"`
		}
		readInfo(&next)
		return next.Code
	}
	const install = "inst-e2e"
	functional := func(cfg runtimeConfig, credential string) int {
		t.Helper()
		return securedFunctional(t, cfg.RelaySecureBaseURL, cfg.RelaySpkiPin, credential)
	}

	// An older binary paired `cabin` under the plain install id.
	setActiveServerProfile("cabin")
	legacy, err := pairDeviceV1(nil, bootstrapURL, info.Code, deviceMetadata{
		Name: "legacy", Platform: "darwin", Client: "cli", ClientInstallID: install,
	})
	if err != nil {
		t.Fatal(err)
	}
	legacyCfg := runtimeConfig{
		ClientInstallID:    install,
		RelaySecureBaseURL: fmt.Sprintf("https://127.0.0.1:%d", legacy.SecurePort),
		RelaySpkiPin:       legacy.SpkiPin,
	}
	if err := activateDeviceV1(legacyCfg.RelaySecureBaseURL, legacy.SpkiPin, legacy.Credential); err != nil {
		t.Fatal(err)
	}
	if err := writeDeviceCredential(legacy.Credential); err != nil {
		t.Fatal(err)
	}

	pair := func(profile string, cfg runtimeConfig) (runtimeConfig, string) {
		t.Helper()
		setActiveServerProfile(profile)
		if _, err := runSecurePairing(bootstrapURL, nextCode(), &cfg,
			func(*runtimeConfig) error { return nil }, defaultPairingClientInfo()); err != nil {
			t.Fatalf("pair %s: %v", profile, err)
		}
		credential, ok, err := readDeviceCredential()
		if err != nil || !ok {
			t.Fatalf("%s credential not stored: %v", profile, err)
		}
		return cfg, credential
	}
	cabinCfg, cabinCredential := pair("cabin", legacyCfg)
	if code := functional(legacyCfg, legacy.Credential); code != http.StatusUnauthorized {
		t.Fatalf("legacy cabin device still active after re-pair: %d", code)
	}
	defaultCfg, defaultCredential := pair(defaultServerProfileName, runtimeConfig{ClientInstallID: install})

	if code := functional(cabinCfg, cabinCredential); code != http.StatusOK {
		t.Fatalf("cabin lost its device when default paired: %d", code)
	}
	if code := functional(defaultCfg, defaultCredential); code != http.StatusOK {
		t.Fatalf("default functional: %d", code)
	}
}

func TestRepairRetiresPreviousDeviceOnlyOnSameRelay(t *testing.T) {
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	resetServerProfileSelection(t)
	old, replacement := validCredential(21), validCredential(22)
	newID := parseDeviceCredential(replacement).deviceID

	var revoked []string
	previousRevoke := revokeSelfDeviceV1ForPairing
	revokeSelfDeviceV1ForPairing = func(base, pin, credential string) error {
		revoked = append(revoked, base+"|"+pin+"|"+credential)
		return nil
	}
	t.Cleanup(func() { revokeSelfDeviceV1ForPairing = previousRevoke })

	cfg := runtimeConfig{RelaySecureBaseURL: "https://ha:8792", RelaySpkiPin: "PIN"}
	for _, tc := range []struct {
		name, profile, newPin, newID string
		want                         int
	}{
		{"named profile, same Relay", "cabin", "PIN", newID, 1},
		{"other Relay", "cabin", "OTHER", newID, 0},
		{"same device", "cabin", "PIN", parseDeviceCredential(old).deviceID, 0},
		{"default relies on Relay replacement", defaultServerProfileName, "PIN", newID, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
			revoked = nil
			setActiveServerProfile(tc.profile)
			if err := writeDeviceCredential(old); err != nil {
				t.Fatal(err)
			}
			readPreviousLocalDevice(cfg, SecretStoreForbidUI).retireOnSameRelay("https://ha-new:8792", tc.newID, tc.newPin)
			if len(revoked) != tc.want ||
				(tc.want == 1 && revoked[0] != "https://ha-new:8792|PIN|"+old) {
				t.Fatalf("revoked = %q, want %d call(s)", revoked, tc.want)
			}
		})
	}
}
