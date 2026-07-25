package main

import (
	"context"
	"errors"
	"testing"
)

func TestInteractiveCloudDeviceReadOnlyPreflightAllowsSelectedSlotUI(
	t *testing.T,
) {
	oldProbe := probeCloudDeviceStorageForSetup
	oldPending := readCloudPendingDeviceForSetup
	oldCurrent := readCloudDeviceForSetup
	t.Cleanup(func() {
		probeCloudDeviceStorageForSetup = oldProbe
		readCloudPendingDeviceForSetup = oldPending
		readCloudDeviceForSetup = oldCurrent
	})

	policies := []SecretStoreUIPolicy{}
	probeCloudDeviceStorageForSetup = func(
		_ context.Context,
		ui SecretStoreUIPolicy,
	) (deviceStorageProbe, error) {
		t.Fatalf("read-only device preflight mutated storage with %q", ui)
		return deviceStorageProbe{}, nil
	}
	readCloudPendingDeviceForSetup = func(
		_ context.Context,
		ui SecretStoreUIPolicy,
	) (pendingDeviceCredentialRecord, bool, error) {
		policies = append(policies, ui)
		return pendingDeviceCredentialRecord{}, false, nil
	}
	readCloudDeviceForSetup = func(
		_ context.Context,
		ui SecretStoreUIPolicy,
	) (string, bool, error) {
		policies = append(policies, ui)
		return "", false, nil
	}

	if err := preflightCloudDeviceAccess(
		context.Background(),
		"",
		true,
		SecretStoreAllowUI,
	); err != nil {
		t.Fatal(err)
	}
	if len(policies) != 2 {
		t.Fatalf("device preflight policies = %v", policies)
	}
	for _, policy := range policies {
		if policy != SecretStoreAllowUI {
			t.Fatalf("device preflight policy = %q", policy)
		}
	}
}

func TestInteractiveCloudDeviceWritablePreflightProbesBeforeSlots(
	t *testing.T,
) {
	oldProbe := probeCloudDeviceStorageForSetup
	oldPending := readCloudPendingDeviceForSetup
	oldCurrent := readCloudDeviceForSetup
	t.Cleanup(func() {
		probeCloudDeviceStorageForSetup = oldProbe
		readCloudPendingDeviceForSetup = oldPending
		readCloudDeviceForSetup = oldCurrent
	})

	operations := []string{}
	probeCloudDeviceStorageForSetup = func(
		_ context.Context,
		ui SecretStoreUIPolicy,
	) (deviceStorageProbe, error) {
		operations = append(operations, "probe:"+string(ui))
		return deviceStorageProbe{mode: "keyring"}, nil
	}
	readCloudPendingDeviceForSetup = func(
		_ context.Context,
		ui SecretStoreUIPolicy,
	) (pendingDeviceCredentialRecord, bool, error) {
		operations = append(operations, "pending:"+string(ui))
		return pendingDeviceCredentialRecord{}, false, nil
	}
	readCloudDeviceForSetup = func(
		_ context.Context,
		ui SecretStoreUIPolicy,
	) (string, bool, error) {
		operations = append(operations, "current:"+string(ui))
		return "", false, nil
	}

	if err := preflightWritableCloudDeviceAccess(
		context.Background(),
		"",
		true,
		SecretStoreAllowUI,
	); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"probe:allow_ui",
		"pending:allow_ui",
		"current:allow_ui",
	}
	if len(operations) != len(want) {
		t.Fatalf("writable device preflight = %v", operations)
	}
	for index := range want {
		if operations[index] != want[index] {
			t.Fatalf(
				"writable device preflight[%d]=%q want %q",
				index,
				operations[index],
				want[index],
			)
		}
	}
}

