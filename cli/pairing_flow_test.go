package main

import (
	"errors"
	"net/http"
	"os"
	"testing"
)

// Regression: a re-pair whose activation fails must NOT overwrite the working
// install's live endpoint; the new endpoint is only recorded as pending (so a
// crash between activation and promotion can still resume).
func TestRunSecurePairingFailedRePairPreservesLiveEndpoint(t *testing.T) {
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())

	origPair, origActivate := pairDeviceV1ForPairing, activateDeviceV1ForPairing
	const validCred = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	pairDeviceV1ForPairing = func(_ *http.Client, _, _ string, _ deviceMetadata) (*provisionedCredential, error) {
		return &provisionedCredential{DeviceID: "dev-new", Credential: validCred, SpkiPin: "NEW_PIN", SecurePort: 8792}, nil
	}
	activateDeviceV1ForPairing = func(_, _, _ string) error {
		return errors.New("secure port unreachable")
	}
	t.Cleanup(func() {
		pairDeviceV1ForPairing = origPair
		activateDeviceV1ForPairing = origActivate
	})

	cfg := runtimeConfig{ClientInstallID: "inst-existing", RelaySecureBaseURL: "https://old:8792", RelaySpkiPin: "OLD_PIN"}
	if _, err := runSecurePairing("http://relay:8791", "123456", &cfg, func(*runtimeConfig) error { return nil }, defaultPairingClientInfo()); err == nil {
		t.Fatal("expected a pairing error when activation fails")
	}
	if cfg.RelaySecureBaseURL != "https://old:8792" || cfg.RelaySpkiPin != "OLD_PIN" {
		t.Fatalf("failed re-pair overwrote the live endpoint: %q / %q", cfg.RelaySecureBaseURL, cfg.RelaySpkiPin)
	}
	if cfg.PendingSecureBaseURL == "" || cfg.PendingSpkiPin != "NEW_PIN" {
		t.Fatalf("pending endpoint not recorded: %q / %q", cfg.PendingSecureBaseURL, cfg.PendingSpkiPin)
	}
}

// Regression: if the live-endpoint save fails after activation, the credential
// must NOT have been promoted — the pending slot has to survive so resume can
// finish, instead of a lost pending credential and an un-updated config.
func TestRunSecurePairingKeepsPendingCredentialWhenEndpointSaveFails(t *testing.T) {
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())

	const validCred = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	origPair, origActivate := pairDeviceV1ForPairing, activateDeviceV1ForPairing
	pairDeviceV1ForPairing = func(_ *http.Client, _, _ string, _ deviceMetadata) (*provisionedCredential, error) {
		return &provisionedCredential{DeviceID: "dev-new", Credential: validCred, SpkiPin: "PIN", SecurePort: 8792}, nil
	}
	activateDeviceV1ForPairing = func(_, _, _ string) error { return nil } // activation succeeds
	t.Cleanup(func() {
		pairDeviceV1ForPairing = origPair
		activateDeviceV1ForPairing = origActivate
	})

	// Fail the live-endpoint save (the 2nd saveCfg, after the pre-activation one).
	saves := 0
	saveCfg := func(*runtimeConfig) error {
		saves++
		if saves == 2 {
			return errors.New("disk full")
		}
		return nil
	}
	cfg := runtimeConfig{ClientInstallID: "inst-x"}
	if _, err := runSecurePairing("http://relay:8791", "123456", &cfg, saveCfg, defaultPairingClientInfo()); err == nil {
		t.Fatal("expected an error when the endpoint save fails")
	}
	if _, ok, err := readPendingDeviceCredential(); err != nil || !ok {
		t.Fatalf("pending credential was lost before the endpoint save succeeded (ok=%v err=%v)", ok, err)
	}
}

