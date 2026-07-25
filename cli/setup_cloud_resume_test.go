package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecoverRemoteOnlyCloudSetupPreservesTransaction(t *testing.T) {
	metadata := cloudMetadataForTest(strings.Repeat("a", 32))
	raw := runtimeConfig{
		ProfileID:       "profile-resume",
		ClientInstallID: "install-resume",
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateTokenStored,
			Pending: &metadata,
		},
	}

	recovered, ok := recoverRemoteOnlyCloudSetupConfig(raw)
	if !ok {
		t.Fatal("valid remote-only Cloud transaction was not recovered")
	}
	if recovered.ProfileID != raw.ProfileID ||
		recovered.ClientInstallID != raw.ClientInstallID ||
		recovered.Cloud == nil ||
		recovered.Cloud.Pending == nil ||
		recovered.Cloud.Pending.CredentialGeneration !=
			metadata.CredentialGeneration {
		t.Fatalf("recovered config lost transaction identity: %+v", recovered)
	}
}

func TestRecoverRemoteOnlyCloudSetupRejectsCorruptTransaction(t *testing.T) {
	metadata := cloudMetadataForTest("bad-generation")
	raw := runtimeConfig{
		ProfileID: "profile-resume",
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateTokenStored,
			Pending: &metadata,
		},
	}
	if _, ok := recoverRemoteOnlyCloudSetupConfig(raw); ok {
		t.Fatal("corrupt remote-only Cloud transaction was recovered")
	}
}

func TestResumeReadyCloudOnlySetupSkipsFreshAuthorization(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{"schema_version":3}`)
	paths.StateFile = filepath.Join(paths.ConfigDir, "state.json")
	metadata := cloudMetadataForTest(strings.Repeat("b", 32))
	cfg := runtimeConfig{
		ProfileID:       "profile-ready",
		RelayInstanceID: "relay-ready",
		RoutePolicy:     routePolicyCloud,
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateReady,
			Current: &metadata,
		},
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	coordinator := newSelectingCloudCoordinator()
	installCloudCommandCoordinator(t, coordinator)
	oldVerify := verifyCloudDeviceHealthForSetup
	verifyCloudDeviceHealthForSetup = func(
		context.Context,
		runtimeConfig,
	) error {
		return nil
	}
	t.Cleanup(func() {
		verifyCloudDeviceHealthForSetup = oldVerify
	})

	var output strings.Builder
	state := installState{}
	code := resumeInteractiveCloudOnlySetup(
		bufio.NewReader(strings.NewReader("")),
		&output,
		paths,
		cfg,
		&state,
		"all",
		nil,
		nil,
	)
	if code != 0 {
		t.Fatalf("resume exit = %d, output:\n%s", code, output.String())
	}
	if coordinator.preflightCalls != 1 ||
		coordinator.localCalls != 0 ||
		coordinator.remoteCalls != 0 {
		t.Fatalf(
			"coordinator calls preflight/local/remote = %d/%d/%d",
			coordinator.preflightCalls,
			coordinator.localCalls,
			coordinator.remoteCalls,
		)
	}
}

func TestResumeCloudOnlySetupPersistsSecurityHealthFailure(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{"schema_version":3}`)
	paths.StateFile = filepath.Join(paths.ConfigDir, "state.json")
	metadata := cloudMetadataForTest(strings.Repeat("c", 32))
	cfg := runtimeConfig{
		ProfileID:       "profile-resume-security",
		RelayInstanceID: "relay-resume-security",
		RoutePolicy:     routePolicyCloud,
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateReady,
			Current: &metadata,
		},
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	coordinator := newSelectingCloudCoordinator()
	installCloudCommandCoordinator(t, coordinator)
	installSetupCloudHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			return newCloudError(
				CloudErrIdentityMismatch,
				"verify resumed Cloud health",
				nil,
			)
		},
	)

	var output strings.Builder
	exit := resumeInteractiveCloudOnlySetup(
		bufio.NewReader(strings.NewReader("")),
		&output,
		paths,
		cfg,
		&installState{},
		"all",
		nil,
		nil,
	)
	if exit != 1 {
		t.Fatalf("resume exit=%d output=%s", exit, output.String())
	}
	saved, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Cloud == nil ||
		saved.Cloud.RecoveryHold == nil ||
		saved.Cloud.RecoveryHold.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("resume security failure was not held: %+v", saved.Cloud)
	}
}

