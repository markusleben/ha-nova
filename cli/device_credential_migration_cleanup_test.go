package main

import (
	"context"
	"os"
	"testing"
)

func TestMigrateServiceDeviceCredentialResumesAmbiguousCleanup(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	credential := generateTestDeviceCredential(t)
	if err := writeDeviceCredential(credential); err != nil {
		t.Fatal(err)
	}
	originalDelete := secretKeyringDeleteWithPolicy
	deleteReturnedAmbiguous := false
	secretKeyringDeleteWithPolicy = func(
		ctx context.Context,
		service, account string,
		ui SecretStoreUIPolicy,
	) error {
		if err := originalDelete(ctx, service, account, ui); err != nil {
			return err
		}
		if !deleteReturnedAmbiguous {
			deleteReturnedAmbiguous = true
			return newCloudError(
				CloudErrSecretOutcomeUnknown,
				"delete migrated keyring credential",
				nil,
			)
		}
		return nil
	}
	t.Cleanup(func() {
		secretKeyringDeleteWithPolicy = originalDelete
	})

	migrated, err := migrateKeyringDeviceCredentialToFile()
	if !migrated ||
		!IsCloudErrorCode(err, CloudErrSecretOutcomeUnknown) {
		t.Fatalf(
			"ambiguous cleanup: migrated=%v err=%v",
			migrated,
			err,
		)
	}
	cleanupPath, err := keyringDeviceCredentialCleanupPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(cleanupPath); err != nil {
		t.Fatalf("cleanup checkpoint missing: %v", err)
	}
	secretKeyringDeleteWithPolicy = originalDelete
	resumed, err := migrateKeyringDeviceCredentialToFile()
	if err != nil || !resumed {
		t.Fatalf("cleanup resume: resumed=%v err=%v", resumed, err)
	}
	if _, err := os.Lstat(cleanupPath); !os.IsNotExist(err) {
		t.Fatalf("cleanup checkpoint remains: %v", err)
	}
	got, ok, err := readDeviceCredential()
	if err != nil || !ok || got != credential {
		t.Fatalf("file credential after cleanup: ok=%v err=%v", ok, err)
	}
}
