package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRemoteCloudDeviceNeverActivatesLocalPendingCredential(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	if err := writePendingDeviceCredential(validCredential(24)); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	ingress := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests.Add(1)
			return nil, errors.New("request must not execute")
		}),
	)

	_, err := establishRemoteCloudDevice(
		context.Background(),
		cloudSetupRemoteTestSession(ingress),
		cloudSetupRemoteTestRequest(t, func(
			cloudRemotePairingPrompt,
		) (string, error) {
			t.Fatal("local pending opened Cloud owner pairing")
			return "", nil
		}),
		"relay-recovery",
	)
	if !IsCloudErrorCode(err, CloudErrDevicePendingConflict) {
		t.Fatalf("local pending Cloud setup error = %v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("local pending sent %d Cloud requests", requests.Load())
	}
}

func TestRemoteCloudDeviceRejectsPendingForAnotherRelayBeforeRequest(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	if err := writePendingCloudDeviceCredential(
		validCredential(25),
		"relay-other",
	); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	ingress := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests.Add(1)
			return nil, errors.New("request must not execute")
		}),
	)

	_, err := establishRemoteCloudDevice(
		context.Background(),
		cloudSetupRemoteTestSession(ingress),
		cloudSetupRemoteTestRequest(t, func(
			cloudRemotePairingPrompt,
		) (string, error) {
			t.Fatal("mismatched pending opened Cloud owner pairing")
			return "", nil
		}),
		"relay-recovery",
	)
	if !IsCloudErrorCode(err, CloudErrRelayInstance) {
		t.Fatalf("mismatched pending error = %v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("mismatched pending sent %d Cloud requests", requests.Load())
	}
}

func TestRemoteCloudDeviceAmbiguousPendingActivationStaysResumable(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	credential := validCredential(0)
	if err := writePendingCloudDeviceCredential(
		credential,
		"relay-recovery",
	); err != nil {
		t.Fatal(err)
	}
	ingress := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("response lost")
		}),
	)

	_, err := establishRemoteCloudDevice(
		context.Background(),
		cloudSetupRemoteTestSession(ingress),
		cloudSetupRemoteTestRequest(t, func(
			cloudRemotePairingPrompt,
		) (string, error) {
			t.Fatal("ambiguous activation opened a second pairing")
			return "", nil
		}),
		"relay-recovery",
	)
	if !IsCloudErrorCode(err, CloudErrNetwork) {
		t.Fatalf("ambiguous pending activation error = %v", err)
	}
	record, exists, readErr := readPendingDeviceCredentialRecord()
	if readErr != nil || !exists ||
		record.Credential != credential ||
		record.RelayInstanceID != "relay-recovery" {
		t.Fatalf(
			"ambiguous pending was not retained: %+v %v %v",
			record,
			exists,
			readErr,
		)
	}
}

func TestRemoteCloudDeviceDefinitivePendingRejectionAllowsFreshPairing(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	rejected := validCredential(1)
	replacement := validCredential(2)
	replacementID := parseDeviceCredential(replacement).deviceID
	if err := writePendingCloudDeviceCredential(
		rejected,
		"relay-recovery",
	); err != nil {
		t.Fatal(err)
	}
	requests := 0
	ingress := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			if !strings.HasSuffix(request.URL.Path, CloudPathDeviceActivate) {
				t.Fatalf("unexpected request path %q", request.URL.Path)
			}
			switch request.Header.Get("Authorization") {
			case "Bearer " + rejected:
				return cloudSetupRemoteTestResponse(
					http.StatusUnauthorized,
					`{"ok":false,"error":{"code":"UNAUTHORIZED","message":"generic"}}`,
				), nil
			case "Bearer " + replacement:
				return cloudSetupRemoteTestResponse(
					http.StatusOK,
					fmt.Sprintf(
						`{"ok":true,"data":{"device_id":%q,"activated":true,"bound":true,"changed":true}}`,
						replacementID,
					),
				), nil
			default:
				t.Fatalf(
					"unexpected activation credential %q",
					request.Header.Get("Authorization"),
				)
				return nil, errors.New("unexpected activation credential")
			}
		}),
	)
	previousPair := pairDeviceV2ForCloudSetup
	pairCalls := 0
	pairDeviceV2ForCloudSetup = func(
		_ context.Context,
		got *CloudIngressClient,
		code string,
		_ deviceMetadata,
		relayInstanceID string,
	) (*cloudProvisionedCredential, error) {
		pairCalls++
		if got != ingress ||
			code != "123456" ||
			relayInstanceID != "relay-recovery" {
			t.Fatalf(
				"fresh pairing args: client=%p code=%q relay=%q",
				got,
				code,
				relayInstanceID,
			)
		}
		if _, exists, err := readPendingDeviceCredential(); err != nil || exists {
			t.Fatalf(
				"definitively rejected pending was not cleared: %v %v",
				exists,
				err,
			)
		}
		return &cloudProvisionedCredential{
			Credential:      replacement,
			DeviceID:        replacementID,
			RelayInstanceID: relayInstanceID,
		}, nil
	}
	t.Cleanup(func() {
		pairDeviceV2ForCloudSetup = previousPair
	})
	promptCalls := 0
	got, err := establishRemoteCloudDevice(
		context.Background(),
		cloudSetupRemoteTestSession(ingress),
		cloudSetupRemoteTestRequest(t, func(
			cloudRemotePairingPrompt,
		) (string, error) {
			promptCalls++
			return "123456", nil
		}),
		"relay-recovery",
	)
	if err != nil {
		t.Fatalf("fresh Cloud pairing after rejection: %v", err)
	}
	if got != replacement ||
		requests != 2 ||
		pairCalls != 1 ||
		promptCalls != 1 {
		t.Fatalf(
			"fresh pairing result=%q requests=%d pair=%d prompt=%d",
			got,
			requests,
			pairCalls,
			promptCalls,
		)
	}
}

