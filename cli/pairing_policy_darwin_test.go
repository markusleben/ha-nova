//go:build darwin

package main

import (
	"context"
	"net/http"
	"testing"

	"github.com/zalando/go-keyring"
)

type darwinDevicePolicyEvent struct {
	operation string
	service   string
	ui        SecretStoreUIPolicy
}

func installDarwinDevicePolicyRecorder(
	t *testing.T,
) (map[string]string, *[]darwinDevicePolicyEvent) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", "")
	previousForced := deviceCredentialFileModeForced
	previousExplicit := deviceCredentialFileModeExplicit
	deviceCredentialFileModeForced = false
	deviceCredentialFileModeExplicit = false

	slots := map[string]string{}
	events := []darwinDevicePolicyEvent{}
	previousPreflight := deviceCredentialPreflightWithContext
	previousGet := secretKeyringGetWithPolicy
	previousSet := secretKeyringSetWithPolicy
	previousDelete := secretKeyringDeleteWithPolicy
	deviceCredentialPreflightWithContext = func(
		ctx context.Context,
		ui SecretStoreUIPolicy,
	) error {
		events = append(events, darwinDevicePolicyEvent{
			operation: "preflight",
			ui:        ui,
		})
		return validateDeviceCredentialPreflightRequest(ctx, ui)
	}
	secretKeyringGetWithPolicy = func(
		_ context.Context,
		service, _ string,
		ui SecretStoreUIPolicy,
	) (string, error) {
		events = append(events, darwinDevicePolicyEvent{
			operation: "get",
			service:   service,
			ui:        ui,
		})
		value, exists := slots[service]
		if !exists {
			return "", keyring.ErrNotFound
		}
		return value, nil
	}
	secretKeyringSetWithPolicy = func(
		_ context.Context,
		service, _, value string,
		ui SecretStoreUIPolicy,
	) error {
		events = append(events, darwinDevicePolicyEvent{
			operation: "set",
			service:   service,
			ui:        ui,
		})
		slots[service] = value
		return nil
	}
	secretKeyringDeleteWithPolicy = func(
		_ context.Context,
		service, _ string,
		ui SecretStoreUIPolicy,
	) error {
		events = append(events, darwinDevicePolicyEvent{
			operation: "delete",
			service:   service,
			ui:        ui,
		})
		if _, exists := slots[service]; !exists {
			return keyring.ErrNotFound
		}
		delete(slots, service)
		return nil
	}
	t.Cleanup(func() {
		deviceCredentialFileModeForced = previousForced
		deviceCredentialFileModeExplicit = previousExplicit
		deviceCredentialPreflightWithContext = previousPreflight
		secretKeyringGetWithPolicy = previousGet
		secretKeyringSetWithPolicy = previousSet
		secretKeyringDeleteWithPolicy = previousDelete
	})
	return slots, &events
}

func TestDarwinExplicitPairingUsesInteractiveDevicePolicy(t *testing.T) {
	slots, events := installDarwinDevicePolicyRecorder(t)
	credential := validCredential(210)
	previousPair := pairDeviceV1ForPairing
	previousActivate := activateDeviceV1ForPairing
	pairDeviceV1ForPairing = func(
		*http.Client,
		string,
		string,
		deviceMetadata,
	) (*provisionedCredential, error) {
		return &provisionedCredential{
			DeviceID:   deviceIDOf(credential),
			Credential: credential,
			SpkiPin:    "pin",
			SecurePort: 8792,
		}, nil
	}
	activateDeviceV1ForPairing = func(string, string, string) error {
		return nil
	}
	t.Cleanup(func() {
		pairDeviceV1ForPairing = previousPair
		activateDeviceV1ForPairing = previousActivate
	})

	cfg := runtimeConfig{ClientInstallID: "inst-policy-test"}
	if _, err := runSecurePairing(
		"http://relay:8791",
		"123456",
		&cfg,
		func(*runtimeConfig) error { return nil },
		defaultPairingClientInfo(),
	); err != nil {
		t.Fatal(err)
	}
	if slots[activeDeviceCredentialService()] != credential {
		t.Fatal("explicit pairing did not promote the credential")
	}
	assertDarwinCredentialSlotPolicy(
		t,
		*events,
		SecretStoreAllowUI,
	)
}

