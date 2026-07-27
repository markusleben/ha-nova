package main

import (
	"context"
	"errors"
	"testing"
)

func TestCloudRemoveDeletesCloudDevicePendingAfterVerifiedRevocation(
	t *testing.T,
) {
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	paths, store, _, _ := cloudRemoveCommandFixture(t)
	if err := writePendingCloudDeviceCredential(
		validCredential(2),
		"relay-1",
	); err != nil {
		t.Fatal(err)
	}
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error { return nil },
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 0 {
		t.Fatalf("Cloud remove exit=%d output=%s", exit, output)
	}
	if _, exists, err := readPendingDeviceCredentialRecord(); err != nil || exists {
		t.Fatalf(
			"Cloud remove retained device pending: exists=%v err=%v",
			exists,
			err,
		)
	}
}

func TestCloudRemovePreservesCloudDevicePendingWhenRevocationFails(
	t *testing.T,
) {
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	paths, store, _, _ := cloudRemoveCommandFixture(t)
	credential := validCredential(3)
	if err := writePendingCloudDeviceCredential(
		credential,
		"relay-1",
	); err != nil {
		t.Fatal(err)
	}
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			return newCloudError(
				CloudErrNetwork,
				"test revocation failure",
				errors.New("offline"),
			)
		},
	)

	exit, _ := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	record, exists, err := readPendingDeviceCredentialRecord()
	if exit != 1 ||
		err != nil ||
		!exists ||
		record.Credential != credential ||
		record.Source != pendingDeviceCredentialSourceCloud {
		t.Fatalf(
			"failed remove result exit=%d record=%+v exists=%v err=%v",
			exit,
			record,
			exists,
			err,
		)
	}
}

func TestCloudRemoveNeverDeletesLocalDevicePending(t *testing.T) {
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	paths, store, _, _ := cloudRemoveCommandFixture(t)
	credential := validCredential(4)
	if err := writePendingDeviceCredential(credential); err != nil {
		t.Fatal(err)
	}
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error { return nil },
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 0 {
		t.Fatalf("Cloud remove exit=%d output=%s", exit, output)
	}
	record, exists, err := readPendingDeviceCredentialRecord()
	if err != nil ||
		!exists ||
		record.Credential != credential ||
		record.Source != pendingDeviceCredentialSourceLocal {
		t.Fatalf(
			"Cloud remove changed local pending: %+v exists=%v err=%v",
			record,
			exists,
			err,
		)
	}
}