func TestExpectedRemoteCloudIdentityUsesStoredRelayWithoutLocalProbe(
	t *testing.T,
) {
	origin, err := cloudOriginFromCanonical("https://unit.ui.nabu.casa")
	if err != nil {
		t.Fatal(err)
	}
	previousDiscover := discoverCloudFromLocalRelayForRemoteSetup
	discoverCloudFromLocalRelayForRemoteSetup = func(
		context.Context,
		runtimeConfig,
	) (cloudLocalDiscovery, error) {
		t.Fatal("stored Relay identity triggered local discovery")
		return cloudLocalDiscovery{}, nil
	}
	t.Cleanup(func() {
		discoverCloudFromLocalRelayForRemoteSetup = previousDiscover
	})
	got, err := expectedRemoteCloudRelayIdentity(
		context.Background(),
		cloudRemoteSetupRequest{
			cloudSetupRequest: cloudSetupRequest{
				Config: runtimeConfig{
					RelayInstanceID:    "relay-stored",
					RelaySecureBaseURL: "https://local.example:8792",
					RelaySpkiPin:       "pin",
				},
			},
			Origin: origin,
		},
	)
	if err != nil || got != "relay-stored" {
		t.Fatalf("stored remote Relay identity = %q err=%v", got, err)
	}
}

func TestExpectedRemoteCloudIdentityProvesLegacyLocalProfileBeforeOAuth(
	t *testing.T,
) {
	origin, err := cloudOriginFromCanonical("https://unit.ui.nabu.casa")
	if err != nil {
		t.Fatal(err)
	}
	previousDiscover := discoverCloudFromLocalRelayForRemoteSetup
	discoverCalls := 0
	discoverCloudFromLocalRelayForRemoteSetup = func(
		_ context.Context,
		cfg runtimeConfig,
	) (cloudLocalDiscovery, error) {
		discoverCalls++
		if cfg.RelaySecureBaseURL == "" || cfg.RelaySpkiPin == "" {
			t.Fatal("local discovery did not receive the paired endpoint")
		}
		return cloudLocalDiscovery{
			Origin:          origin,
			RelayInstanceID: "relay-proven",
		}, nil
	}
	t.Cleanup(func() {
		discoverCloudFromLocalRelayForRemoteSetup = previousDiscover
	})

	got, err := expectedRemoteCloudRelayIdentity(
		context.Background(),
		cloudRemoteSetupRequest{
			cloudSetupRequest: cloudSetupRequest{
				Config: runtimeConfig{
					RelaySecureBaseURL: "https://local.example:8792",
					RelaySpkiPin:       "pin",
				},
			},
			Origin: origin,
		},
	)
	if err != nil || got != "relay-proven" || discoverCalls != 1 {
		t.Fatalf(
			"legacy local Relay proof = %q calls=%d err=%v",
			got,
			discoverCalls,
			err,
		)
	}
}

func TestExpectedRemoteCloudIdentityRejectsDifferentLocalCloudOrigin(
	t *testing.T,
) {
	requested, err := cloudOriginFromCanonical("https://unit.ui.nabu.casa")
	if err != nil {
		t.Fatal(err)
	}
	local, err := cloudOriginFromCanonical("https://other.ui.nabu.casa")
	if err != nil {
		t.Fatal(err)
	}
	previousDiscover := discoverCloudFromLocalRelayForRemoteSetup
	discoverCloudFromLocalRelayForRemoteSetup = func(
		context.Context,
		runtimeConfig,
	) (cloudLocalDiscovery, error) {
		return cloudLocalDiscovery{
			Origin:          local,
			RelayInstanceID: "relay-proven",
		}, nil
	}
	t.Cleanup(func() {
		discoverCloudFromLocalRelayForRemoteSetup = previousDiscover
	})

	_, err = expectedRemoteCloudRelayIdentity(
		context.Background(),
		cloudRemoteSetupRequest{
			cloudSetupRequest: cloudSetupRequest{
				Config: runtimeConfig{
					RelaySecureBaseURL: "https://local.example:8792",
					RelaySpkiPin:       "pin",
				},
			},
			Origin: requested,
		},
	)
	if !IsCloudErrorCode(err, CloudErrIdentityMismatch) {
		t.Fatalf("different local Cloud origin error = %v", err)
	}
}
