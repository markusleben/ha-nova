package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestRemoteCloudSetupReopensDeviceStorageAfterOAuthRelock(t *testing.T) {
	request, store, session, result := remoteRelockCoordinatorFixture(t)
	relocked := false
	var events []string
	installRemoteRelockCoordinatorHooks(
		t,
		func(
			productionCloudSetupCoordinator,
			context.Context,
			cloudSetupRequest,
			CloudOrigin,
			string,
		) (cloudSetupResult, cloudVerifiedSession, OAuthSecretStore, error) {
			events = append(events, "authorized")
			relocked = true
			return result, session, store, nil
		},
		func(_ context.Context, ui SecretStoreUIPolicy) (deviceStorageProbe, error) {
			events = append(events, "device-unlock")
			if ui != SecretStoreAllowUI {
				t.Fatalf("post-authorization probe policy = %q", ui)
			}
			relocked = false
			return deviceStorageProbe{mode: "keyring"}, nil
		},
		func(
			context.Context,
			cloudVerifiedSession,
			cloudRemoteSetupRequest,
			string,
		) (string, error) {
			events = append(events, "device-operation")
			if relocked {
				return "", errors.New("device storage was not reopened")
			}
			return validCredential(151), nil
		},
	)

	got, err := (productionCloudSetupCoordinator{}).AddRemoteWithPairing(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.RelayInstanceID != session.Relay.RelayInstanceID ||
		strings.Join(events, ",") !=
			"authorized,device-unlock,device-operation" {
		t.Fatalf("result=%+v events=%v", got, events)
	}
}

func TestRemoteCloudSetupKeepsAuthorizationWhenPostOAuthUnlockDenied(
	t *testing.T,
) {
	request, store, session, result := remoteRelockCoordinatorFixture(t)
	locked := newCloudError(
		CloudErrSecretStoreLocked,
		"unlock relocked device storage",
		nil,
	)
	establishCalls := 0
	installRemoteRelockCoordinatorHooks(
		t,
		func(
			productionCloudSetupCoordinator,
			context.Context,
			cloudSetupRequest,
			CloudOrigin,
			string,
		) (cloudSetupResult, cloudVerifiedSession, OAuthSecretStore, error) {
			return result, session, store, nil
		},
		func(_ context.Context, ui SecretStoreUIPolicy) (deviceStorageProbe, error) {
			if ui != SecretStoreAllowUI {
				t.Fatalf("post-authorization probe policy = %q", ui)
			}
			return deviceStorageProbe{}, locked
		},
		func(
			context.Context,
			cloudVerifiedSession,
			cloudRemoteSetupRequest,
			string,
		) (string, error) {
			establishCalls++
			return "", nil
		},
	)

	_, err := (productionCloudSetupCoordinator{}).AddRemoteWithPairing(
		context.Background(),
		request,
	)
	if !errors.Is(err, locked) ||
		!strings.Contains(
			err.Error(),
			"re-open device secure storage after Home Assistant Cloud sign-in",
		) {
		t.Fatalf("post-OAuth unlock error = %v", err)
	}
	if establishCalls != 0 {
		t.Fatalf("device operation ran after denied unlock: %d", establishCalls)
	}
	if _, exists, loadErr := store.LoadPending(
		context.Background(),
		SecretStoreForbidUI,
	); loadErr != nil || !exists {
		t.Fatalf(
			"pending authorization was not kept for recovery: exists=%v err=%v",
			exists,
			loadErr,
		)
	}
}

func TestRemoteCloudSetupReopensDeviceStorageAfterOwnerCodeRelock(
	t *testing.T,
) {
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	resetServerProfileSelection(t)

	oldProbe := probeCloudDeviceStorageForSetup
	oldPending := readCloudPendingDeviceForSetup
	oldCurrent := readCloudDeviceForSetup
	oldPair := pairDeviceV2ForCloudSetup
	t.Cleanup(func() {
		probeCloudDeviceStorageForSetup = oldProbe
		readCloudPendingDeviceForSetup = oldPending
		readCloudDeviceForSetup = oldCurrent
		pairDeviceV2ForCloudSetup = oldPair
	})

	relocked := false
	ownerCodeReturned := false
	unlockAfterCode := 0
	assertReadable := func(ui SecretStoreUIPolicy) {
		t.Helper()
		if relocked {
			t.Fatalf("device storage read while relocked with policy %q", ui)
		}
	}
	probeCloudDeviceStorageForSetup = func(
		_ context.Context,
		ui SecretStoreUIPolicy,
	) (deviceStorageProbe, error) {
		if ui == SecretStoreAllowUI && ownerCodeReturned {
			relocked = false
			unlockAfterCode++
		}
		assertReadable(ui)
		return deviceStorageProbe{mode: "keyring"}, nil
	}
	readCloudPendingDeviceForSetup = func(
		_ context.Context,
		ui SecretStoreUIPolicy,
	) (pendingDeviceCredentialRecord, bool, error) {
		assertReadable(ui)
		return pendingDeviceCredentialRecord{}, false, nil
	}
	readCloudDeviceForSetup = func(
		_ context.Context,
		ui SecretStoreUIPolicy,
	) (string, bool, error) {
		assertReadable(ui)
		return "", false, nil
	}

	credential := validCredential(153)
	deviceID := deviceIDOf(credential)
	pairDeviceV2ForCloudSetup = func(
		_ context.Context,
		_ *CloudIngressClient,
		code string,
		_ deviceMetadata,
		expectedRelayInstanceID string,
	) (*cloudProvisionedCredential, error) {
		if relocked || unlockAfterCode != 1 {
			t.Fatalf(
				"pair ran before post-code unlock: relocked=%v unlocks=%d",
				relocked,
				unlockAfterCode,
			)
		}
		if code != "123456" ||
			expectedRelayInstanceID != "relay-recovery" {
			t.Fatalf(
				"pair input: code=%q relay=%q",
				code,
				expectedRelayInstanceID,
			)
		}
		return &cloudProvisionedCredential{
			Credential:      credential,
			DeviceID:        deviceID,
			RelayInstanceID: expectedRelayInstanceID,
		}, nil
	}
	ingress := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if !strings.HasSuffix(
				request.URL.Path,
				CloudPathDeviceActivate,
			) {
				t.Fatalf("unexpected Cloud path %q", request.URL.Path)
			}
			return cloudSetupRemoteTestResponse(
				http.StatusOK,
				`{"ok":true,"data":{"device_id":"`+
					deviceID+
					`","activated":true,"bound":true,"changed":true}}`,
			), nil
		}),
	)
	request := cloudSetupRemoteTestRequest(
		t,
		func(cloudRemotePairingPrompt) (string, error) {
			ownerCodeReturned = true
			relocked = true
			return "123456", nil
		},
	)

	got, err := establishRemoteCloudDevice(
		context.Background(),
		cloudSetupRemoteTestSession(ingress),
		request,
		"relay-recovery",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != credential || unlockAfterCode != 1 {
		t.Fatalf(
			"credential=%q unlocks-after-code=%d",
			got,
			unlockAfterCode,
		)
	}
}

