package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCloudVerifiedCleanupRevokesPendingAndCurrentForBothServerOutcomes(
	t *testing.T,
) {
	for _, test := range []struct {
		name        string
		serverState map[string]string
	}{
		{
			name: "activation not reached",
			serverState: map[string]string{
				"current": "active",
				"pending": "pending",
			},
		},
		{
			name: "activation completed before checkpoint",
			serverState: map[string]string{
				"pending": "active",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, currentCredential, pendingCredential :=
				activationUncertainCloudDeviceFixture(t)
			state := make(map[string]string, len(test.serverState))
			for credential, status := range test.serverState {
				state[credential] = status
			}
			var calls []string
			installRemoteDeviceRevokeHook(
				t,
				func(
					_ context.Context,
					got runtimeConfig,
					_ OAuthSecretStore,
					credential string,
				) error {
					switch credential {
					case pendingCredential:
						calls = append(calls, "pending")
						if got.Cloud == nil ||
							got.Cloud.State != cloudStateCloudVerified ||
							got.Cloud.Pending == nil {
							t.Fatalf("pending revoke config = %+v", got)
						}
						delete(state, "pending")
					case currentCredential:
						calls = append(calls, "current")
						if got.Cloud == nil ||
							got.Cloud.State != cloudStateReady ||
							got.Cloud.Pending != nil ||
							got.Cloud.Current == nil {
							t.Fatalf("current revoke config = %+v", got)
						}
						delete(state, "current")
					default:
						t.Fatalf("unexpected credential %q", credential)
					}
					return nil
				},
			)

			removed, err := revokeRemoteOnlyCloudDeviceBeforeOAuth(
				context.Background(),
				cfg,
				remoteOnlyCloudTestProfile,
				nil,
				nil,
				false,
				acceptCloudDeviceRevocationCheckpoint,
			)
			if err != nil || !removed {
				t.Fatalf("cleanup removed=%v err=%v", removed, err)
			}
			if strings.Join(calls, ",") != "pending,current" {
				t.Fatalf("device revocation order = %v", calls)
			}
			if len(state) != 0 {
				t.Fatalf("server records survived cleanup: %v", state)
			}
			assertActivationUncertainDeviceSlots(t, false)
		})
	}
}

func TestCloudVerifiedCleanupPreservesBothSlotsWhenSecondRevokeFails(
	t *testing.T,
) {
	cfg, currentCredential, pendingCredential :=
		activationUncertainCloudDeviceFixture(t)
	var calls []string
	installRemoteDeviceRevokeHook(
		t,
		func(
			_ context.Context,
			_ runtimeConfig,
			_ OAuthSecretStore,
			credential string,
		) error {
			switch credential {
			case pendingCredential:
				calls = append(calls, "pending")
				return nil
			case currentCredential:
				calls = append(calls, "current")
				return newCloudError(
					CloudErrOutcomeUnknown,
					"revoke current Cloud device",
					errors.New("response lost"),
				)
			default:
				t.Fatalf("unexpected credential %q", credential)
				return nil
			}
		},
	)

	removed, err := revokeRemoteOnlyCloudDeviceBeforeOAuth(
		context.Background(),
		cfg,
		remoteOnlyCloudTestProfile,
		nil,
		nil,
		false,
		acceptCloudDeviceRevocationCheckpoint,
	)
	if removed || !IsCloudErrorCode(err, CloudErrOutcomeUnknown) {
		t.Fatalf("cleanup removed=%v err=%v", removed, err)
	}
	if strings.Join(calls, ",") != "pending,current" {
		t.Fatalf("device revocation order = %v", calls)
	}
	assertActivationUncertainDeviceSlots(t, true)
}

func activationUncertainCloudDeviceFixture(
	t *testing.T,
) (runtimeConfig, string, string) {
	t.Helper()
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	setActiveServerProfile(remoteOnlyCloudTestProfile)

	envelope := productionCloudTestEnvelope()
	envelope.ProfileID = "profile-1"
	origin, err := cloudOriginFromCanonical(envelope.CanonicalOrigin)
	if err != nil {
		t.Fatal(err)
	}
	current := cloudMetadataFromEnvelope(origin, envelope)
	pending := current
	pending.CredentialGeneration = strings.Repeat("e", 32)
	pending.OAuthClientID = "http://127.0.0.1:54322/ha-nova"
	cfg := runtimeConfig{
		ProfileID:       envelope.ProfileID,
		RelayInstanceID: envelope.RelayInstanceID,
		RoutePolicy:     routePolicyCloud,
		Cloud: &cloudLifecycleMetadata{
			State:                   cloudStateCloudVerified,
			Current:                 &current,
			Pending:                 &pending,
			DeviceActivationStarted: true,
		},
	}
	currentCredential := validCredential(101)
	pendingCredential := validCredential(102)
	cfg.Cloud.DeviceActivationDeviceID = deviceIDOf(pendingCredential)
	if err := writeDeviceCredential(currentCredential); err != nil {
		t.Fatal(err)
	}
	if err := writePendingCloudDeviceCredential(
		pendingCredential,
		cfg.RelayInstanceID,
	); err != nil {
		t.Fatal(err)
	}
	return cfg, currentCredential, pendingCredential
}

func assertActivationUncertainDeviceSlots(t *testing.T, want bool) {
	t.Helper()
	if _, exists, err := readDeviceCredential(); err != nil || exists != want {
		t.Fatalf("current slot exists=%v want=%v err=%v", exists, want, err)
	}
	if _, exists, err := readPendingDeviceCredentialRecord(); err != nil ||
		exists != want {
		t.Fatalf("pending slot exists=%v want=%v err=%v", exists, want, err)
	}
}
