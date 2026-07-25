package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestDeviceActivationCheckpointIsIdempotentOnlyForExactDevice(
	t *testing.T,
) {
	credential := validCredential(111)
	deviceID := deviceIDOf(credential)
	cfg := runtimeConfig{
		Cloud: &cloudLifecycleMetadata{
			State:                    cloudStateDeviceBoundOrPaired,
			Pending:                  remoteDeviceCheckpointMetadata(t),
			DeviceActivationStarted:  true,
			DeviceActivationDeviceID: deviceID,
		},
	}
	saves := 0
	request := newCloudSetupRequest(
		&cfg,
		func(runtimeConfig) error {
			saves++
			return nil
		},
	)
	if err := request.CheckpointDeviceActivation(deviceID); err != nil {
		t.Fatalf("same device checkpoint: %v", err)
	}
	if saves != 0 {
		t.Fatalf("idempotent checkpoint saves = %d", saves)
	}
	if err := request.CheckpointDeviceActivation(
		deviceIDOf(validCredential(112)),
	); !IsCloudErrorCode(err, CloudErrIdentityMismatch) {
		t.Fatalf("mismatched checkpoint = %v", err)
	}
	if cfg.Cloud.DeviceActivationDeviceID != deviceID || saves != 0 {
		t.Fatalf("mismatch changed checkpoint: %+v saves=%d", cfg.Cloud, saves)
	}
}

func TestDeviceBoundRecoveryUsesOnlyExactPromotedCurrent(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	credential := validCredential(113)
	deviceID := deviceIDOf(credential)
	if err := writeDeviceCredential(credential); err != nil {
		t.Fatal(err)
	}
	cfg := remotePromotedDeviceRecoveryConfig(t, deviceID)
	request := cloudSetupRemoteTestRequest(
		t,
		func(cloudRemotePairingPrompt) (string, error) {
			t.Fatal("promoted recovery requested another owner code")
			return "", nil
		},
	)
	request.cloudSetupRequest = newCloudSetupRequest(
		&cfg,
		func(runtimeConfig) error { return nil },
	)
	calls := 0
	ingress := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			if !strings.HasSuffix(request.URL.Path, CloudPathDeviceBind) {
				t.Fatalf("unexpected request path %q", request.URL.Path)
			}
			return cloudSetupRemoteTestResponse(
				http.StatusOK,
				fmt.Sprintf(
					`{"ok":true,"data":{"device_id":%q,"bound":true,"changed":false}}`,
					deviceID,
				),
			), nil
		}),
	)
	got, err := establishRemoteCloudDevice(
		context.Background(),
		cloudSetupRemoteTestSession(ingress),
		request,
		"relay-recovery",
	)
	if err != nil || got != credential || calls != 1 {
		t.Fatalf(
			"promoted recovery credential=%q calls=%d err=%v",
			got,
			calls,
			err,
		)
	}
}

func TestDeviceBoundRecoveryRejectsMismatchedOrMissingCurrent(
	t *testing.T,
) {
	for _, test := range []struct {
		name       string
		credential string
	}{
		{name: "mismatched", credential: validCredential(114)},
		{name: "missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
			expectedID := deviceIDOf(validCredential(115))
			if test.credential != "" {
				if err := writeDeviceCredential(test.credential); err != nil {
					t.Fatal(err)
				}
			}
			cfg := remotePromotedDeviceRecoveryConfig(t, expectedID)
			request := cloudSetupRemoteTestRequest(
				t,
				func(cloudRemotePairingPrompt) (string, error) {
					t.Fatal("unsafe recovery requested another owner code")
					return "", nil
				},
			)
			request.cloudSetupRequest = newCloudSetupRequest(
				&cfg,
				func(runtimeConfig) error { return nil },
			)
			ingress := newProtocolTestCloudIngressClient(
				t,
				roundTripFunc(func(*http.Request) (*http.Response, error) {
					t.Fatal("unsafe recovery disclosed a credential")
					return nil, nil
				}),
			)
			_, err := establishRemoteCloudDevice(
				context.Background(),
				cloudSetupRemoteTestSession(ingress),
				request,
				"relay-recovery",
			)
			if test.credential == "" {
				if !IsCloudErrorCode(err, CloudErrSecretNotFound) {
					t.Fatalf("missing recovery = %v", err)
				}
			} else if !IsCloudErrorCode(err, CloudErrIdentityMismatch) {
				t.Fatalf("mismatched recovery = %v", err)
			}
		})
	}
}

func remotePromotedDeviceRecoveryConfig(
	t *testing.T,
	deviceID string,
) runtimeConfig {
	t.Helper()
	return runtimeConfig{
		ProfileID:       "profile-1",
		ClientInstallID: "inst-recovery",
		RelayInstanceID: "relay-recovery",
		Cloud: &cloudLifecycleMetadata{
			State:                    cloudStateDeviceBoundOrPaired,
			Pending:                  remoteDeviceCheckpointMetadata(t),
			DeviceActivationStarted:  true,
			DeviceActivationDeviceID: deviceID,
		},
	}
}

func remoteDeviceCheckpointMetadata(
	t *testing.T,
) *cloudConnectionMetadata {
	t.Helper()
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	envelope := productionCloudTestEnvelope()
	envelope.RelayInstanceID = "relay-recovery"
	metadata := cloudMetadataFromEnvelope(origin, envelope)
	return &metadata
}