func TestDarwinDoctorResumeForbidsDeviceKeychainUI(t *testing.T) {
	slots, events := installDarwinDevicePolicyRecorder(t)
	credential := validCredential(211)
	slots[activeDeviceCredentialPendingService()] = credential
	previousActivate := activateDeviceV1ForPairing
	activateDeviceV1ForPairing = func(string, string, string) error {
		return nil
	}
	t.Cleanup(func() {
		activateDeviceV1ForPairing = previousActivate
	})
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg := runtimeConfig{
		ProfileID:            "profile-doctor-policy",
		PendingSecureBaseURL: "https://relay:8792",
		PendingSpkiPin:       "pin",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	snapshot, hadSnapshot, err := readOptionalFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := resumeInterruptedPairingForDoctor(
		paths,
		&cfg,
		nil,
		snapshot,
		hadSnapshot,
	)
	if err != nil || !resumed {
		t.Fatalf("Doctor resume=%v err=%v", resumed, err)
	}
	assertAllDarwinPolicies(t, *events, SecretStoreForbidUI)
}

func TestDarwinExplicitPairResumeAllowsDeviceKeychainUI(t *testing.T) {
	slots, events := installDarwinDevicePolicyRecorder(t)
	credential := validCredential(212)
	slots[activeDeviceCredentialPendingService()] = credential
	previousActivate := activateDeviceV1ForPairing
	activateDeviceV1ForPairing = func(string, string, string) error {
		return nil
	}
	t.Cleanup(func() {
		activateDeviceV1ForPairing = previousActivate
	})
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg := runtimeConfig{
		ProfileID:            "profile-pair-policy",
		PendingSecureBaseURL: "https://relay:8792",
		PendingSpkiPin:       "pin",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	snapshot, hadSnapshot, err := readOptionalFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := resumeInterruptedPairingForExplicitPair(
		paths,
		&cfg,
		nil,
		snapshot,
		hadSnapshot,
	)
	if err != nil || !resumed {
		t.Fatalf("explicit pair resume=%v err=%v", resumed, err)
	}
	assertAllDarwinPolicies(t, *events, SecretStoreAllowUI)
}

func TestDarwinInteractiveSetupResumeAllowsDeviceKeychainUI(t *testing.T) {
	slots, events := installDarwinDevicePolicyRecorder(t)
	credential := validCredential(213)
	slots[activeDeviceCredentialPendingService()] = credential
	previousActivate := activateDeviceV1ForPairing
	activateDeviceV1ForPairing = func(string, string, string) error {
		return nil
	}
	t.Cleanup(func() {
		activateDeviceV1ForPairing = previousActivate
	})
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg := runtimeConfig{
		ProfileID:            "profile-setup-policy",
		PendingSecureBaseURL: "https://relay:8792",
		PendingSpkiPin:       "pin",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	resumed, err := resumeInteractiveSetupPendingActivation(
		paths,
		&cfg,
	)
	if err != nil || !resumed {
		t.Fatalf("interactive setup resume=%v err=%v", resumed, err)
	}
	assertAllDarwinPolicies(t, *events, SecretStoreAllowUI)
}

func TestDarwinPairCommandFileModeForbidsKeychainUI(t *testing.T) {
	slots, events := installDarwinDevicePolicyRecorder(t)
	previousPrompt := deviceCredentialPromptSessionAvailable
	deviceCredentialPromptSessionAvailable = func() bool { return false }
	deviceCredentialPreflightWithContext = func(
		ctx context.Context,
		ui SecretStoreUIPolicy,
	) error {
		*events = append(*events, darwinDevicePolicyEvent{
			operation: "preflight",
			ui:        ui,
		})
		return darwinDeviceCredentialPreflight(ctx, ui)
	}
	t.Cleanup(func() {
		deviceCredentialPromptSessionAvailable = previousPrompt
	})
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	credential := validCredential(214)
	slots[activeDeviceCredentialPendingService()] = credential
	if err := saveConfig(
		paths,
		runtimeConfig{
			RelayBaseURL:         "http://relay:8791",
			PendingSecureBaseURL: "https://relay:8792",
			PendingSpkiPin:       "pin",
		},
	); err != nil {
		t.Fatal(err)
	}
	previousActivate := activateDeviceV1ForPairing
	activateDeviceV1ForPairing = func(
		_, _, got string,
	) error {
		if got != credential {
			t.Fatalf("resume credential = %q", got)
		}
		return nil
	}
	previousPair := runSecurePairingForPairCmd
	runSecurePairingForPairCmd = func(
		string,
		string,
		*runtimeConfig,
		func(*runtimeConfig) error,
		pairingClientInfo,
	) (string, error) {
		t.Fatal("resumed file-mode pairing asked for a new code")
		return "", nil
	}
	t.Cleanup(func() {
		activateDeviceV1ForPairing = previousActivate
		runSecurePairingForPairCmd = previousPair
	})

	if code := runPairCommand(
		paths,
		[]string{
			"--code", "123456",
			"--credential-store", "file",
		},
	); code != 0 {
		t.Fatalf("file-backed pair exit = %d", code)
	}
	assertAllDarwinPolicies(t, *events, SecretStoreForbidUI)
}

func assertDarwinCredentialSlotPolicy(
	t *testing.T,
	events []darwinDevicePolicyEvent,
	want SecretStoreUIPolicy,
) {
	t.Helper()
	checked := 0
	for _, event := range events {
		if event.service != activeDeviceCredentialService() &&
			event.service != activeDeviceCredentialPendingService() {
			continue
		}
		checked++
		if event.ui != want {
			t.Fatalf("credential event %+v; want UI policy %q", event, want)
		}
	}
	if checked < 4 {
		t.Fatalf("only %d credential-slot policy events: %+v", checked, events)
	}
}

func assertAllDarwinPolicies(
	t *testing.T,
	events []darwinDevicePolicyEvent,
	want SecretStoreUIPolicy,
) {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("no secure-storage policy events recorded")
	}
	for _, event := range events {
		if event.ui != want {
			t.Fatalf("secure-storage event %+v; want UI policy %q", event, want)
		}
	}
}
