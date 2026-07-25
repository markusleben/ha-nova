package main

import (
	"context"
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
