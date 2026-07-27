package main

import (
	"context"
	"strings"
	"testing"
)

func TestCloudRemoveManualRecoveryRequiresYesAndExactProfile(
	t *testing.T,
) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "yes required",
			args: []string{
				"--confirm-remote-access-revoked",
				defaultServerProfileName,
			},
			want: "requires --yes",
		},
		{
			name: "exact profile required",
			args: []string{
				"--yes",
				"--confirm-remote-access-revoked",
				"cabin",
			},
			want: "must exactly match selected server profile",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			paths, store, _, current := cloudRemoveCommandFixture(t)
			if err := store.DeleteCurrent(
				context.Background(),
				SecretStoreForbidUI,
			); err != nil {
				t.Fatal(err)
			}
			exit, output := captureCommandOutput(t, func() int {
				return runCloudRemoveCommand(paths, test.args)
			})
			if exit != 1 || !strings.Contains(output, test.want) {
				t.Fatalf("exit=%d output=%s", exit, output)
			}
			cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Cloud == nil ||
				cfg.Cloud.Current == nil ||
				cfg.Cloud.Current.CredentialGeneration !=
					current.Generation ||
				cfg.Cloud.RecoveryHold != nil {
				t.Fatalf("invalid confirmation mutated config: %+v", cfg.Cloud)
			}
		})
	}
}

func TestCloudRemoveRejectsUnnecessaryManualRecoveryFlag(
	t *testing.T,
) {
	paths, store, backend, current := cloudRemoveCommandFixture(t)
	revokeCalled := false
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			revokeCalled = true
			return nil
		},
	)
	resetProductionCloudPolicies(backend)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{
			"--yes",
			"--confirm-remote-access-revoked",
			defaultServerProfileName,
		})
	})
	if exit != 1 ||
		!strings.Contains(output, "accepted only when automatic cleanup") {
		t.Fatalf("exit=%d output=%s", exit, output)
	}
	if revokeCalled {
		t.Fatal("unnecessary manual flag reached remote revocation")
	}
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cloud == nil ||
		cfg.Cloud.Current == nil ||
		cfg.Cloud.Current.CredentialGeneration != current.Generation ||
		cfg.Cloud.RecoveryHold != nil {
		t.Fatalf("unnecessary manual flag mutated config: %+v", cfg.Cloud)
	}
}

func TestCloudRemoveManualRecoveryClearsMissingAuthorization(
	t *testing.T,
) {
	paths, store, backend, _ := cloudRemoveCommandFixture(t)
	if err := store.DeleteCurrent(
		context.Background(),
		SecretStoreForbidUI,
	); err != nil {
		t.Fatal(err)
	}
	remoteRevokeCalled := false
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			remoteRevokeCalled = true
			return nil
		},
	)
	resetProductionCloudPolicies(backend)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{
			"--yes",
			"--confirm-remote-access-revoked",
			defaultServerProfileName,
		})
	})
	if exit != 0 {
		t.Fatalf("exit=%d output=%s", exit, output)
	}
	if remoteRevokeCalled {
		t.Fatal("manual recovery repeated a remote OAuth revocation")
	}
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cloud != nil ||
		cfg.RoutePolicy != routePolicyLocal ||
		cfg.RelaySecureBaseURL == "" ||
		cfg.RelaySpkiPin == "" {
		t.Fatalf("manual recovery result = %+v", cfg)
	}
}

func TestCloudRemoveManualRecoveryClearsRemoteOnlyDeviceWithoutNetwork(
	t *testing.T,
) {
	paths, store, backend, _, credential :=
		remoteOnlyCloudRemovalFixture(t)
	if err := store.DeleteCurrent(
		context.Background(),
		SecretStoreForbidUI,
	); err != nil {
		t.Fatal(err)
	}
	deviceRevokeCalled := false
	installRemoteDeviceRevokeHook(
		t,
		func(context.Context, runtimeConfig, OAuthSecretStore, string) error {
			deviceRevokeCalled = true
			return nil
		},
	)
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			t.Fatal("manual recovery repeated remote OAuth revocation")
			return nil
		},
	)
	resetProductionCloudPolicies(backend)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{
			"--yes",
			"--confirm-remote-access-revoked",
			remoteOnlyCloudTestProfile,
		})
	})
	if exit != 0 {
		t.Fatalf("exit=%d output=%s", exit, output)
	}
	if deviceRevokeCalled {
		t.Fatal("manual recovery repeated remote device revocation")
	}
	service := deviceCredentialServiceForProfile(
		remoteOnlyCloudTestProfile,
	)
	if value, exists, err := readCredentialSlot(service); err != nil ||
		exists {
		t.Fatalf(
			"manual recovery retained device credential %q: exists=%v err=%v",
			value,
			exists,
			err,
		)
	}
	if strings.Contains(output, credential) {
		t.Fatal("manual recovery output exposed device credential")
	}
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cloud != nil ||
		cfg.RoutePolicy != routePolicyLocal ||
		cfg.RelayInstanceID != "" {
		t.Fatalf("remote-only manual recovery result = %+v", cfg)
	}
}