// Regression: retiring the device credential (user switched to the manual/legacy
// path) must clear the PENDING slot too, even when the live endpoint was never
// set — otherwise a half-finished pairing lingers and resume could complete it.
func TestRetireDeviceCredentialClearsPendingWhenLiveEmpty(t *testing.T) {
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())

	const validCred = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	if err := writePendingDeviceCredential(validCred); err != nil {
		t.Fatalf("writePendingDeviceCredential: %v", err)
	}
	cfg := runtimeConfig{PendingSecureBaseURL: "https://relay:8792", PendingSpkiPin: "PIN"} // live empty

	retireDeviceCredential(&cfg)

	if _, ok, _ := readPendingDeviceCredential(); ok {
		t.Fatal("pending credential was not cleared; resume could pick up a stale pairing")
	}
	if cfg.PendingSecureBaseURL != "" || cfg.PendingSpkiPin != "" {
		t.Fatalf("pending endpoint not cleared: %q / %q", cfg.PendingSecureBaseURL, cfg.PendingSpkiPin)
	}
}

// Regression: retiring the device credential on a file-backed install (switch
// back to the legacy token path) must also drop the .file-backend marker, or the
// credential-less install stays classified as file-backed and a later desktop
// re-pair with a healthy keyring would keep storing credentials in files.
func TestRetireDeviceCredentialClearsFileBackendMarker(t *testing.T) {
	withDeviceStorageTestHome(t)
	deviceCredentialFileModeForced = true // headless: writes go to the file backend
	cred := generateTestDeviceCredential(t)
	if err := writeDeviceCredential(cred); err != nil {
		t.Fatalf("seed file-backed install: %v", err)
	}
	if !deviceFileBackendMarkerExists() {
		t.Fatal("setup: expected a file-backend marker after storing a credential")
	}

	cfg := runtimeConfig{} // no live endpoint → no revoke attempt
	retireDeviceCredential(&cfg)

	if deviceFileBackendMarkerExists() {
		t.Fatal("retire left the file-backend marker behind")
	}
	if deviceSecretFileExists(deviceCredentialService) {
		t.Fatal("retire left the current credential file behind")
	}
	if deviceSecretFileBacked() {
		t.Fatal("install still classified as file-backed after retiring the credential")
	}
}

// Regression: a headless pairing interrupted before promotion leaves a pending
// FILE with no marker. Retire/purge must remove that orphan file directly (its
// routed delete would go to the keyring), not leave it — and the empty secrets
// dir must go too.
func TestRetireRemovesOrphanPendingFileWithoutMarker(t *testing.T) {
	withDeviceStorageTestHome(t)
	if err := deviceSecretFileSet(deviceCredentialPendingService, generateTestDeviceCredential(t)); err != nil {
		t.Fatalf("seed orphan pending file: %v", err)
	}
	if deviceFileBackendMarkerExists() {
		t.Fatal("a pending write must not create a marker (test premise)")
	}

	cfg := runtimeConfig{} // no live endpoint → no revoke attempt
	retireDeviceCredential(&cfg)

	if deviceSecretFileExists(deviceCredentialPendingService) {
		t.Fatal("orphan pending credential file left on disk after retire")
	}
	dir, _ := deviceSecretFileDir()
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("secrets dir left behind after retire (orphan residue not fully cleared)")
	}
}

// Regression: an IPv6 relay host must stay bracketed when building the secure
// endpoint URL, or activation and later functional calls get an invalid host.
func TestSecureBaseFromBootstrapBracketsIPv6(t *testing.T) {
	got, err := secureBaseFromBootstrap("http://[2001:db8::1]:8791", 8792)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://[2001:db8::1]:8792" {
		t.Fatalf("IPv6 host not bracketed: got %q", got)
	}
}

func TestSecureBaseFromBootstrapIPv4(t *testing.T) {
	got, err := secureBaseFromBootstrap("http://192.168.1.5:8791", 8792)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://192.168.1.5:8792" {
		t.Fatalf("got %q, want https://192.168.1.5:8792", got)
	}
}

func TestSecureBaseFromBootstrapRejectsBadPort(t *testing.T) {
	if _, err := secureBaseFromBootstrap("http://192.168.1.5:8791", 0); err == nil {
		t.Fatal("expected an error for an invalid secure port")
	}
}
