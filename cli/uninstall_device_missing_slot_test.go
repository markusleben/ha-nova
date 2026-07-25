package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfilePurgeRejectsCopiedBearerWithoutCurrentDeviceSlot(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	paths := writeTestConfigFile(t, `{
		"schema_version":3,
		"ha_url":"http://ha.local:8123",
		"relay_base_url":"http://ha.local:8791",
		"relay_token_file":"relay-token",
		"relay_secure_base_url":"https://ha.local:8792",
		"relay_spki_pin":"pin"
	}`)
	if err := os.WriteFile(
		filepath.Join(paths.ConfigDir, "relay-token"),
		[]byte("copied-legacy-bearer\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	targets, err := collectProfilePurgeTargets(paths)
	if err != nil {
		t.Fatal(err)
	}
	err = validateProfilePurgeTargets(targets)
	if err == nil ||
		!strings.Contains(err.Error(), "current credential is missing") {
		t.Fatalf("copied-bearer cleanup error = %v", err)
	}
}

func TestProfilePurgeRejectsMissingPendingSlotForRecordedEndpoint(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	target := profilePurgeTarget{
		name:                 defaultServerProfileName,
		pendingSecureBaseURL: "https://ha.local:8792",
		pendingSpkiPin:       "pin",
	}

	err := validateProfilePurgeTargets([]profilePurgeTarget{target})
	if err == nil ||
		!strings.Contains(err.Error(), "pending credential is missing") {
		t.Fatalf("missing-pending cleanup error = %v", err)
	}
}

func TestProfilePurgeAcceptsInterruptedFirstCredentialPromotion(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	if err := writePendingDeviceCredential(validCredential(151)); err != nil {
		t.Fatal(err)
	}
	target := profilePurgeTarget{
		name:                 defaultServerProfileName,
		secureBaseURL:        "https://ha.local:8792",
		spkiPin:              "pin",
		pendingSecureBaseURL: "https://ha.local:8792",
		pendingSpkiPin:       "pin",
	}

	if err := validateProfilePurgeTargets(
		[]profilePurgeTarget{target},
	); err != nil {
		t.Fatalf("interrupted first promotion was rejected: %v", err)
	}
}

func TestProfilePurgeCompletesKeyringToFileMigrationBeforeSlotCheck(
	t *testing.T,
) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	resetServerProfileSelection(t)
	credential := validCredential(155)
	service := deviceCredentialServiceForProfile(
		defaultServerProfileName,
	)
	if err := writeDeviceCredential(credential); err != nil {
		t.Fatal(err)
	}
	if err := deviceSecretFileSet(service, credential); err != nil {
		t.Fatal(err)
	}
	if err := writeKeyringDeviceCredentialCleanup(
		[]string{service},
	); err != nil {
		t.Fatal(err)
	}
	originalRevoke := revokeSelfDeviceV1ForUninstall
	revokeSelfDeviceV1ForUninstall = func(
		_, _, got string,
	) error {
		if got != credential {
			t.Fatalf("revoke credential = %q", got)
		}
		return nil
	}
	t.Cleanup(func() {
		revokeSelfDeviceV1ForUninstall = originalRevoke
	})

	err := purgeProfileDeviceCredentialWithReport(
		profilePurgeTarget{
			name:          defaultServerProfileName,
			secureBaseURL: "https://ha.local:8792",
			spkiPin:       "pin",
		},
		&uninstallReport{},
		false,
	)
	if err != nil {
		t.Fatalf("purge during migration recovery: %v", err)
	}
	if _, exists, readErr :=
		readKeyringDeviceCredentialCleanup(); readErr != nil || exists {
		t.Fatalf(
			"migration cleanup remains: exists=%v err=%v",
			exists,
			readErr,
		)
	}
}

func TestProfilePurgeAcceptsMissingSlotAfterExactCloudCheckpoint(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	envelope := productionCloudTestEnvelope()
	origin, err := cloudOriginFromCanonical(envelope.CanonicalOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, envelope)
	revokedCredential := validCredential(152)
	cfg := runtimeConfig{
		ProfileID:            envelope.ProfileID,
		RelayInstanceID:      envelope.RelayInstanceID,
		RoutePolicy:          routePolicyAutomatic,
		RelaySecureBaseURL:   "https://ha.local:8792",
		RelaySpkiPin:         "pin",
		PendingSecureBaseURL: "",
		PendingSpkiPin:       "",
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateReady,
			Current: &metadata,
			DeviceRevocationCompleted: &cloudDeviceRevocationCheckpoint{
				CurrentDeviceID: deviceIDOf(revokedCredential),
			},
		},
	}
	target, err := profilePurgeTargetFromConfig(
		defaultServerProfileName,
		cfg,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := validateProfilePurgeTargets(
		[]profilePurgeTarget{target},
	); err != nil {
		t.Fatalf("checkpointed missing slot was rejected: %v", err)
	}
	if err := purgeProfileDeviceCredentialWithReport(
		target,
		&uninstallReport{},
		false,
	); err != nil {
		t.Fatalf("checkpointed missing slot purge failed: %v", err)
	}
}

func TestFullPurgeMissingSiblingSlotPreservesEveryProfileAndConfig(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	paths := writeTestConfigFile(t, `{
		"schema_version":3,
		"default_server":"default",
		"servers":{
			"default":{
				"profile_id":"profile-default",
				"relay_secure_base_url":"https://default.local:8792",
				"relay_spki_pin":"pin-default",
				"route_policy":"local"
			},
			"cabin":{
				"profile_id":"profile-cabin",
				"relay_secure_base_url":"https://cabin.local:8792",
				"relay_spki_pin":"pin-cabin",
				"route_policy":"local"
			}
		}
	}`)
	defaultCredential := validCredential(153)
	if err := writeDeviceCredential(defaultCredential); err != nil {
		t.Fatal(err)
	}
	revokeCalls := 0
	originalRevoke := revokeSelfDeviceV1ForUninstall
	revokeSelfDeviceV1ForUninstall = func(string, string, string) error {
		revokeCalls++
		return nil
	}
	t.Cleanup(func() {
		revokeSelfDeviceV1ForUninstall = originalRevoke
	})

	err := finalizeLocalUninstallWithProgressUnlocked(
		paths,
		installState{},
		&uninstallReport{},
		uninstallModePurge,
		nil,
		nil,
	)
	if err == nil ||
		!strings.Contains(err.Error(), `server "cabin"`) ||
		!strings.Contains(err.Error(), "current credential is missing") {
		t.Fatalf("multi-profile purge error = %v", err)
	}
	if revokeCalls != 0 {
		t.Fatalf("purge revoked before validating every profile: %d", revokeCalls)
	}
	if _, statErr := os.Stat(paths.ConfigFile); statErr != nil {
		t.Fatalf("config was removed after failed validation: %v", statErr)
	}
	got, exists, readErr := readDeviceCredential()
	if readErr != nil || !exists || got != defaultCredential {
		t.Fatalf(
			"default credential changed: got=%q exists=%v err=%v",
			got,
			exists,
			readErr,
		)
	}
}
