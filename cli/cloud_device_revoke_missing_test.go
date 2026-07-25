package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloudRemoveMissingCurrentDeviceFailsBeforeOAuthAndKeepsProfile(
	t *testing.T,
) {
	paths, store, backend, current, _ := remoteOnlyCloudRemovalFixture(t)
	service := deviceCredentialServiceForProfile(remoteOnlyCloudTestProfile)
	if err := secretDelete(service); err != nil {
		t.Fatal(err)
	}
	deviceRevokes := 0
	installRemoteDeviceRevokeHook(
		t,
		func(context.Context, runtimeConfig, OAuthSecretStore, string) error {
			deviceRevokes++
			return nil
		},
	)
	oauthRevokes := 0
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			oauthRevokes++
			return nil
		},
	)
	resetProductionCloudPolicies(backend)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 1 {
		t.Fatalf("missing-device cloud remove exit=%d output=%s", exit, output)
	}
	for _, expected := range []string{
		"current Cloud device credential",
		`server "cabin"`,
		"Home Assistant Owner",
		"NOVA",
		"Devices",
		"Revoke",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing-device guidance lacks %q: %s", expected, output)
		}
	}
	if deviceRevokes != 0 || oauthRevokes != 0 {
		t.Fatalf(
			"missing device reached revocation: device=%d oauth=%d",
			deviceRevokes,
			oauthRevokes,
		)
	}
	held, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if held.Cloud == nil ||
		held.Cloud.Current == nil ||
		held.Cloud.Current.CredentialGeneration != current.Generation ||
		held.Cloud.RecoveryHold == nil ||
		held.Cloud.RecoveryHold.Code != cloudProblemAuthorization ||
		held.Cloud.RecoveryHold.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("missing-device profile was not durably held: %+v", held)
	}
	remaining, exists, err := store.LoadCurrent(
		context.Background(),
		SecretStoreForbidUI,
	)
	if err != nil || !exists || remaining.Generation != current.Generation {
		t.Fatalf(
			"OAuth changed after missing device: %+v exists=%v err=%v",
			remaining,
			exists,
			err,
		)
	}

	exit, output = captureCommandOutput(t, func() int {
		return runServerCommand(
			paths,
			[]string{"remove", remoteOnlyCloudTestProfile},
		)
	})
	if exit != 1 || !strings.Contains(output, "still has Home Assistant Cloud state") {
		t.Fatalf(
			"server remove bypassed missing-device hold: exit=%d output=%s",
			exit,
			output,
		)
	}
}

func TestCloudRemoveMissingActivationEraPendingDeviceFailsBeforeOAuth(
	t *testing.T,
) {
	paths, store, backend, _, currentCredential :=
		remoteOnlyCloudRemovalFixture(t)
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	pending := *cfg.Cloud.Current
	pending.CredentialGeneration = strings.Repeat("e", 32)
	pending.OAuthClientID = "http://127.0.0.1:54322/ha-nova"
	cfg.Cloud.State = cloudStateCloudVerified
	cfg.Cloud.Pending = &pending
	cfg.Cloud.DeviceActivationStarted = true
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	oauthRevokes := 0
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			oauthRevokes++
			return nil
		},
	)
	resetProductionCloudPolicies(backend)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 1 ||
		!strings.Contains(output, "activation-era pending Cloud device credential") {
		t.Fatalf(
			"missing pending cloud remove exit=%d output=%s",
			exit,
			output,
		)
	}
	if oauthRevokes != 0 {
		t.Fatalf("OAuth revoked after missing pending credential: %d", oauthRevokes)
	}
	stored, exists, err := readCredentialSlot(
		deviceCredentialServiceForProfile(remoteOnlyCloudTestProfile),
	)
	if err != nil || !exists || stored != currentCredential {
		t.Fatalf(
			"current device changed after missing pending: %q exists=%v err=%v",
			stored,
			exists,
			err,
		)
	}
	held, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if held.Cloud == nil ||
		held.Cloud.State != cloudStateCloudVerified ||
		held.Cloud.Pending == nil ||
		held.Cloud.RecoveryHold == nil ||
		held.Cloud.RecoveryHold.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("missing pending state was not durably held: %+v", held.Cloud)
	}
}

func TestCloudDeviceCleanupPropagatesBackendReadErrorAsNotMissing(
	t *testing.T,
) {
	_, _, _, _, _ = remoteOnlyCloudRemovalFixture(t)
	service := deviceCredentialServiceForProfile(remoteOnlyCloudTestProfile)
	if err := secretDelete(service); err != nil {
		t.Fatal(err)
	}
	secretDir := os.Getenv("HA_NOVA_TEST_SECRET_DIR")
	if secretDir == "" {
		t.Fatal("test secret directory is missing")
	}
	path := testSecretPath(secretDir, service)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := runtimeConfig{
		ProfileID:       "profile-1",
		RelayInstanceID: "relay-1",
		Cloud: &cloudLifecycleMetadata{
			State: cloudStateReady,
			Current: &cloudConnectionMetadata{
				Origin:               productionCloudTestOrigin,
				CanonicalOrigin:      productionCloudTestOrigin,
				OAuthClientID:        "http://127.0.0.1:43123/ha-nova",
				CredentialGeneration: strings.Repeat("a", 32),
				HAUserID:             "user-1",
			},
		},
	}

	removed, err := revokeRemoteOnlyCloudDeviceBeforeOAuth(
		context.Background(),
		cfg,
		remoteOnlyCloudTestProfile,
		nil,
		nil,
		false,
	)
	if removed || err == nil {
		t.Fatalf("backend read error removed=%v err=%v", removed, err)
	}
	var problem *cloudProblem
	if errors.As(err, &problem) {
		t.Fatalf("backend read error was misclassified as missing: %v", err)
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) ||
		filepath.Clean(pathErr.Path) != filepath.Clean(path) {
		t.Fatalf("backend read error lost provenance: %T %v", err, err)
	}
}

func TestFullPurgeMissingCurrentDeviceFailsBeforeOAuthAndPersistsHold(
	t *testing.T,
) {
	paths, store, backend, current, _ := remoteOnlyCloudRemovalFixture(t)
	if err := secretDelete(
		deviceCredentialServiceForProfile(remoteOnlyCloudTestProfile),
	); err != nil {
		t.Fatal(err)
	}
	oauthRevokes := 0
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			oauthRevokes++
			return nil
		},
	)
	resetProductionCloudPolicies(backend)

	err := purgeCloudAuthorizationsForUninstall(
		paths,
		&uninstallReport{},
		false,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "current Cloud device credential") {
		t.Fatalf("missing-device full purge error = %v", err)
	}
	if oauthRevokes != 0 {
		t.Fatalf("full purge revoked OAuth after missing device: %d", oauthRevokes)
	}
	held, loadErr := loadSelectedRuntimeConfigUnchecked(paths)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if held.Cloud == nil ||
		held.Cloud.Current == nil ||
		held.Cloud.Current.CredentialGeneration != current.Generation ||
		held.Cloud.RecoveryHold == nil ||
		held.Cloud.RecoveryHold.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("full purge did not preserve a durable hold: %+v", held)
	}
}
