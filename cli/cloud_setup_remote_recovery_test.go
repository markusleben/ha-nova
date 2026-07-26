package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRemoteCloudDeviceDefinitiveRejectionRepairsWithOwnerPairing(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	oldCredential := validCredential(11)
	replacement := validCredential(12)
	replacementID := parseDeviceCredential(replacement).deviceID
	if err := writeDeviceCredential(oldCredential); err != nil {
		t.Fatal(err)
	}

	requests := 0
	ingress := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			switch {
			case strings.HasSuffix(
				request.URL.Path,
				CloudPathDeviceBind,
			):
				if request.Header.Get("Authorization") !=
					"Bearer "+oldCredential {
					t.Fatalf(
						"bind authorization = %q",
						request.Header.Get("Authorization"),
					)
				}
				return cloudSetupRemoteTestResponse(
					http.StatusUnauthorized,
					`{"ok":false,"error":{"code":"UNAUTHORIZED","message":"generic"}}`,
				), nil
			case strings.HasSuffix(
				request.URL.Path,
				CloudPathDeviceActivate,
			):
				if request.Header.Get("Authorization") !=
					"Bearer "+replacement {
					t.Fatalf(
						"activate authorization = %q",
						request.Header.Get("Authorization"),
					)
				}
				current, exists, err := readDeviceCredential()
				if err != nil || !exists || current != oldCredential {
					t.Fatalf(
						"current changed before activation: %q %v %v",
						current,
						exists,
						err,
					)
				}
				pending, exists, err := readPendingDeviceCredential()
				if err != nil || !exists || pending != replacement {
					t.Fatalf(
						"pending before activation: %q %v %v",
						pending,
						exists,
						err,
					)
				}
				return cloudSetupRemoteTestResponse(
					http.StatusOK,
					fmt.Sprintf(
						`{"ok":true,"data":{"device_id":%q,"activated":true,"bound":true,"changed":true}}`,
						replacementID,
					),
				), nil
			default:
				t.Fatalf(
					"unexpected Cloud recovery request: %s %s",
					request.Method,
					request.URL.Redacted(),
				)
				return nil, errors.New("unexpected Cloud recovery request")
			}
		}),
	)
	pairCalls := 0
	previousPair := pairDeviceV2ForCloudSetup
	pairDeviceV2ForCloudSetup = func(
		_ context.Context,
		got *CloudIngressClient,
		code string,
		metadata deviceMetadata,
		relayInstanceID string,
	) (*cloudProvisionedCredential, error) {
		pairCalls++
		if got != ingress ||
			code != "123456" ||
			metadata.ClientInstallID != "inst-recovery" ||
			relayInstanceID != "relay-recovery" {
			t.Fatalf(
				"replacement pairing args: client=%p code=%q metadata=%+v relay=%q",
				got,
				code,
				metadata,
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
	credential, err := establishRemoteCloudDevice(
		context.Background(),
		cloudSetupRemoteTestSession(ingress),
		cloudSetupRemoteTestRequest(t, func(
			prompt cloudRemotePairingPrompt,
		) (string, error) {
			promptCalls++
			if prompt.AppURL !=
				"https://unit.ui.nabu.casa/app/"+
					cloudSetupRemoteTestAppSlug() {
				t.Fatalf("pairing app URL = %q", prompt.AppURL)
			}
			return "123456", nil
		}),
		"relay-recovery",
	)
	if err != nil {
		t.Fatalf("establishRemoteCloudDevice: %v", err)
	}
	if credential != replacement ||
		requests != 2 ||
		pairCalls != 1 ||
		promptCalls != 1 {
		t.Fatalf(
			"recovery result credential=%q requests=%d pair=%d prompt=%d",
			credential,
			requests,
			pairCalls,
			promptCalls,
		)
	}
	current, exists, err := readDeviceCredential()
	if err != nil || !exists || current != replacement {
		t.Fatalf(
			"replacement current: %q %v %v",
			current,
			exists,
			err,
		)
	}
	if _, exists, err := readPendingDeviceCredential(); err != nil || exists {
		t.Fatalf("pending remained after replacement: %v %v", exists, err)
	}
}

func TestRemoteCloudDeviceRejectedCurrentProbesBeforeOwnerCode(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	withDeviceStorageTestHome(t)
	oldCredential := validCredential(15)
	if err := writeDeviceCredential(oldCredential); err != nil {
		t.Fatal(err)
	}
	requests := 0
	ingress := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			if !strings.HasSuffix(request.URL.Path, CloudPathDeviceBind) {
				t.Fatalf("unexpected request path %q", request.URL.Path)
			}
			return cloudSetupRemoteTestResponse(
				http.StatusUnauthorized,
				`{"ok":false,"error":{"code":"UNAUTHORIZED","message":"generic"}}`,
			), nil
		}),
	)
	storageErr := errors.New("simulated unwritable credential store")
	preflightCalls := 0
	oldPreflight := deviceCredentialPreflightWithContext
	deviceCredentialPreflightWithContext = func(
		ctx context.Context,
		ui SecretStoreUIPolicy,
	) error {
		if err := validateDeviceCredentialPreflightRequest(ctx, ui); err != nil {
			return err
		}
		preflightCalls++
		if preflightCalls == 3 {
			return storageErr
		}
		return nil
	}
	t.Cleanup(func() {
		deviceCredentialPreflightWithContext = oldPreflight
	})
	promptCalls := 0
	_, err := establishRemoteCloudDevice(
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
	if !errors.Is(err, storageErr) {
		t.Fatalf("unwritable storage error = %v", err)
	}
	if requests != 1 || preflightCalls != 3 || promptCalls != 0 {
		t.Fatalf(
			"unwritable recovery requests=%d preflights=%d prompts=%d",
			requests,
			preflightCalls,
			promptCalls,
		)
	}
}