func TestCloudOriginForSetupResumeRepromptsAfterInvalidURL(t *testing.T) {
	cfg := runtimeConfig{
		Cloud: &cloudLifecycleMetadata{
			State: cloudStateAuthorizing,
		},
	}
	var output strings.Builder
	origin, err := cloudOriginForSetupResume(
		context.Background(),
		bufio.NewReader(strings.NewReader(
			"http://unsafe.example\n"+productionCloudTestOrigin+"\n",
		)),
		&output,
		cfg,
	)
	if err != nil {
		t.Fatalf("cloudOriginForSetupResume: %v", err)
	}
	if origin.CanonicalOrigin != productionCloudTestOrigin {
		t.Fatalf("origin = %q", origin.CanonicalOrigin)
	}
	if !strings.Contains(
		output.String(),
		"complete HTTPS remote URL",
	) {
		t.Fatalf("missing retry guidance:\n%s", output.String())
	}
}

func TestCloudOnlyResumeURLCancelReportsSavedCheckpoint(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := runtimeConfig{
		ProfileID: "profile-resume-cancel",
		Cloud: &cloudLifecycleMetadata{
			State: cloudStateAuthorizing,
		},
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	installCloudCommandCoordinator(t, newSelectingCloudCoordinator())

	var output strings.Builder
	exit := resumeInteractiveCloudOnlySetup(
		bufio.NewReader(strings.NewReader("exit\n")),
		&output,
		paths,
		cfg,
		&installState{},
		"",
		nil,
		nil,
	)
	if exit != 0 ||
		!strings.Contains(
			output.String(),
			`saved checkpoint "authorizing"`,
		) ||
		!strings.Contains(
			output.String(),
			"ha-nova cloud add --server default",
		) ||
		strings.Contains(output.String(), "Setup cancelled.") {
		t.Fatalf(
			"resume cancellation exit=%d output=%s",
			exit,
			output.String(),
		)
	}
}

func TestDeviceSetupStateUsesCloudRouteWithoutLocalAddress(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{"schema_version":3}`)
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/health" {
				http.NotFound(writer, request)
				return
			}
			if request.Header.Get("Authorization") !=
				"Bearer cloud-device" {
				http.Error(writer, "unauthorized", http.StatusUnauthorized)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			fmt.Fprint(
				writer,
				`{"ok":true,"data":{"status":"ok","ha_ws_connected":true,"relay_instance_id":"relay-cloud"}}`,
			)
		},
	))
	defer server.Close()

	oldResolver := resolveCloudRelayTransportForCLI
	resolveCloudRelayTransportForCLI = func(
		context.Context,
		runtimeConfig,
	) (relayTransportSelection, error) {
		return relayTransportSelection{
			BaseURL:    server.URL,
			Client:     server.Client(),
			Credential: "cloud-device",
			DeviceMode: true,
			Via:        relayViaCloud,
		}, nil
	}
	t.Cleanup(func() {
		resolveCloudRelayTransportForCLI = oldResolver
	})

	metadata := cloudMetadataForTest(strings.Repeat("c", 32))
	cfg := runtimeConfig{
		ProfileID:       "profile-cloud",
		RelayInstanceID: "relay-cloud",
		RoutePolicy:     routePolicyCloud,
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateReady,
			Current: &metadata,
		},
	}
	current, ok := deviceSetupState(
		paths,
		cfg,
		installState{},
		"codex",
	)
	if !ok ||
		!current.ConfigOK ||
		!current.TokenOK ||
		!current.RelayOK ||
		!current.WSOK {
		t.Fatalf("Cloud-only setup state = %+v, transport ok=%v", current, ok)
	}
}
