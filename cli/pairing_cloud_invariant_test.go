package main

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestLocalPairingDoesNotReplaceConfiguredCloudDevice(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	pairCalls := 0
	previousPair := pairDeviceV1ForPairing
	pairDeviceV1ForPairing = func(
		*http.Client,
		string,
		string,
		deviceMetadata,
	) (*provisionedCredential, error) {
		pairCalls++
		return nil, errors.New("must not pair")
	}
	t.Cleanup(func() {
		pairDeviceV1ForPairing = previousPair
	})
	cfg := runtimeConfig{
		RelayInstanceID: "relay-cloud",
		Cloud: &cloudLifecycleMetadata{
			State: cloudStateAuthorizing,
		},
	}

	_, err := runSecurePairing(
		"http://relay:8791",
		"123456",
		&cfg,
		func(*runtimeConfig) error {
			t.Fatal("blocked local pairing mutated config")
			return nil
		},
		defaultPairingClientInfo(),
	)
	if err == nil || !strings.Contains(err.Error(), "Cloud access is configured") {
		t.Fatalf("configured Cloud local pairing error = %v", err)
	}
	if pairCalls != 0 || cfg.RelayInstanceID != "relay-cloud" {
		t.Fatalf(
			"blocked local pairing calls=%d config=%+v",
			pairCalls,
			cfg,
		)
	}
}

func TestLocalPairingDoesNotOverwriteOrphanedCloudPending(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	credential := validCredential(25)
	if err := writePendingCloudDeviceCredential(
		credential,
		"relay-cloud",
	); err != nil {
		t.Fatal(err)
	}
	pairCalls := 0
	previousPair := pairDeviceV1ForPairing
	pairDeviceV1ForPairing = func(
		*http.Client,
		string,
		string,
		deviceMetadata,
	) (*provisionedCredential, error) {
		pairCalls++
		return nil, errors.New("must not pair")
	}
	t.Cleanup(func() {
		pairDeviceV1ForPairing = previousPair
	})

	cfg := runtimeConfig{}
	_, err := runSecurePairing(
		"http://relay:8791",
		"123456",
		&cfg,
		func(*runtimeConfig) error { return nil },
		defaultPairingClientInfo(),
	)
	if err == nil || !strings.Contains(err.Error(), "Cloud device pairing is still pending") {
		t.Fatalf("orphaned Cloud pending local pairing error = %v", err)
	}
	record, exists, readErr := readPendingDeviceCredentialRecord()
	if pairCalls != 0 ||
		readErr != nil ||
		!exists ||
		record.Credential != credential ||
		record.RelayInstanceID != "relay-cloud" {
		t.Fatalf(
			"orphaned Cloud pending calls=%d record=%+v exists=%v err=%v",
			pairCalls,
			record,
			exists,
			readErr,
		)
	}
}

func TestSuccessfulLocalPairingClearsUnprovedRelayIdentity(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	credential := validCredential(0)
	previousPair := pairDeviceV1ForPairing
	previousActivate := activateDeviceV1ForPairing
	pairDeviceV1ForPairing = func(
		*http.Client,
		string,
		string,
		deviceMetadata,
	) (*provisionedCredential, error) {
		return &provisionedCredential{
			DeviceID:   parseDeviceCredential(credential).deviceID,
			Credential: credential,
			SpkiPin:    "pin",
			SecurePort: 8792,
		}, nil
	}
	activateDeviceV1ForPairing = func(_, _, _ string) error { return nil }
	t.Cleanup(func() {
		pairDeviceV1ForPairing = previousPair
		activateDeviceV1ForPairing = previousActivate
	})
	cfg := runtimeConfig{RelayInstanceID: "relay-old"}

	if _, err := runSecurePairing(
		"http://relay:8791",
		"123456",
		&cfg,
		func(*runtimeConfig) error { return nil },
		defaultPairingClientInfo(),
	); err != nil {
		t.Fatal(err)
	}
	if cfg.RelayInstanceID != "" {
		t.Fatalf("local pairing retained stale Relay identity %q", cfg.RelayInstanceID)
	}
}

func TestLocalPendingResumeDoesNotMutateConfiguredCloudDevice(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	if err := writePendingDeviceCredential(validCredential(1)); err != nil {
		t.Fatal(err)
	}
	previousActivate := activateDeviceV1ForPairing
	activateCalls := 0
	activateDeviceV1ForPairing = func(_, _, _ string) error {
		activateCalls++
		return nil
	}
	t.Cleanup(func() {
		activateDeviceV1ForPairing = previousActivate
	})
	cfg := runtimeConfig{
		PendingSecureBaseURL: "https://relay:8792",
		PendingSpkiPin:       "pin",
		Cloud: &cloudLifecycleMetadata{
			State: cloudStateAuthorizing,
		},
	}

	resumed, err := resumePendingActivation(
		&cfg,
		func(*runtimeConfig) error { return nil },
	)
	if err == nil || resumed || activateCalls != 0 {
		t.Fatalf(
			"configured Cloud local resume=%v calls=%d err=%v",
			resumed,
			activateCalls,
			err,
		)
	}
}

func TestLocalPendingResumeClearsUnprovedRelayIdentity(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	pending := validCredential(2)
	if err := writePendingDeviceCredential(pending); err != nil {
		t.Fatal(err)
	}
	previousActivate := activateDeviceV1ForPairing
	activateDeviceV1ForPairing = func(_, _, credential string) error {
		if credential != pending {
			t.Fatalf("resumed credential = %q", credential)
		}
		return nil
	}
	t.Cleanup(func() {
		activateDeviceV1ForPairing = previousActivate
	})
	cfg := runtimeConfig{
		RelayInstanceID:      "relay-before-reinstall",
		PendingSecureBaseURL: "https://relay-new:8792",
		PendingSpkiPin:       "pin-new",
	}
	var saved []runtimeConfig

	resumed, err := resumePendingActivation(
		&cfg,
		func(value *runtimeConfig) error {
			saved = append(saved, *value)
			return nil
		},
	)
	if err != nil || !resumed {
		t.Fatalf("local pending resume=%v err=%v", resumed, err)
	}
	if cfg.RelayInstanceID != "" || len(saved) == 0 ||
		saved[0].RelayInstanceID != "" {
		t.Fatalf(
			"resumed local pairing retained Relay identity: cfg=%q saves=%+v",
			cfg.RelayInstanceID,
			saved,
		)
	}
}