func TestRemoteCloudDeviceNetworkFailureNeverStartsReplacementPairing(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	oldCredential := validCredential(13)
	if err := writeDeviceCredential(oldCredential); err != nil {
		t.Fatal(err)
	}
	ingress := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network unavailable")
		}),
	)
	previousPair := pairDeviceV2ForCloudSetup
	pairDeviceV2ForCloudSetup = func(
		context.Context,
		*CloudIngressClient,
		string,
		deviceMetadata,
		string,
	) (*cloudProvisionedCredential, error) {
		t.Fatal("network failure reached replacement pairing")
		return nil, nil
	}
	t.Cleanup(func() {
		pairDeviceV2ForCloudSetup = previousPair
	})
	promptCalls := 0
	_, err := establishRemoteCloudDevice(
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
	if !IsCloudErrorCode(err, CloudErrNetwork) {
		t.Fatalf("network failure = %v", err)
	}
	if promptCalls != 0 {
		t.Fatalf("network failure opened replacement prompt %d times", promptCalls)
	}
	current, exists, readErr := readDeviceCredential()
	if readErr != nil || !exists || current != oldCredential {
		t.Fatalf(
			"network failure changed current: %q %v %v",
			current,
			exists,
			readErr,
		)
	}
	if _, exists, readErr := readPendingDeviceCredential(); readErr != nil || exists {
		t.Fatalf(
			"network failure created pending: %v %v",
			exists,
			readErr,
		)
	}
}

func cloudSetupRemoteTestResponse(
	status int,
	body string,
) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header: http.Header{
			relayVersionHeader: []string{"1.2.3"},
			"Content-Type":     []string{"application/json"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}

func cloudSetupRemoteTestSession(
	ingress *CloudIngressClient,
) cloudVerifiedSession {
	const ingressRoot = "/api/hassio_ingress/abcdefghijklmnopqrstuvwxyzABCDEFGH123456789"
	return cloudVerifiedSession{
		App: HAAddonInfo{
			Slug:         cloudSetupRemoteTestAppSlug(),
			State:        "started",
			Version:      "1.2.3",
			Ingress:      true,
			IngressEntry: ingressRoot,
			IngressURL:   ingressRoot + haNOVAIngressUIEntry,
		},
		Relay: CloudRelayInfo{
			RelayInstanceID: "relay-recovery",
		},
		Ingress: ingress,
	}
}

func cloudSetupRemoteTestAppSlug() string {
	appSlug, err := selectedCloudNOVAAppSlug()
	if err != nil {
		panic(err)
	}
	return appSlug
}

func cloudSetupRemoteTestRequest(
	t *testing.T,
	pairingCode cloudRemotePairingCodeProvider,
) cloudRemoteSetupRequest {
	t.Helper()
	origin, err := cloudOriginFromCanonical(
		"https://unit.ui.nabu.casa",
	)
	if err != nil {
		t.Fatal(err)
	}
	return cloudRemoteSetupRequest{
		cloudSetupRequest: cloudSetupRequest{
			Config: runtimeConfig{
				ClientInstallID: "inst-recovery",
			},
			AdvancePendingLifecycle: func(cloudLifecycleState) error {
				return nil
			},
			CheckpointDeviceActivation: func(string) error {
				return nil
			},
			ClearDeviceActivation: func() error {
				return nil
			},
			CheckpointDeviceBinding: func(string) error {
				return nil
			},
		},
		Origin:      origin,
		PairingCode: pairingCode,
	}
}
