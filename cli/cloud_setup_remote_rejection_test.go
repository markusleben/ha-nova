package main

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
)

func TestRejectedResumedCloudDeviceClearsMarkerBeforeCredential(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	credential := validCredential(91)
	if err := writePendingCloudDeviceCredential(
		credential,
		"relay-recovery",
	); err != nil {
		t.Fatal(err)
	}
	cfg := remoteActivationRejectionConfig(t, true, credential)
	var savedMarkers []bool
	save := func(value runtimeConfig) error {
		pending, exists, err := readPendingDeviceCredential()
		if err != nil || !exists || pending != credential {
			t.Fatalf(
				"marker cleared after credential: pending=%q exists=%v err=%v",
				pending,
				exists,
				err,
			)
		}
		savedMarkers = append(
			savedMarkers,
			value.Cloud.DeviceActivationStarted,
		)
		return nil
	}
	stopPairing := errors.New("stop after rejected resumed credential")
	promptCalls := 0
	request := cloudSetupRemoteTestRequest(
		t,
		func(cloudRemotePairingPrompt) (string, error) {
			promptCalls++
			return "", stopPairing
		},
	)
	request.cloudSetupRequest = newCloudSetupRequest(&cfg, save)
	ingress, activationCalls := rejectedActivationCloudIngress(t)

	_, err := establishRemoteCloudDevice(
		context.Background(),
		cloudSetupRemoteTestSession(ingress),
		request,
		"relay-recovery",
	)
	if !errors.Is(err, stopPairing) {
		t.Fatalf("resumed rejection error = %v", err)
	}
	if cfg.Cloud.DeviceActivationStarted ||
		!reflect.DeepEqual(savedMarkers, []bool{false}) {
		t.Fatalf(
			"resumed rejection marker cfg=%v saves=%v",
			cfg.Cloud.DeviceActivationStarted,
			savedMarkers,
		)
	}
	if _, exists, readErr := readPendingDeviceCredential(); readErr != nil ||
		exists {
		t.Fatalf(
			"resumed rejection retained pending: exists=%v err=%v",
			exists,
			readErr,
		)
	}
	if *activationCalls != 1 || promptCalls != 1 {
		t.Fatalf(
			"resumed rejection calls activation=%d prompt=%d",
			*activationCalls,
			promptCalls,
		)
	}
}

func TestRejectedFreshCloudDeviceClearsMarkerBeforeCredential(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	credential := validCredential(92)
	deviceID := parseDeviceCredential(credential).deviceID
	cfg := remoteActivationRejectionConfig(t, false, credential)
	var savedMarkers []bool
	save := func(value runtimeConfig) error {
		pending, exists, err := readPendingDeviceCredential()
		if err != nil || !exists || pending != credential {
			t.Fatalf(
				"fresh marker save without pending: pending=%q exists=%v err=%v",
				pending,
				exists,
				err,
			)
		}
		savedMarkers = append(
			savedMarkers,
			value.Cloud.DeviceActivationStarted,
		)
		return nil
	}
	request := cloudSetupRemoteTestRequest(
		t,
		func(cloudRemotePairingPrompt) (string, error) {
			return "123456", nil
		},
	)
	request.cloudSetupRequest = newCloudSetupRequest(&cfg, save)
	ingress, activationCalls := rejectedActivationCloudIngress(t)
	previousPair := pairDeviceV2ForCloudSetup
	pairDeviceV2ForCloudSetup = func(
		context.Context,
		*CloudIngressClient,
		string,
		deviceMetadata,
		string,
	) (*cloudProvisionedCredential, error) {
		return &cloudProvisionedCredential{
			Credential:      credential,
			DeviceID:        deviceID,
			RelayInstanceID: "relay-recovery",
		}, nil
	}
	t.Cleanup(func() {
		pairDeviceV2ForCloudSetup = previousPair
	})

	_, err := establishRemoteCloudDevice(
		context.Background(),
		cloudSetupRemoteTestSession(ingress),
		request,
		"relay-recovery",
	)
	if !IsCloudErrorCode(err, CloudErrDeviceRejected) {
		t.Fatalf("fresh rejection error = %v", err)
	}
	if cfg.Cloud.DeviceActivationStarted ||
		!reflect.DeepEqual(savedMarkers, []bool{true, false}) {
		t.Fatalf(
			"fresh rejection marker cfg=%v saves=%v",
			cfg.Cloud.DeviceActivationStarted,
			savedMarkers,
		)
	}
	if _, exists, readErr := readPendingDeviceCredential(); readErr != nil ||
		exists {
		t.Fatalf(
			"fresh rejection retained pending: exists=%v err=%v",
			exists,
			readErr,
		)
	}
	if *activationCalls != 1 {
		t.Fatalf("fresh rejection activation calls = %d", *activationCalls)
	}
}

