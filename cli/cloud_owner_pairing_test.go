package main

import (
	"context"
	"errors"
	"testing"
)

func TestOwnerPairingRepromptsMalformedCodeBeforeRelayCall(t *testing.T) {
	probeCalls := installOwnerPairingStorageHooks(t)
	pairCalls := 0
	installOwnerPairingRelayHook(t, func(code string) (
		*cloudProvisionedCredential,
		error,
	) {
		pairCalls++
		if code != "123456" {
			t.Fatalf("paired code = %q", code)
		}
		return ownerPairingTestCredential(), nil
	})
	prompts := 0
	request := cloudSetupRemoteTestRequest(t, func(
		prompt cloudRemotePairingPrompt,
	) (string, error) {
		prompts++
		switch prompts {
		case 1:
			if prompt.RetryReason != "" {
				t.Fatalf("initial retry reason = %q", prompt.RetryReason)
			}
			return "12", nil
		case 2:
			if prompt.RetryReason != invalidOwnerPairingCodeMessage {
				t.Fatalf("malformed retry reason = %q", prompt.RetryReason)
			}
			return "123 456", nil
		default:
			t.Fatalf("unexpected prompt %d", prompts)
			return "", nil
		}
	})

	got, err := provisionRemoteCloudDeviceWithOwnerCode(
		context.Background(),
		cloudSetupRemoteTestSession(nil),
		request,
		"relay-recovery",
		"https://unit.ui.nabu.casa/app",
	)
	if err != nil || got.DeviceID != ownerPairingTestCredential().DeviceID {
		t.Fatalf("provisioned=%+v err=%v", got, err)
	}
	if prompts != 2 || pairCalls != 1 || *probeCalls != 1 {
		t.Fatalf(
			"prompts=%d pairs=%d storage-probes=%d",
			prompts,
			pairCalls,
			*probeCalls,
		)
	}
}

func TestOwnerPairingRepromptsDefinitiveRelayRejection(t *testing.T) {
	probeCalls := installOwnerPairingStorageHooks(t)
	pairCalls := 0
	installOwnerPairingRelayHook(t, func(string) (
		*cloudProvisionedCredential,
		error,
	) {
		pairCalls++
		if pairCalls == 1 {
			return nil, newCloudError(
				CloudErrPairingInactive,
				"pair test device",
				nil,
			)
		}
		return ownerPairingTestCredential(), nil
	})
	prompts := 0
	request := cloudSetupRemoteTestRequest(t, func(
		prompt cloudRemotePairingPrompt,
	) (string, error) {
		prompts++
		if prompts == 2 &&
			prompt.RetryReason != rejectedOwnerPairingCodeMessage {
			t.Fatalf("rejected retry reason = %q", prompt.RetryReason)
		}
		return "123456", nil
	})

	_, err := provisionRemoteCloudDeviceWithOwnerCode(
		context.Background(),
		cloudSetupRemoteTestSession(nil),
		request,
		"relay-recovery",
		"https://unit.ui.nabu.casa/app",
	)
	if err != nil {
		t.Fatal(err)
	}
	if prompts != 2 || pairCalls != 2 || *probeCalls != 2 {
		t.Fatalf(
			"prompts=%d pairs=%d storage-probes=%d",
			prompts,
			pairCalls,
			*probeCalls,
		)
	}
}

func TestOwnerPairingDoesNotRepromptAmbiguousRelayFailure(t *testing.T) {
	probeCalls := installOwnerPairingStorageHooks(t)
	networkErr := newCloudError(CloudErrNetwork, "pair test device", nil)
	installOwnerPairingRelayHook(t, func(string) (
		*cloudProvisionedCredential,
		error,
	) {
		return nil, networkErr
	})
	prompts := 0
	request := cloudSetupRemoteTestRequest(t, func(
		cloudRemotePairingPrompt,
	) (string, error) {
		prompts++
		return "123456", nil
	})

	_, err := provisionRemoteCloudDeviceWithOwnerCode(
		context.Background(),
		cloudSetupRemoteTestSession(nil),
		request,
		"relay-recovery",
		"https://unit.ui.nabu.casa/app",
	)
	if !errors.Is(err, networkErr) {
		t.Fatalf("network error = %v", err)
	}
	if prompts != 1 || *probeCalls != 1 {
		t.Fatalf("prompts=%d storage-probes=%d", prompts, *probeCalls)
	}
}

func installOwnerPairingStorageHooks(t *testing.T) *int {
	t.Helper()
	oldProbe := probeCloudDeviceStorageForSetup
	oldPending := readCloudPendingDeviceForSetup
	oldCurrent := readCloudDeviceForSetup
	t.Cleanup(func() {
		probeCloudDeviceStorageForSetup = oldProbe
		readCloudPendingDeviceForSetup = oldPending
		readCloudDeviceForSetup = oldCurrent
	})
	probeCalls := 0
	probeCloudDeviceStorageForSetup = func(
		context.Context,
		SecretStoreUIPolicy,
	) (deviceStorageProbe, error) {
		probeCalls++
		return deviceStorageProbe{mode: "test"}, nil
	}
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
	return &probeCalls
}

func installOwnerPairingRelayHook(
	t *testing.T,
	result func(string) (*cloudProvisionedCredential, error),
) {
	t.Helper()
	oldPair := pairDeviceV2ForCloudSetup
	t.Cleanup(func() {
		pairDeviceV2ForCloudSetup = oldPair
	})
	pairDeviceV2ForCloudSetup = func(
		_ context.Context,
		_ *CloudIngressClient,
		code string,
		_ deviceMetadata,
		_ string,
	) (*cloudProvisionedCredential, error) {
		return result(code)
	}
}

func ownerPairingTestCredential() *cloudProvisionedCredential {
	credential := validCredential(188)
	return &cloudProvisionedCredential{
		Credential:      credential,
		DeviceID:        deviceIDOf(credential),
		RelayInstanceID: "relay-recovery",
	}
}
