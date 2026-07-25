package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestRemoteCloudPendingForeignDeviceIDIsIdentityMismatch(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	credential := validCredential(5)
	if err := writePendingCloudDeviceCredential(
		credential,
		"relay-recovery",
	); err != nil {
		t.Fatal(err)
	}
	ingress := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if !strings.HasSuffix(request.URL.Path, CloudPathDeviceActivate) {
				t.Fatalf("unexpected request path %q", request.URL.Path)
			}
			return cloudSetupRemoteTestResponse(
				http.StatusOK,
				`{"ok":true,"data":{"device_id":"foreign-device","activated":true,"bound":true,"changed":false}}`,
			), nil
		}),
	)
	_, err := establishRemoteCloudDevice(
		context.Background(),
		cloudSetupRemoteTestSession(ingress),
		cloudSetupRemoteTestRequest(t, func(
			cloudRemotePairingPrompt,
		) (string, error) {
			t.Fatal("identity mismatch opened a second pairing")
			return "", nil
		}),
		"relay-recovery",
	)
	if !IsCloudErrorCode(err, CloudErrIdentityMismatch) {
		t.Fatalf("foreign pending device id error = %v", err)
	}
	record, exists, readErr := readPendingDeviceCredentialRecord()
	if readErr != nil || !exists || record.Credential != credential {
		t.Fatalf(
			"identity mismatch changed pending: %+v exists=%v err=%v",
			record,
			exists,
			readErr,
		)
	}
}

func TestRemoteCloudCurrentForeignDeviceIDIsIdentityMismatch(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	credential := validCredential(6)
	if err := writeDeviceCredential(credential); err != nil {
		t.Fatal(err)
	}
	ingress := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if !strings.HasSuffix(request.URL.Path, CloudPathDeviceBind) {
				t.Fatalf("unexpected request path %q", request.URL.Path)
			}
			return cloudSetupRemoteTestResponse(
				http.StatusOK,
				fmt.Sprintf(
					`{"ok":true,"data":{"device_id":%q,"bound":true,"changed":false}}`,
					"foreign-device",
				),
			), nil
		}),
	)
	_, err := establishRemoteCloudDevice(
		context.Background(),
		cloudSetupRemoteTestSession(ingress),
		cloudSetupRemoteTestRequest(t, func(
			cloudRemotePairingPrompt,
		) (string, error) {
			t.Fatal("identity mismatch opened replacement pairing")
			return "", nil
		}),
		"relay-recovery",
	)
	if !IsCloudErrorCode(err, CloudErrIdentityMismatch) {
		t.Fatalf("foreign current device id error = %v", err)
	}
	current, exists, readErr := readDeviceCredential()
	if readErr != nil || !exists || current != credential {
		t.Fatalf(
			"identity mismatch changed current=%q exists=%v err=%v",
			current,
			exists,
			readErr,
		)
	}
}

func TestRemoteCloudUnknownRelayNeverDisclosesCurrentDeviceSecret(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	currentCredential := validCredential(9)
	replacement := validCredential(10)
	replacementID := parseDeviceCredential(replacement).deviceID
	if err := writeDeviceCredential(currentCredential); err != nil {
		t.Fatal(err)
	}
	bindCalls := 0
	activationCalls := 0
	ingress := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if strings.HasSuffix(request.URL.Path, CloudPathDeviceBind) {
				bindCalls++
				t.Fatalf("unknown Relay received the current device credential")
			}
			if !strings.HasSuffix(request.URL.Path, CloudPathDeviceActivate) {
				t.Fatalf("unexpected request path %q", request.URL.Path)
			}
			activationCalls++
			if request.Header.Get("Authorization") != "Bearer "+replacement {
				t.Fatalf(
					"replacement activation authorization = %q",
					request.Header.Get("Authorization"),
				)
			}
			return cloudSetupRemoteTestResponse(
				http.StatusOK,
				fmt.Sprintf(
					`{"ok":true,"data":{"device_id":%q,"activated":true,"bound":true,"changed":true}}`,
					replacementID,
				),
			), nil
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
		if got != ingress || code != "123456" ||
			relayInstanceID != "relay-recovery" {
			t.Fatalf(
				"replacement pairing client=%p code=%q relay=%q",
				got,
				code,
				relayInstanceID,
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
		"",
	)
	if err != nil {
		t.Fatalf("unknown-Relay replacement pairing: %v", err)
	}
	if got != replacement || bindCalls != 0 || activationCalls != 1 ||
		pairCalls != 1 || promptCalls != 1 {
		t.Fatalf(
			"unknown-Relay result=%q bind=%d activate=%d pair=%d prompt=%d",
			got,
			bindCalls,
			activationCalls,
			pairCalls,
			promptCalls,
		)
	}
	saved, exists, readErr := readDeviceCredential()
	if readErr != nil || !exists || saved != replacement {
		t.Fatalf(
			"replacement current=%q exists=%v err=%v",
			saved,
			exists,
			readErr,
		)
	}
}

func TestRemoteCloudDevicePreflightRejectsLocalPendingBeforeAuthorization(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	credential := validCredential(7)
	if err := writePendingDeviceCredential(credential); err != nil {
		t.Fatal(err)
	}
	err := preflightRemoteCloudDeviceState("relay-1")
	if !IsCloudErrorCode(err, CloudErrDevicePendingConflict) {
		t.Fatalf("local pending remote preflight error = %v", err)
	}
	got, exists, readErr := readPendingDeviceCredential()
	if readErr != nil || !exists || got != credential {
		t.Fatalf(
			"remote preflight changed local pending=%q exists=%v err=%v",
			got,
			exists,
			readErr,
		)
	}
}

func TestRemoteCloudDevicePreflightRejectsKnownRelayMismatch(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	if err := writePendingCloudDeviceCredential(
		validCredential(8),
		"relay-other",
	); err != nil {
		t.Fatal(err)
	}
	err := preflightRemoteCloudDeviceState("relay-expected")
	if !IsCloudErrorCode(err, CloudErrRelayInstance) {
		t.Fatalf("known pending Relay mismatch error = %v", err)
	}
}
