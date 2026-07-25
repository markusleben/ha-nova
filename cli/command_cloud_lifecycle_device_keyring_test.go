package main

import (
	"context"
	"strings"
	"testing"
)

func TestCloudStatusClassifiesLockedDeviceKeyringAsUnlockable(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := pendingCloudReconnectCommandConfig(cloudStateTokenStored)
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	installCloudCommandHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			return desktopKeyringLockedError("login keychain is locked")
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, nil)
	})
	if exit != 1 ||
		!strings.Contains(output, string(cloudProblemSecureStorage)) ||
		!strings.Contains(output, string(cloudRemediationUnlockStorage)) ||
		!strings.Contains(output, "ha-nova cloud unlock --server default") {
		t.Fatalf("locked device-keyring status exit=%d output=%s", exit, output)
	}
}
