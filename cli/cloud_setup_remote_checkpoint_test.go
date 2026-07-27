package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestRemoteDeviceCheckpointPrecedesPendingPromotion(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	credential := validCredential(30)
	deviceID := parseDeviceCredential(credential).deviceID
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
				fmt.Sprintf(
					`{"ok":true,"data":{"device_id":%q,"activated":true,"bound":true,"changed":false}}`,
					deviceID,
				),
			), nil
		}),
	)
	request := cloudSetupRemoteTestRequest(
		t,
		func(cloudRemotePairingPrompt) (string, error) {
			t.Fatal("resumable pending requested another owner code")
			return "", nil
		},
	)
	checkpointErr := errors.New("simulated config checkpoint failure")
	request.CheckpointDeviceBinding = func(
		relayInstanceID string,
	) error {
		if relayInstanceID != "relay-recovery" {
			t.Fatalf("checkpoint Relay = %q", relayInstanceID)
		}
		return checkpointErr
	}

	_, err := establishRemoteCloudDevice(
		context.Background(),
		cloudSetupRemoteTestSession(ingress),
		request,
		"relay-recovery",
	)
	if !errors.Is(err, checkpointErr) {
		t.Fatalf("checkpoint failure = %v", err)
	}
	record, exists, readErr := readPendingDeviceCredentialRecord()
	if readErr != nil || !exists ||
		record.Credential != credential ||
		record.Source != pendingDeviceCredentialSourceCloud {
		t.Fatalf(
			"failed checkpoint lost pending=%+v exists=%v err=%v",
			record,
			exists,
			readErr,
		)
	}
	if _, exists, readErr := readDeviceCredential(); readErr != nil || exists {
		t.Fatalf(
			"failed checkpoint promoted current: exists=%v err=%v",
			exists,
			readErr,
		)
	}

	request.CheckpointDeviceBinding = func(string) error {
		return nil
	}
	got, err := establishRemoteCloudDevice(
		context.Background(),
		cloudSetupRemoteTestSession(ingress),
		request,
		"relay-recovery",
	)
	if err != nil || got != credential {
		t.Fatalf("checkpoint resume credential=%q err=%v", got, err)
	}
	if _, exists, readErr := readPendingDeviceCredentialRecord(); readErr != nil ||
		exists {
		t.Fatalf(
			"checkpoint resume left pending: exists=%v err=%v",
			exists,
			readErr,
		)
	}
	current, exists, readErr := readDeviceCredential()
	if readErr != nil || !exists || current != credential {
		t.Fatalf(
			"checkpoint resume current=%q exists=%v err=%v",
			current,
			exists,
			readErr,
		)
	}
}