func TestRejectedCloudDeviceClearSaveFailurePreservesRecoveryState(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	credential := validCredential(93)
	if err := writePendingCloudDeviceCredential(
		credential,
		"relay-recovery",
	); err != nil {
		t.Fatal(err)
	}
	cfg := remoteActivationRejectionConfig(t, true, credential)
	saveErr := errors.New("simulated activation marker clear failure")
	saveCalls := 0
	save := func(value runtimeConfig) error {
		saveCalls++
		if value.Cloud.DeviceActivationStarted {
			t.Fatal("marker clear save retained the activation marker")
		}
		pending, exists, err := readPendingDeviceCredential()
		if err != nil || !exists || pending != credential {
			t.Fatalf(
				"failed clear deleted credential: pending=%q exists=%v err=%v",
				pending,
				exists,
				err,
			)
		}
		return saveErr
	}
	promptCalls := 0
	request := cloudSetupRemoteTestRequest(
		t,
		func(cloudRemotePairingPrompt) (string, error) {
			promptCalls++
			return "123456", nil
		},
	)
	request.cloudSetupRequest = newCloudSetupRequest(&cfg, save)
	ingress, activationCalls := rejectedActivationCloudIngress(t)

	_, err := establishRemoteCloudDevice(
		context.Background(),
		cloudSetupRemoteTestSession(ingress),
		request,
		"relay-recovery",
	)
	if !errors.Is(err, saveErr) {
		t.Fatalf("marker clear failure = %v", err)
	}
	if !cfg.Cloud.DeviceActivationStarted || saveCalls != 1 {
		t.Fatalf(
			"failed clear state marker=%v saves=%d",
			cfg.Cloud.DeviceActivationStarted,
			saveCalls,
		)
	}
	pending, exists, readErr := readPendingDeviceCredential()
	if readErr != nil || !exists || pending != credential {
		t.Fatalf(
			"failed clear recovery pending=%q exists=%v err=%v",
			pending,
			exists,
			readErr,
		)
	}
	if *activationCalls != 1 || promptCalls != 0 {
		t.Fatalf(
			"failed clear calls activation=%d prompt=%d",
			*activationCalls,
			promptCalls,
		)
	}
}

func remoteActivationRejectionConfig(
	t *testing.T,
	activationStarted bool,
	credential string,
) runtimeConfig {
	t.Helper()
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	envelope := productionCloudTestEnvelope()
	envelope.RelayInstanceID = "relay-recovery"
	pending := cloudMetadataFromEnvelope(origin, envelope)
	cfg := runtimeConfig{
		ProfileID:       envelope.ProfileID,
		ClientInstallID: "inst-recovery",
		Cloud: &cloudLifecycleMetadata{
			State:                   cloudStateCloudVerified,
			Pending:                 &pending,
			DeviceActivationStarted: activationStarted,
		},
	}
	if activationStarted {
		cfg.Cloud.DeviceActivationDeviceID = deviceIDOf(credential)
	}
	return cfg
}

func rejectedActivationCloudIngress(
	t *testing.T,
) (*CloudIngressClient, *int) {
	t.Helper()
	calls := 0
	ingress := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return cloudSetupRemoteTestResponse(
				http.StatusUnauthorized,
				`{"ok":false,"error":{"code":"UNAUTHORIZED","message":"generic"}}`,
			), nil
		}),
	)
	return ingress, &calls
}
