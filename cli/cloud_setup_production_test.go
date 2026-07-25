package main

import (
	"context"
	"strings"
	"testing"
)

const (
	productionCloudTestOrigin      = "https://unit.ui.nabu.casa"
	productionCloudTestClientID    = "http://127.0.0.1:43123/ha-nova"
	productionCloudTestGeneration  = "0123456789abcdef0123456789abcdef"
	productionCloudTestIngressRoot = "/api/hassio_ingress/abcdefghijklmnopqrstuvwxyzABCDEFGH123456789"
)

func TestProductionCoordinatorPreflightRefusesUnsafePromptSession(t *testing.T) {
	oldPromptSession := cloudInteractivePromptSessionForSetup
	oldStore := newCloudSecretStoreForCLI
	cloudInteractivePromptSessionForSetup = func() bool { return false }
	newCloudSecretStoreForCLI = func(string) (OAuthSecretStore, error) {
		t.Fatal("unsafe prompt session opened native secure storage")
		return nil, nil
	}
	t.Cleanup(func() {
		cloudInteractivePromptSessionForSetup = oldPromptSession
		newCloudSecretStoreForCLI = oldStore
	})

	err := (productionCloudSetupCoordinator{}).Preflight(
		context.Background(),
		"profile-1",
	)
	if !IsCloudErrorCode(err, CloudErrUnsupportedPlatform) {
		t.Fatalf("Preflight() error = %v, want UnsupportedPlatform", err)
	}
}

func TestProductionCloudModeIsNotOfferedOutsideSafePromptSession(t *testing.T) {
	oldCoordinator := cloudCoordinatorForSetup
	oldPromptSession := cloudInteractivePromptSessionForSetup
	cloudCoordinatorForSetup = productionCloudSetupCoordinator{}
	cloudInteractivePromptSessionForSetup = func() bool { return false }
	t.Cleanup(func() {
		cloudCoordinatorForSetup = oldCoordinator
		cloudInteractivePromptSessionForSetup = oldPromptSession
	})

	if shouldOfferSetupConnectionMode(
		runtimeConfig{},
		"",
		"",
		"",
		"",
		false,
	) {
		t.Fatal("unsafe prompt session offered Home Assistant Cloud setup")
	}
}

func TestProductionCoordinatorResumesPendingAuthorizationWithoutUI(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	store := productionCloudTestStore(t, backend)
	pending, err := store.CreatePending(
		context.Background(),
		productionCloudTestEnvelope(),
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatalf("CreatePending: %v", err)
	}
	resetProductionCloudPolicies(backend)

	server := newProductionCloudProtocolServer(t)
	defer server.Close()
	client := productionCloudMappedClient(t, server)
	oldStore := newCloudSecretStoreForCLI
	oldHTTP := cloudHTTPClientForCLI
	oldBrowser := openCloudOAuthBrowserForSetup
	newCloudSecretStoreForCLI = func(profileID string) (OAuthSecretStore, error) {
		if profileID != "profile-1" {
			t.Fatalf("secret-store profile = %q", profileID)
		}
		return store, nil
	}
	cloudHTTPClientForCLI = client
	openCloudOAuthBrowserForSetup = func(context.Context, string) error {
		t.Fatal("resumed authorization opened a browser")
		return nil
	}
	t.Cleanup(func() {
		newCloudSecretStoreForCLI = oldStore
		cloudHTTPClientForCLI = oldHTTP
		openCloudOAuthBrowserForSetup = oldBrowser
	})

	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, pending)
	cfg := runtimeConfig{
		ProfileID:       "profile-1",
		RelayInstanceID: "relay-1",
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateTokenStored,
			Pending: &metadata,
		},
	}
	var persisted []cloudConnectionMetadata
	var states []cloudLifecycleState
	request := cloudSetupRequest{
		Config: cfg,
		PersistPendingMetadata: func(value cloudConnectionMetadata) error {
			persisted = append(persisted, value)
			return nil
		},
		AdvancePendingLifecycle: func(state cloudLifecycleState) error {
			states = append(states, state)
			return nil
		},
	}

	result, session, returnedStore, err := (productionCloudSetupCoordinator{}).authorizeAndVerify(
		context.Background(),
		request,
		origin,
		"relay-1",
	)
	if err != nil {
		t.Fatalf("authorizeAndVerify: %v", err)
	}
	if returnedStore != store || result.RelayInstanceID != "relay-1" ||
		result.Current.HAUserID != "user-1" ||
		session.Envelope.RefreshTokenID != "refresh-1" {
		t.Fatalf("result=%+v session envelope=%+v", result, session.Envelope)
	}
	if len(persisted) != 2 ||
		len(states) != 2 ||
		states[0] != cloudStateTokenStored ||
		states[1] != cloudStateCloudVerified {
		t.Fatalf("persisted=%+v states=%v", persisted, states)
	}
	assertProductionCloudPolicies(t, backend, SecretStoreForbidUI)

	resetProductionCloudPolicies(backend)
	current, err := promoteCloudAuthorization(
		context.Background(),
		store,
		pending.Generation,
		session.Envelope,
	)
	if err != nil {
		t.Fatalf("promoteCloudAuthorization: %v", err)
	}
	if current.Generation != pending.Generation || current.State != OAuthSecretCurrent {
		t.Fatalf("promoted current = %+v", current)
	}
	assertProductionCloudPolicies(t, backend, SecretStoreForbidUI)
	cfg.Cloud.State = cloudStateDeviceBoundOrPaired

	resetProductionCloudPolicies(backend)
	result, _, _, err = (productionCloudSetupCoordinator{}).authorizeAndVerify(
		context.Background(),
		request,
		origin,
		"relay-1",
	)
	if err != nil || result.Current.CredentialGeneration != current.Generation {
		t.Fatalf("post-promotion authorizeAndVerify = %+v, %v", result, err)
	}
	assertProductionCloudPolicies(t, backend, SecretStoreForbidUI)

	resetProductionCloudPolicies(backend)
	resumed, alreadyCurrent, err := resumableCloudEnvelope(
		context.Background(),
		store,
		cfg,
		origin,
		SecretStoreForbidUI,
	)
	if err != nil || !alreadyCurrent || resumed.Generation != current.Generation {
		t.Fatalf("post-promotion resume = %+v current=%v err=%v", resumed, alreadyCurrent, err)
	}
	assertProductionCloudPolicies(t, backend, SecretStoreForbidUI)
}

