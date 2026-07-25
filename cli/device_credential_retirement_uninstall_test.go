package main

import (
	"strings"
	"testing"
)

func pendingRetirementUninstallFixture(
	t *testing.T,
) (runtimePaths, runtimeConfig, string) {
	t.Helper()
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	credential := validCredential(97)
	if err := writeDeviceCredential(credential); err != nil {
		t.Fatal(err)
	}
	previous := runtimeConfig{
		ProfileID:          "profile-1",
		RelayInstanceID:    "relay-1",
		RelayBaseURL:       "http://relay:8791",
		RelaySecureBaseURL: "https://relay:8792",
		RelaySpkiPin:       "pin",
	}
	if err := saveConfig(paths, previous); err != nil {
		t.Fatal(err)
	}
	if err := writeDeviceCredentialRetirementCheckpoint(
		paths,
		previous,
	); err != nil {
		t.Fatal(err)
	}
	cleared := previous
	cleared.RelaySecureBaseURL = ""
	cleared.RelaySpkiPin = ""
	cleared.RelayInstanceID = ""
	if err := saveConfig(paths, cleared); err != nil {
		t.Fatal(err)
	}
	return paths, previous, credential
}

func TestPurgeSettlesPendingRetirementFromCheckpoint(t *testing.T) {
	paths, previous, credential :=
		pendingRetirementUninstallFixture(t)
	revokeCalls := 0
	originalRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(base, pin, got string) error {
		revokeCalls++
		if base != previous.RelaySecureBaseURL ||
			pin != previous.RelaySpkiPin ||
			got != credential {
			t.Fatalf("revoke = %q %q %q", base, pin, got)
		}
		return nil
	}
	t.Cleanup(func() { revokeSelfDeviceV1ForRetire = originalRevoke })

	if err := settleDeviceCredentialRetirementsForPurge(
		paths,
		&uninstallReport{},
		nil,
	); err != nil {
		t.Fatal(err)
	}
	if revokeCalls != 1 {
		t.Fatalf("revokes = %d", revokeCalls)
	}
	if _, exists, err := readDeviceCredential(); err != nil || exists {
		t.Fatalf("credential remains: exists=%v err=%v", exists, err)
	}
	if _, exists, err :=
		readDeviceCredentialRetirementCheckpoint(paths); err != nil ||
		exists {
		t.Fatalf("checkpoint remains: exists=%v err=%v", exists, err)
	}
}

func TestStandardUninstallBlocksPendingRetirement(t *testing.T) {
	paths, _, credential := pendingRetirementUninstallFixture(t)

	err := finalizeLocalUninstallWithProgressUnlocked(
		paths,
		installState{},
		&uninstallReport{},
		uninstallModeStandard,
		nil,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "retirement is pending") {
		t.Fatalf("standard uninstall error = %v", err)
	}
	got, exists, readErr := readDeviceCredential()
	if readErr != nil || !exists || got != credential {
		t.Fatalf(
			"credential changed: got=%q exists=%v err=%v",
			got,
			exists,
			readErr,
		)
	}
}

func TestPurgeConsumesRevokedCheckpointWithRestoredEndpoint(
	t *testing.T,
) {
	paths, previous, _ := pendingRetirementUninstallFixture(t)
	if err := saveConfig(paths, previous); err != nil {
		t.Fatal(err)
	}
	checkpoint, exists, err :=
		readDeviceCredentialRetirementCheckpoint(paths)
	if err != nil || !exists {
		t.Fatalf("checkpoint: exists=%v err=%v", exists, err)
	}
	if _, err := markDeviceCredentialRetirementRevoked(
		paths,
		checkpoint,
	); err != nil {
		t.Fatal(err)
	}
	revokeCalls := 0
	originalRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error {
		revokeCalls++
		return nil
	}
	t.Cleanup(func() { revokeSelfDeviceV1ForRetire = originalRevoke })

	if err := settleDeviceCredentialRetirementsForPurge(
		paths,
		&uninstallReport{},
		nil,
	); err != nil {
		t.Fatal(err)
	}
	if revokeCalls != 0 {
		t.Fatalf("already-revoked checkpoint retried %d revokes", revokeCalls)
	}
	if _, exists, err := readDeviceCredential(); err != nil || exists {
		t.Fatalf("credential remains: exists=%v err=%v", exists, err)
	}
	if _, exists, err :=
		readDeviceCredentialRetirementCheckpoint(paths); err != nil ||
		exists {
		t.Fatalf("checkpoint remains: exists=%v err=%v", exists, err)
	}
}

func TestGuidedTeardownSkipsOnlyMatchingRetirementRelay(t *testing.T) {
	for _, test := range []struct {
		name        string
		evidence    uninstallRelayRemovalEvidence
		wantRevokes int
	}{
		{
			name: "exact default Relay",
			evidence: uninstallRelayRemovalEvidence{
				defaultServerProfileName: "relay-1",
			},
			wantRevokes: 0,
		},
		{
			name: "sibling profile with same Relay id",
			evidence: uninstallRelayRemovalEvidence{
				"cabin": "relay-1",
			},
			wantRevokes: 1,
		},
		{
			name: "default profile with different Relay id",
			evidence: uninstallRelayRemovalEvidence{
				defaultServerProfileName: "relay-other",
			},
			wantRevokes: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			paths, _, _ := pendingRetirementUninstallFixture(t)
			revokeCalls := 0
			oldRevoke := revokeSelfDeviceV1ForRetire
			revokeSelfDeviceV1ForRetire = func(
				string,
				string,
				string,
			) error {
				revokeCalls++
				return nil
			}
			t.Cleanup(func() {
				revokeSelfDeviceV1ForRetire = oldRevoke
			})

			if err := settleDeviceCredentialRetirementsForPurge(
				paths,
				&uninstallReport{},
				test.evidence,
			); err != nil {
				t.Fatal(err)
			}
			if revokeCalls != test.wantRevokes {
				t.Fatalf(
					"retirement revokes = %d, want %d",
					revokeCalls,
					test.wantRevokes,
				)
			}
		})
	}
}