func TestCloudRemoveManualRecoveryCannotBypassCorruptSecret(
	t *testing.T,
) {
	paths, store, backend, current, credential :=
		remoteOnlyCloudRemovalFixture(t)
	backend.mu.Lock()
	backend.values[oauthSecretCurrentService+"\x00profile-1"] = "{broken"
	backend.mu.Unlock()
	deviceRevokeCalled := false
	installRemoteDeviceRevokeHook(
		t,
		func(context.Context, runtimeConfig, OAuthSecretStore, string) error {
			deviceRevokeCalled = true
			return nil
		},
	)
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			t.Fatal("corrupt OAuth slot reached remote revocation")
			return nil
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{
			"--yes",
			"--confirm-remote-access-revoked",
			remoteOnlyCloudTestProfile,
		})
	})
	if exit != 1 || !strings.Contains(output, "missing or inconsistent") {
		t.Fatalf("exit=%d output=%s", exit, output)
	}
	if deviceRevokeCalled {
		t.Fatal("corrupt OAuth slot bypass reached device cleanup")
	}
	value, exists, err := readCredentialSlot(
		deviceCredentialServiceForProfile(remoteOnlyCloudTestProfile),
	)
	if err != nil || !exists || value != credential {
		t.Fatalf(
			"corrupt-slot bypass changed device: value=%q exists=%v err=%v",
			value,
			exists,
			err,
		)
	}
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cloud == nil ||
		cfg.Cloud.Current == nil ||
		cfg.Cloud.Current.CredentialGeneration != current.Generation {
		t.Fatalf("corrupt-slot bypass changed config: %+v", cfg.Cloud)
	}
}

func TestCloudRemoveManualRecoveryRejectsUntrackedPendingCloudDevice(
	t *testing.T,
) {
	paths, store, backend, _, currentCredential :=
		remoteOnlyCloudRemovalFixture(t)
	if err := store.DeleteCurrent(
		context.Background(),
		SecretStoreForbidUI,
	); err != nil {
		t.Fatal(err)
	}
	setActiveServerProfile(remoteOnlyCloudTestProfile)
	pendingCredential := validCredential(121)
	if err := writePendingCloudDeviceCredential(
		pendingCredential,
		"relay-1",
	); err != nil {
		t.Fatal(err)
	}
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			t.Fatal("untracked pending device reached remote revocation")
			return nil
		},
	)
	resetProductionCloudPolicies(backend)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{
			"--yes",
			"--confirm-remote-access-revoked",
			remoteOnlyCloudTestProfile,
		})
	})
	if exit != 1 ||
		!strings.Contains(output, "pending Cloud device credential") {
		t.Fatalf("exit=%d output=%s", exit, output)
	}
	current, exists, err := readCredentialSlot(
		deviceCredentialServiceForProfile(remoteOnlyCloudTestProfile),
	)
	if err != nil || !exists || current != currentCredential {
		t.Fatalf(
			"current slot=%q exists=%v err=%v",
			current,
			exists,
			err,
		)
	}
	pending, exists, err := readPendingDeviceCredentialRecord()
	if err != nil ||
		!exists ||
		pending.Credential != pendingCredential ||
		pending.Source != pendingDeviceCredentialSourceCloud {
		t.Fatalf(
			"pending slot=%+v exists=%v err=%v",
			pending,
			exists,
			err,
		)
	}
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cloud == nil ||
		cfg.Cloud.DeviceRevocationCompleted != nil {
		t.Fatalf("untracked pending device wrote checkpoint: %+v", cfg.Cloud)
	}
}

func TestManualRecoveryFlagIsRemoveOnly(t *testing.T) {
	if _, err := parseCloudCommandFlags(
		"remove",
		[]string{
			"--yes",
			"--confirm-remote-access-revoked",
			defaultServerProfileName,
		},
	); err != nil {
		t.Fatalf("remove flag parse = %v", err)
	}
	if _, err := parseCloudCommandFlags(
		"status",
		[]string{
			"--confirm-remote-access-revoked",
			defaultServerProfileName,
		},
	); err == nil {
		t.Fatal("status accepted remove-only recovery flag")
	}
}
