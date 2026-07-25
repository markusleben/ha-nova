package main

import (
	"strings"
	"testing"
)

func TestPendingCloudCredentialCarriesRelayProvenanceThroughPromotion(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	credential := validCredential(21)

	if err := writePendingCloudDeviceCredential(
		credential,
		"relay-provenance",
	); err != nil {
		t.Fatal(err)
	}
	record, exists, err := readPendingDeviceCredentialRecord()
	if err != nil || !exists {
		t.Fatalf("read pending Cloud credential: exists=%v err=%v", exists, err)
	}
	if record.Credential != credential ||
		record.Source != pendingDeviceCredentialSourceCloud ||
		record.RelayInstanceID != "relay-provenance" {
		t.Fatalf("pending Cloud provenance = %+v", record)
	}
	raw, err := secretGet(activeDeviceCredentialPendingService())
	if err != nil {
		t.Fatal(err)
	}
	if raw == credential || !strings.Contains(raw, `"source":"cloud"`) {
		t.Fatal("Cloud pending slot did not preserve a provenance envelope")
	}

	if err := promotePendingDeviceCredential(); err != nil {
		t.Fatal(err)
	}
	current, exists, err := readDeviceCredential()
	if err != nil || !exists || current != credential {
		t.Fatalf(
			"promoted credential=%q exists=%v err=%v",
			current,
			exists,
			err,
		)
	}
}

func TestPendingCredentialEnvelopeValidationIsFailClosed(t *testing.T) {
	credential := validCredential(22)
	tests := map[string]string{
		"wrong source": `{"version":1,"source":"local","credential":"` +
			credential + `","relay_instance_id":"relay-1"}`,
		"missing relay": `{"version":1,"source":"cloud","credential":"` +
			credential + `","relay_instance_id":""}`,
		"future version": `{"version":2,"source":"cloud","credential":"` +
			credential + `","relay_instance_id":"relay-1"}`,
		"unknown field": `{"version":1,"source":"cloud","credential":"` +
			credential + `","relay_instance_id":"relay-1","extra":true}`,
		"trailing value": `{"version":1,"source":"cloud","credential":"` +
			credential + `","relay_instance_id":"relay-1"} {}`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := decodePendingDeviceCredentialRecord(raw); err == nil {
				t.Fatal("invalid pending credential envelope was accepted")
			}
		})
	}
}

func TestLocalPairingResumeNeverActivatesCloudPendingCredential(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	credential := validCredential(23)
	if err := writePendingCloudDeviceCredential(
		credential,
		"relay-cloud-only",
	); err != nil {
		t.Fatal(err)
	}

	previousActivate := activateDeviceV1ForPairing
	activationCalls := 0
	activateDeviceV1ForPairing = func(_, _, _ string) error {
		activationCalls++
		return nil
	}
	t.Cleanup(func() {
		activateDeviceV1ForPairing = previousActivate
	})

	cfg := runtimeConfig{
		PendingSecureBaseURL: "https://local.example:8792",
		PendingSpkiPin:       "pin",
	}
	resumed, err := resumePendingActivation(
		&cfg,
		func(*runtimeConfig) error { return nil },
	)
	if err == nil || resumed {
		t.Fatalf("Cloud pending local resume: resumed=%v err=%v", resumed, err)
	}
	if activationCalls != 0 {
		t.Fatalf("Cloud pending reached local activation %d times", activationCalls)
	}
	record, exists, readErr := readPendingDeviceCredentialRecord()
	if readErr != nil || !exists ||
		record.Source != pendingDeviceCredentialSourceCloud {
		t.Fatalf(
			"Cloud pending changed after blocked local resume: %+v %v %v",
			record,
			exists,
			readErr,
		)
	}
}

func TestCloudPendingProvenanceSurvivesKeyringToFileMigration(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	credential := validCredential(24)
	if err := writePendingCloudDeviceCredential(
		credential,
		"relay-migrated",
	); err != nil {
		t.Fatal(err)
	}

	migrated, err := migrateKeyringDeviceCredentialToFile()
	if err != nil || !migrated {
		t.Fatalf("migrate Cloud pending: migrated=%v err=%v", migrated, err)
	}
	record, exists, err := readPendingDeviceCredentialRecord()
	if err != nil || !exists ||
		record.Credential != credential ||
		record.Source != pendingDeviceCredentialSourceCloud ||
		record.RelayInstanceID != "relay-migrated" {
		t.Fatalf(
			"migrated Cloud pending=%+v exists=%v err=%v",
			record,
			exists,
			err,
		)
	}
}
