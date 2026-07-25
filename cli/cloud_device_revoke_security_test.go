package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRemoteDeviceBearerIsNotSentBeforeExactSessionBinding(t *testing.T) {
	envelope := productionCloudTestEnvelope()
	envelope.ProfileID = "profile-1"
	origin, err := cloudOriginFromCanonical(envelope.CanonicalOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, envelope)
	cfg := runtimeConfig{
		ProfileID:       envelope.ProfileID,
		RelayInstanceID: envelope.RelayInstanceID,
		RoutePolicy:     routePolicyCloud,
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateReady,
			Current: &metadata,
		},
	}
	credential := validCredential(21)

	for name, mutate := range map[string]func(*cloudVerifiedSession){
		"different Home Assistant user": func(session *cloudVerifiedSession) {
			session.User.ID = "user-other"
		},
		"different Relay": func(session *cloudVerifiedSession) {
			session.Relay.RelayInstanceID = "relay-other"
		},
		"different authorization generation": func(
			session *cloudVerifiedSession,
		) {
			session.Envelope.Generation = strings.Repeat("e", 32)
		},
	} {
		t.Run(name, func(t *testing.T) {
			requests := 0
			ingress := newProtocolTestCloudIngressClient(
				t,
				roundTripFunc(func(*http.Request) (*http.Response, error) {
					requests++
					return &http.Response{
						StatusCode: http.StatusOK,
						Header: http.Header{
							relayVersionHeader: []string{"1.2.3"},
						},
						Body: io.NopCloser(strings.NewReader(
							`{"ok":true,"data":{"device_id":"device-1","revoked":true,"changed":true}}`,
						)),
					}, nil
				}),
			)
			session := cloudVerifiedSession{
				Envelope: envelope,
				User:     HACurrentUser{ID: metadata.HAUserID},
				Relay: CloudRelayInfo{
					RelayInstanceID: cfg.RelayInstanceID,
				},
				Ingress: ingress,
			}
			mutate(&session)
			err := revokeRemoteCloudDeviceWithVerifiedSession(
				context.Background(),
				cfg,
				metadata,
				credential,
				session,
			)
			if err == nil {
				t.Fatal("mismatched verified session was accepted")
			}
			if requests != 0 {
				t.Fatalf(
					"device bearer reached an unproven Relay: requests=%d",
					requests,
				)
			}
		})
	}
}

func TestRemoteCloudDeviceRevocationUsesFullyVerifiedProductionSession(
	t *testing.T,
) {
	server := newProductionCloudProtocolServer(t)
	defer server.Close()
	previousClient := cloudHTTPClientForCLI
	cloudHTTPClientForCLI = productionCloudMappedClient(t, server)
	t.Cleanup(func() {
		cloudHTTPClientForCLI = previousClient
	})

	backend := newMemoryOAuthSecretBackend()
	store := productionCloudTestStore(t, backend)
	pending, err := store.CreatePending(
		context.Background(),
		productionCloudTestEnvelope(),
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.PromotePending(
		context.Background(),
		pending.Generation,
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(current.CanonicalOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, current)
	cfg := runtimeConfig{
		ProfileID:       current.ProfileID,
		RelayInstanceID: current.RelayInstanceID,
		RoutePolicy:     routePolicyCloud,
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateReady,
			Current: &metadata,
		},
	}
	if err := revokeRemoteCloudDevice(
		context.Background(),
		cfg,
		store,
		validCredential(23),
	); err != nil {
		t.Fatalf("production remote device revoke: %v", err)
	}
}

func TestVerifiedRemoteDeviceRevocationRetainsStateOnRelayRejection(
	t *testing.T,
) {
	envelope := productionCloudTestEnvelope()
	origin, err := cloudOriginFromCanonical(envelope.CanonicalOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, envelope)
	cfg := runtimeConfig{
		ProfileID:       envelope.ProfileID,
		RelayInstanceID: envelope.RelayInstanceID,
		RoutePolicy:     routePolicyCloud,
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateReady,
			Current: &metadata,
		},
	}
	requests := 0
	ingress := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header: http.Header{
					relayVersionHeader: []string{"1.2.3"},
				},
				Body: io.NopCloser(strings.NewReader(
					`{"ok":false,"error":{"code":"UNAUTHORIZED"}}`,
				)),
			}, nil
		}),
	)
	session := cloudVerifiedSession{
		Envelope: envelope,
		User:     HACurrentUser{ID: metadata.HAUserID},
		Relay: CloudRelayInfo{
			RelayInstanceID: cfg.RelayInstanceID,
		},
		Ingress: ingress,
	}

	err = revokeRemoteCloudDeviceWithVerifiedSession(
		context.Background(),
		cfg,
		metadata,
		validCredential(75),
		session,
	)
	if !IsCloudErrorCode(err, CloudErrDeviceRejected) || requests != 1 {
		t.Fatalf("Relay rejection err=%v requests=%d", err, requests)
	}
}

func TestVerifiedRemoteRevocationUsesCloudVerifiedPendingCheckpoint(
	t *testing.T,
) {
	envelope := productionCloudTestEnvelope()
	envelope.ProfileID = "profile-1"
	origin, err := cloudOriginFromCanonical(envelope.CanonicalOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, envelope)
	credential := validCredential(76)
	deviceID := deviceCredentialID(credential)
	requests := 0
	ingress := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					relayVersionHeader: []string{"1.2.3"},
				},
				Body: io.NopCloser(strings.NewReader(fmt.Sprintf(
					`{"ok":true,"data":{"device_id":%q,"revoked":true,"changed":true}}`,
					deviceID,
				))),
			}, nil
		}),
	)
	cfg := runtimeConfig{
		ProfileID:       envelope.ProfileID,
		RelayInstanceID: envelope.RelayInstanceID,
		RoutePolicy:     routePolicyCloud,
		Cloud: &cloudLifecycleMetadata{
			State:                   cloudStateCloudVerified,
			Pending:                 &metadata,
			DeviceActivationStarted: true,
		},
	}
	session := cloudVerifiedSession{
		Envelope: envelope,
		User:     HACurrentUser{ID: metadata.HAUserID},
		Relay: CloudRelayInfo{
			RelayInstanceID: envelope.RelayInstanceID,
		},
		Ingress: ingress,
	}

	if err := revokeRemoteCloudDeviceWithVerifiedSession(
		context.Background(),
		cfg,
		metadata,
		credential,
		session,
	); err != nil {
		t.Fatalf("revoke Cloud-verified pending device: %v", err)
	}
	if requests != 1 {
		t.Fatalf("Relay requests = %d, want 1", requests)
	}
}