func remoteRelockCoordinatorFixture(
	t *testing.T,
) (
	cloudRemoteSetupRequest,
	*KeyringOAuthSecretStore,
	cloudVerifiedSession,
	cloudSetupResult,
) {
	t.Helper()
	resetServerProfileSelection(t)
	envelope := productionCloudTestEnvelope()
	envelope.ProfileID = "profile-1"
	store := productionCloudTestStore(t, newMemoryOAuthSecretBackend())
	pending, err := store.CreatePending(
		context.Background(),
		envelope,
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(envelope.CanonicalOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, pending)
	request := cloudRemoteSetupRequest{
		cloudSetupRequest: cloudSetupRequest{
			Config: runtimeConfig{
				ProfileID:       envelope.ProfileID,
				ClientInstallID: "inst-relock",
				RelayInstanceID: envelope.RelayInstanceID,
			},
		},
		Origin: origin,
		PairingCode: func(cloudRemotePairingPrompt) (string, error) {
			t.Fatal("owner pairing prompt reached test seam")
			return "", nil
		},
	}
	session := cloudVerifiedSession{
		Envelope: pending,
		User:     HACurrentUser{ID: envelope.HAUserID},
		Relay: CloudRelayInfo{
			RelayInstanceID: envelope.RelayInstanceID,
		},
	}
	return request, store, session, cloudSetupResult{
		Current:         metadata,
		RelayInstanceID: envelope.RelayInstanceID,
	}
}

func installRemoteRelockCoordinatorHooks(
	t *testing.T,
	authorize func(
		productionCloudSetupCoordinator,
		context.Context,
		cloudSetupRequest,
		CloudOrigin,
		string,
	) (cloudSetupResult, cloudVerifiedSession, OAuthSecretStore, error),
	probe func(
		context.Context,
		SecretStoreUIPolicy,
	) (deviceStorageProbe, error),
	establish func(
		context.Context,
		cloudVerifiedSession,
		cloudRemoteSetupRequest,
		string,
	) (string, error),
) {
	t.Helper()
	oldAuthorize := authorizeAndVerifyCloudForSetup
	oldProbe := probeCloudDeviceStorageForSetup
	oldPending := readCloudPendingDeviceForSetup
	oldCurrent := readCloudDeviceForSetup
	oldEstablish := establishRemoteCloudDeviceForSetup
	authorizeAndVerifyCloudForSetup = authorize
	probeCloudDeviceStorageForSetup = probe
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
		return "", false, nil
	}
	establishRemoteCloudDeviceForSetup = establish
	t.Cleanup(func() {
		authorizeAndVerifyCloudForSetup = oldAuthorize
		probeCloudDeviceStorageForSetup = oldProbe
		readCloudPendingDeviceForSetup = oldPending
		readCloudDeviceForSetup = oldCurrent
		establishRemoteCloudDeviceForSetup = oldEstablish
	})
}
