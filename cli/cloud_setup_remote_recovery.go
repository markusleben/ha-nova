package main

import (
	"context"
	"fmt"
)

func discardRejectedPendingCloudDeviceCredential(
	ctx context.Context,
	request cloudRemoteSetupRequest,
) error {
	// Persist the definitive non-activation before deleting the secret. A
	// failed config save must leave both marker and credential available for a
	// safe retry; a crash after the save may leave a harmless rejected secret.
	if err := clearRemoteCloudDeviceActivation(request); err != nil {
		return err
	}
	if err := deletePendingDeviceCredentialWithPolicy(
		ctx,
		SecretStoreForbidUI,
	); err != nil {
		return fmt.Errorf(
			"clear rejected pending Cloud credential: %w",
			err,
		)
	}
	return nil
}
