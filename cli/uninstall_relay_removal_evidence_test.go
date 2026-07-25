package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestUninstallRelayRemovalEvidenceRequiresExactProfileAndRelay(t *testing.T) {
	evidence := uninstallRelayRemovalEvidenceFromPreflight(
		uninstallPreflight{
			config: runtimeConfig{RelayInstanceID: "relay-default"},
		},
		true,
	)
	if !evidence.matches(defaultServerProfileName, "relay-default") {
		t.Fatal("exact guided-teardown Relay was not recognized")
	}
	if evidence.matches("cabin", "relay-default") {
		t.Fatal("default teardown matched a sibling profile")
	}
	if evidence.matches(defaultServerProfileName, "relay-other") {
		t.Fatal("default teardown matched a different Relay")
	}
	if evidence.matches(defaultServerProfileName, "") {
		t.Fatal("default teardown matched an unknown Relay")
	}
	if uninstallRelayRemovalEvidenceFromPreflight(
		uninstallPreflight{},
		true,
	).matches(defaultServerProfileName, "relay-default") {
		t.Fatal("unknown guided-teardown identity was treated as evidence")
	}
}

func TestGuidedDefaultTeardownStillRevokesSiblingCloudDevice(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	paths := writeTestConfigFile(t, `{"schema_version":1}`)

	defaultConfig, defaultStore := readyCloudPurgeProfile(
		t,
		"profile-default-evidence",
		"relay-default",
	)
	cabinConfig, cabinStore := readyCloudPurgeProfile(
		t,
		"profile-cabin-evidence",
		"relay-cabin",
	)
	writeCloudRetryProfiles(
		t,
		paths,
		cabinConfig,
		defaultConfig,
	)
	if err := secretSet(
		deviceCredentialServiceForProfile(defaultServerProfileName),
		validCredential(131),
	); err != nil {
		t.Fatal(err)
	}
	if err := secretSet(
		deviceCredentialServiceForProfile("cabin"),
		validCredential(132),
	); err != nil {
		t.Fatal(err)
	}

	oldStore := newCloudSecretStoreForCLI
	oldOAuthRevoke := revokeAndVerifyCloudAuthorizationForCLI
	newCloudSecretStoreForCLI = func(
		profileID string,
	) (OAuthSecretStore, error) {
		switch profileID {
		case defaultConfig.ProfileID:
			return defaultStore, nil
		case cabinConfig.ProfileID:
			return cabinStore, nil
		default:
			t.Fatalf("unexpected profile id %q", profileID)
			return nil, nil
		}
	}
	revokeAndVerifyCloudAuthorizationForCLI = func(
		context.Context,
		OAuthSecretEnvelope,
	) error {
		return nil
	}
	t.Cleanup(func() {
		newCloudSecretStoreForCLI = oldStore
		revokeAndVerifyCloudAuthorizationForCLI = oldOAuthRevoke
	})

	var revokedRelays []string
	installRemoteDeviceRevokeHook(
		t,
		func(
			_ context.Context,
			cfg runtimeConfig,
			_ OAuthSecretStore,
			_ string,
		) error {
			revokedRelays = append(revokedRelays, cfg.RelayInstanceID)
			return nil
		},
	)
	if err := purgeCloudAuthorizationsForUninstall(
		paths,
		&uninstallReport{},
		uninstallRelayRemovalEvidence{
			defaultServerProfileName: "relay-default",
		},
	); err != nil {
		t.Fatal(err)
	}
	if len(revokedRelays) != 1 || revokedRelays[0] != "relay-cabin" {
		t.Fatalf("Cloud device revokes = %v", revokedRelays)
	}
}

func TestGuidedDefaultTeardownDoesNotSuppressSiblingDeviceWarning(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	defaultCredential := validCredential(133)
	cabinCredential := validCredential(134)
	if err := secretSet(
		deviceCredentialServiceForProfile(defaultServerProfileName),
		defaultCredential,
	); err != nil {
		t.Fatal(err)
	}
	if err := secretSet(
		deviceCredentialServiceForProfile("cabin"),
		cabinCredential,
	); err != nil {
		t.Fatal(err)
	}

	oldRevoke := revokeSelfDeviceV1ForUninstall
	revokeSelfDeviceV1ForUninstall = func(string, string, string) error {
		return errors.New("relay unavailable")
	}
	t.Cleanup(func() {
		revokeSelfDeviceV1ForUninstall = oldRevoke
	})
	report := &uninstallReport{}
	if err := purgeAllDeviceCredentialsWithReport(
		[]profilePurgeTarget{
			{
				name:            defaultServerProfileName,
				relayInstanceID: "relay-default",
				secureBaseURL:   "https://default:8792",
				spkiPin:         "pin-default",
			},
			{
				name:            "cabin",
				relayInstanceID: "relay-cabin",
				secureBaseURL:   "https://cabin:8792",
				spkiPin:         "pin-cabin",
			},
		},
		report,
		uninstallRelayRemovalEvidence{
			defaultServerProfileName: "relay-default",
		},
	); err != nil {
		t.Fatal(err)
	}
	warnings := 0
	for _, note := range report.notes {
		if strings.Contains(note, "Could not reach the relay") {
			warnings++
			if !strings.Contains(note, deviceCredentialID(cabinCredential)) {
				t.Fatalf("sibling warning = %q", note)
			}
		}
	}
	if warnings != 1 {
		t.Fatalf("unreachable Relay warnings = %d; notes=%v", warnings, report.notes)
	}
}

func readyCloudPurgeProfile(
	t *testing.T,
	profileID string,
	relayInstanceID string,
) (runtimeConfig, *KeyringOAuthSecretStore) {
	t.Helper()
	store, err := NewOAuthSecretStore(
		newMemoryOAuthSecretBackend(),
		profileID,
	)
	if err != nil {
		t.Fatal(err)
	}
	envelope := testOAuthEnvelope(
		"refresh-"+profileID,
		"token-"+profileID,
		"user-evidence",
		relayInstanceID,
	)
	pending, err := store.CreatePending(
		context.Background(),
		envelope,
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.PromotePending(
		context.Background(),
		pending.Generation,
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(current.CanonicalOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, current)
	return runtimeConfig{
		ProfileID:       profileID,
		RelayInstanceID: relayInstanceID,
		RoutePolicy:     routePolicyCloud,
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateReady,
			Current: &metadata,
		},
	}, store
}