func TestRuntimeCloudDevicePreflightForbidsNativeUI(t *testing.T) {
	oldProbe := probeCloudDeviceStorageForSetup
	oldPending := readCloudPendingDeviceForSetup
	oldCurrent := readCloudDeviceForSetup
	t.Cleanup(func() {
		probeCloudDeviceStorageForSetup = oldProbe
		readCloudPendingDeviceForSetup = oldPending
		readCloudDeviceForSetup = oldCurrent
	})

	assertForbidUI := func(ui SecretStoreUIPolicy) {
		t.Helper()
		if ui != SecretStoreForbidUI {
			t.Fatalf("runtime device preflight policy = %q", ui)
		}
	}
	probeCloudDeviceStorageForSetup = func(
		_ context.Context,
		ui SecretStoreUIPolicy,
	) (deviceStorageProbe, error) {
		t.Fatalf("runtime read-only preflight mutated storage with %q", ui)
		return deviceStorageProbe{}, nil
	}
	readCloudPendingDeviceForSetup = func(
		_ context.Context,
		ui SecretStoreUIPolicy,
	) (pendingDeviceCredentialRecord, bool, error) {
		assertForbidUI(ui)
		return pendingDeviceCredentialRecord{}, false, nil
	}
	readCloudDeviceForSetup = func(
		_ context.Context,
		ui SecretStoreUIPolicy,
	) (string, bool, error) {
		assertForbidUI(ui)
		return "", false, nil
	}

	if err := preflightRemoteCloudDeviceStateWithContext(
		context.Background(),
		"",
	); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteCloudDevicePreflightStopsUnprovenCurrentBeforeAuthorization(
	t *testing.T,
) {
	oldPending := readCloudPendingDeviceForSetup
	oldCurrent := readCloudDeviceForSetup
	oldAuthorize := authorizeAndVerifyCloudForSetup
	t.Cleanup(func() {
		readCloudPendingDeviceForSetup = oldPending
		readCloudDeviceForSetup = oldCurrent
		authorizeAndVerifyCloudForSetup = oldAuthorize
	})

	readCloudPendingDeviceForSetup = func(
		_ context.Context,
		ui SecretStoreUIPolicy,
	) (pendingDeviceCredentialRecord, bool, error) {
		if ui != SecretStoreForbidUI {
			t.Fatalf("pending device policy = %q", ui)
		}
		return pendingDeviceCredentialRecord{}, false, nil
	}
	readCloudDeviceForSetup = func(
		_ context.Context,
		ui SecretStoreUIPolicy,
	) (string, bool, error) {
		if ui != SecretStoreForbidUI {
			t.Fatalf("current device policy = %q", ui)
		}
		return validCredential(111), true, nil
	}
	authorizeAndVerifyCloudForSetup = func(
		productionCloudSetupCoordinator,
		context.Context,
		cloudSetupRequest,
		CloudOrigin,
		string,
	) (cloudSetupResult, cloudVerifiedSession, OAuthSecretStore, error) {
		t.Fatal("unproven current device reached Cloud authorization")
		return cloudSetupResult{}, cloudVerifiedSession{}, nil, nil
	}
	origin, err := cloudOriginFromCanonical("https://unit.ui.nabu.casa")
	if err != nil {
		t.Fatal(err)
	}
	pairingCalls := 0
	_, err = (productionCloudSetupCoordinator{}).AddRemoteWithPairing(
		context.Background(),
		cloudRemoteSetupRequest{
			cloudSetupRequest: cloudSetupRequest{
				Config: runtimeConfig{
					ClientInstallID: "install-unproven-current",
				},
			},
			Origin: origin,
			PairingCode: func(
				cloudRemotePairingPrompt,
			) (string, error) {
				pairingCalls++
				return "", nil
			},
		},
	)
	var problem *cloudProblem
	if !errors.As(err, &problem) ||
		problem.Code != cloudProblemIdentityMismatch ||
		problem.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("unproven current error = %v", err)
	}
	if pairingCalls != 0 {
		t.Fatalf("unproven current opened %d pairing prompts", pairingCalls)
	}
}

func TestRemoteCloudDevicePreflightAcceptsCurrentAfterRelayProof(t *testing.T) {
	oldPending := readCloudPendingDeviceForSetup
	oldCurrent := readCloudDeviceForSetup
	t.Cleanup(func() {
		readCloudPendingDeviceForSetup = oldPending
		readCloudDeviceForSetup = oldCurrent
	})

	readCloudPendingDeviceForSetup = func(
		context.Context,
		SecretStoreUIPolicy,
	) (pendingDeviceCredentialRecord, bool, error) {
		return pendingDeviceCredentialRecord{}, false, nil
	}
	readCloudDeviceForSetup = func(
		context.Context,
		SecretStoreUIPolicy,
	) (string, bool, error) {
		return validCredential(112), true, nil
	}
	if err := preflightRemoteCloudDeviceStateWithContext(
		context.Background(),
		"relay-proven-locally",
	); err != nil {
		t.Fatalf("proven current device rejected: %v", err)
	}
}

func TestRemoteCloudFlowStopsUnprovenCurrentBeforeWritableCanary(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	oldProbe := probeCloudDeviceStorageForSetup
	oldPending := readCloudPendingDeviceForSetup
	oldCurrent := readCloudDeviceForSetup
	t.Cleanup(func() {
		probeCloudDeviceStorageForSetup = oldProbe
		readCloudPendingDeviceForSetup = oldPending
		readCloudDeviceForSetup = oldCurrent
	})

	probeCloudDeviceStorageForSetup = func(
		context.Context,
		SecretStoreUIPolicy,
	) (deviceStorageProbe, error) {
		t.Fatal("unproven current device reached writable storage canary")
		return deviceStorageProbe{}, nil
	}
	readCloudPendingDeviceForSetup = func(
		context.Context,
		SecretStoreUIPolicy,
	) (pendingDeviceCredentialRecord, bool, error) {
		return pendingDeviceCredentialRecord{}, false, nil
	}
	readCloudDeviceForSetup = func(
		context.Context,
		SecretStoreUIPolicy,
	) (string, bool, error) {
		return validCredential(113), true, nil
	}
	origin, err := cloudOriginFromCanonical("https://unit.ui.nabu.casa")
	if err != nil {
		t.Fatal(err)
	}
	coordinator := newSelectingCloudCoordinator()
	saveCalls := 0
	_, err = connectRemoteToCloud(
		context.Background(),
		writeTestConfigFile(t, `{"schema_version":1}`),
		runtimeConfig{},
		coordinator,
		origin,
		func(cloudRemotePairingPrompt) (string, error) {
			t.Fatal("unproven current device opened owner pairing")
			return "", nil
		},
		false,
		func(runtimeConfig) error {
			saveCalls++
			return nil
		},
	)
	var problem *cloudProblem
	if !errors.As(err, &problem) ||
		problem.Code != cloudProblemIdentityMismatch ||
		problem.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("unproven current flow error = %v", err)
	}
	if saveCalls != 0 ||
		coordinator.preflightCalls != 0 ||
		coordinator.remoteCalls != 0 {
		t.Fatalf(
			"unproven current flow mutated state: saves=%d preflight=%d add=%d",
			saveCalls,
			coordinator.preflightCalls,
			coordinator.remoteCalls,
		)
	}
}