func TestRemoteDeviceCheckpointDurablyBindsRelayBeforePromotionFailure(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	secretDir := t.TempDir()
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", secretDir)
	credential := validCredential(31)
	deviceID := parseDeviceCredential(credential).deviceID
	if err := writePendingCloudDeviceCredential(
		credential,
		"relay-recovery",
	); err != nil {
		t.Fatal(err)
	}
	currentPath := testSecretPath(secretDir, deviceCredentialService)
	if err := os.Mkdir(currentPath, 0o700); err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	envelope := productionCloudTestEnvelope()
	envelope.RelayInstanceID = "relay-recovery"
	pendingMetadata := cloudMetadataFromEnvelope(origin, envelope)
	cfg := runtimeConfig{
		ProfileID:       "profile-1",
		ClientInstallID: "inst-recovery",
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateCloudVerified,
			Pending: &pendingMetadata,
		},
	}
	var saved []runtimeConfig
	save := func(value runtimeConfig) error {
		saved = append(saved, value)
		return nil
	}
	request := cloudSetupRemoteTestRequest(
		t,
		func(cloudRemotePairingPrompt) (string, error) {
			t.Fatal("resumable pending requested another owner code")
			return "", nil
		},
	)
	request.cloudSetupRequest = newCloudSetupRequest(&cfg, save)
	ingress := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return cloudSetupRemoteTestResponse(
				http.StatusOK,
				fmt.Sprintf(
					`{"ok":true,"data":{"device_id":%q,"activated":true,"bound":true,"changed":false}}`,
					deviceID,
				),
			), nil
		}),
	)

	_, err = establishRemoteCloudDevice(
		context.Background(),
		cloudSetupRemoteTestSession(ingress),
		request,
		"relay-recovery",
	)
	if err == nil {
		t.Fatal("blocked current slot did not interrupt promotion")
	}
	if cfg.RelayInstanceID != "relay-recovery" ||
		cfg.Cloud.State != cloudStateDeviceBoundOrPaired ||
		cfg.Cloud.DeviceActivationDeviceID != deviceID ||
		len(saved) == 0 ||
		saved[len(saved)-1].RelayInstanceID != "relay-recovery" {
		t.Fatalf(
			"durable checkpoint missing: cfg=%+v saves=%+v",
			cfg,
			saved,
		)
	}
	if _, exists, readErr := readPendingDeviceCredentialRecord(); readErr != nil ||
		!exists {
		t.Fatalf(
			"promotion failure lost pending: exists=%v err=%v",
			exists,
			readErr,
		)
	}
	if err := os.Rename(currentPath, currentPath+".blocked"); err != nil {
		t.Fatal(err)
	}
	retry := cloudSetupRemoteTestRequest(
		t,
		func(cloudRemotePairingPrompt) (string, error) {
			t.Fatal("device-bound retry requested another owner code")
			return "", nil
		},
	)
	retry.cloudSetupRequest = newCloudSetupRequest(&cfg, save)
	got, err := establishRemoteCloudDevice(
		context.Background(),
		cloudSetupRemoteTestSession(ingress),
		retry,
		"relay-recovery",
	)
	if err != nil || got != credential {
		t.Fatalf("device-bound retry credential=%q err=%v", got, err)
	}
	current, exists, readErr := readDeviceCredential()
	if readErr != nil || !exists || current != credential {
		t.Fatalf(
			"device-bound retry current=%q exists=%v err=%v",
			current,
			exists,
			readErr,
		)
	}
}

func TestCloudRemoveRevokesActivatedPendingAfterCheckpointWriteFailure(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
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
	origin, err := cloudOriginFromCanonical(pending.CanonicalOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, pending)
	credential := validCredential(76)
	cfg := runtimeConfig{
		ProfileID:       pending.ProfileID,
		ClientInstallID: "inst-checkpoint-failed",
		RoutePolicy:     routePolicyLocal,
		Cloud: &cloudLifecycleMetadata{
			State:                    cloudStateCloudVerified,
			Pending:                  &metadata,
			DeviceActivationStarted:  true,
			DeviceActivationDeviceID: deviceIDOf(credential),
		},
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	if err := writePendingCloudDeviceCredential(
		credential,
		pending.RelayInstanceID,
	); err != nil {
		t.Fatal(err)
	}

	var revokedDevice string
	installRemoteDeviceRevokeHook(
		t,
		func(
			_ context.Context,
			gotConfig runtimeConfig,
			gotStore OAuthSecretStore,
			gotCredential string,
		) error {
			if gotConfig.RelayInstanceID != pending.RelayInstanceID ||
				gotStore != store {
				t.Fatalf(
					"checkpoint recovery provenance cfg=%+v store=%T",
					gotConfig,
					gotStore,
				)
			}
			revokedDevice = gotCredential
			return nil
		},
	)
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error { return nil },
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 0 || revokedDevice != credential {
		t.Fatalf(
			"checkpoint recovery remove exit=%d revoked=%q output=%s",
			exit,
			revokedDevice,
			output,
		)
	}
	if _, exists, err := readPendingDeviceCredentialRecord(); err != nil ||
		exists {
		t.Fatalf(
			"checkpoint recovery pending remained: exists=%v err=%v",
			exists,
			err,
		)
	}
}