func TestResumableCloudEnvelopeFailsClosedAtCrashBoundaries(t *testing.T) {
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	for name, testCase := range map[string]struct {
		change func(*runtimeConfig, *cloudConnectionMetadata, *OAuthSecretEnvelope)
		code   CloudErrorCode
	}{
		"profile": {
			change: func(cfg *runtimeConfig, _ *cloudConnectionMetadata, _ *OAuthSecretEnvelope) {
				cfg.ProfileID = "profile-other"
			},
			code: CloudErrIdentityMismatch,
		},
		"input origin": {
			change: func(_ *runtimeConfig, metadata *cloudConnectionMetadata, _ *OAuthSecretEnvelope) {
				metadata.Origin = "https://other.ui.nabu.casa"
			},
			code: CloudErrIdentityMismatch,
		},
		"canonical origin metadata": {
			change: func(_ *runtimeConfig, metadata *cloudConnectionMetadata, _ *OAuthSecretEnvelope) {
				metadata.CanonicalOrigin = "https://other.ui.nabu.casa"
			},
			code: CloudErrIdentityMismatch,
		},
		"canonical origin secret": {
			change: func(_ *runtimeConfig, _ *cloudConnectionMetadata, envelope *OAuthSecretEnvelope) {
				envelope.CanonicalOrigin = "https://other.ui.nabu.casa"
			},
			code: CloudErrIdentityMismatch,
		},
		"Home Assistant user": {
			change: func(_ *runtimeConfig, metadata *cloudConnectionMetadata, _ *OAuthSecretEnvelope) {
				metadata.HAUserID = "user-other"
			},
			code: CloudErrIdentityMismatch,
		},
		"Relay instance": {
			change: func(_ *runtimeConfig, _ *cloudConnectionMetadata, envelope *OAuthSecretEnvelope) {
				envelope.RelayInstanceID = "relay-other"
			},
			code: CloudErrRelayInstance,
		},
		"generation": {
			change: func(_ *runtimeConfig, metadata *cloudConnectionMetadata, _ *OAuthSecretEnvelope) {
				metadata.CredentialGeneration = strings.Repeat("f", 32)
			},
			code: CloudErrSecretConflict,
		},
		"OAuth client": {
			change: func(_ *runtimeConfig, metadata *cloudConnectionMetadata, _ *OAuthSecretEnvelope) {
				metadata.OAuthClientID = "http://127.0.0.1:43124/ha-nova"
			},
			code: CloudErrSecretConflict,
		},
	} {
		for _, secretState := range []OAuthSecretState{OAuthSecretPending, OAuthSecretCurrent} {
			t.Run(name+"/"+string(secretState), func(t *testing.T) {
				backend := newMemoryOAuthSecretBackend()
				store := productionCloudTestStore(t, backend)
				envelope := productionCloudTestEnvelope()
				metadata := cloudMetadataFromEnvelope(origin, envelope)
				cfg := runtimeConfig{
					ProfileID:       "profile-1",
					RelayInstanceID: "relay-1",
					Cloud: &cloudLifecycleMetadata{
						State:   cloudStateTokenStored,
						Pending: &metadata,
					},
				}
				testCase.change(&cfg, &metadata, &envelope)
				cfg.Cloud.Pending = &metadata
				pending, err := store.CreatePending(
					context.Background(),
					envelope,
					SecretStoreAllowUI,
				)
				if err != nil {
					t.Fatal(err)
				}
				if secretState == OAuthSecretCurrent {
					if _, err := store.PromotePending(
						context.Background(),
						pending.Generation,
						SecretStoreAllowUI,
					); err != nil {
						t.Fatal(err)
					}
					// A current secret can represent the pending generation
					// only after binding/pairing has completed and promotion
					// may have raced the next config checkpoint.
					cfg.Cloud.State = cloudStateDeviceBoundOrPaired
				}
				resetProductionCloudPolicies(backend)
				_, _, err = resumableCloudEnvelope(
					context.Background(),
					store,
					cfg,
					origin,
					SecretStoreForbidUI,
				)
				if !IsCloudErrorCode(err, testCase.code) {
					t.Fatalf("error = %v, want %s", err, testCase.code)
				}
				assertProductionCloudPolicies(t, backend, SecretStoreForbidUI)
			})
		}
	}

	t.Run("pre-verification user and Relay may still be unknown", func(t *testing.T) {
		backend := newMemoryOAuthSecretBackend()
		store := productionCloudTestStore(t, backend)
		envelope := productionCloudTestEnvelope()
		envelope.HAUserID = ""
		envelope.RelayInstanceID = ""
		pending, err := store.CreatePending(
			context.Background(),
			envelope,
			SecretStoreAllowUI,
		)
		if err != nil {
			t.Fatal(err)
		}
		metadata := cloudMetadataFromEnvelope(origin, pending)
		cfg := runtimeConfig{
			ProfileID: "profile-1",
			Cloud: &cloudLifecycleMetadata{
				State:   cloudStateTokenStored,
				Pending: &metadata,
			},
		}
		resetProductionCloudPolicies(backend)
		resumed, current, err := resumableCloudEnvelope(
			context.Background(),
			store,
			cfg,
			origin,
			SecretStoreForbidUI,
		)
		if err != nil || current || resumed.Generation != pending.Generation {
			t.Fatalf("resume = %+v current=%v err=%v", resumed, current, err)
		}
		assertProductionCloudPolicies(t, backend, SecretStoreForbidUI)
	})

	t.Run("durable state without secret", func(t *testing.T) {
		backend := newMemoryOAuthSecretBackend()
		store := productionCloudTestStore(t, backend)
		metadata := cloudMetadataFromEnvelope(origin, productionCloudTestEnvelope())
		cfg := runtimeConfig{
			ProfileID: "profile-1",
			Cloud: &cloudLifecycleMetadata{
				State:   cloudStateTokenStored,
				Pending: &metadata,
			},
		}
		_, _, err := resumableCloudEnvelope(
			context.Background(),
			store,
			cfg,
			origin,
			SecretStoreForbidUI,
		)
		if !IsCloudErrorCode(err, CloudErrSecretNotFound) {
			t.Fatalf("error = %v", err)
		}
		assertProductionCloudPolicies(t, backend, SecretStoreForbidUI)
	})

	t.Run("authorizing checkpoint may restart before token exists", func(t *testing.T) {
		backend := newMemoryOAuthSecretBackend()
		store := productionCloudTestStore(t, backend)
		metadata := cloudMetadataFromEnvelope(origin, productionCloudTestEnvelope())
		cfg := runtimeConfig{
			ProfileID: "profile-1",
			Cloud: &cloudLifecycleMetadata{
				State:   cloudStateAuthorizing,
				Pending: &metadata,
			},
		}
		envelope, current, err := resumableCloudEnvelope(
			context.Background(),
			store,
			cfg,
			origin,
			SecretStoreForbidUI,
		)
		if err != nil || current || envelope.Generation != "" {
			t.Fatalf("resume = %+v current=%v err=%v", envelope, current, err)
		}
		assertProductionCloudPolicies(t, backend, SecretStoreForbidUI)
	})
}
