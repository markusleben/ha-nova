package main

import (
	"context"
	"strings"
	"testing"
)

func TestCloudRemoteResumeCheckpointReady(t *testing.T) {
	if cloudRemoteResumeCheckpointReady(runtimeConfig{}) {
		t.Fatal("empty config must not be resumable")
	}
	for state, want := range map[cloudLifecycleState]bool{
		cloudStateAuthorizing:         false,
		cloudStateTokenStored:         false,
		cloudStateCloudVerified:       true,
		cloudStateDeviceBoundOrPaired: true,
	} {
		cfg := pendingCloudOnlyCommandConfig(state)
		if got := cloudRemoteResumeCheckpointReady(cfg); got != want {
			t.Fatalf("state %s: ready=%v want %v", state, got, want)
		}
	}
	cfg := pendingCloudOnlyCommandConfig(cloudStateCloudVerified)
	cfg.Cloud.Pending = nil
	if cloudRemoteResumeCheckpointReady(cfg) {
		t.Fatal("cloud_verified without pending metadata must not be resumable")
	}
}

func TestRemoteResumeRefusedWithoutResumableCheckpoint(t *testing.T) {
	for name, state := range map[string]cloudLifecycleState{
		"authorizing":  cloudStateAuthorizing,
		"token_stored": cloudStateTokenStored,
	} {
		t.Run(name, func(t *testing.T) {
			resetServerProfileSelection(t)
			paths := setupServerCommandTest(t, `{"schema_version":1}`)
			if err := saveConfig(
				paths,
				pendingCloudOnlyCommandConfig(state),
			); err != nil {
				t.Fatal(err)
			}
			coordinator := successfulCloudCoordinatorForTest()
			installCloudCommandCoordinator(t, coordinator)
			installCloudCommandPromptSession(t, false)

			exit, output := captureCommandOutput(t, func() int {
				return runCloudConnectCommand(
					paths,
					[]string{"--remote-resume"},
					false,
				)
			})
			if exit != 1 ||
				!strings.Contains(output, "--remote-resume only resumes an existing checkpoint") {
				t.Fatalf("early-checkpoint resume exit=%d output=%s", exit, output)
			}
			if coordinator.preflightCalls != 0 || coordinator.addCalls != 0 {
				t.Fatalf(
					"refused resume reached secure storage: calls=%d/%d",
					coordinator.preflightCalls,
					coordinator.addCalls,
				)
			}
		})
	}
}

func TestRemoteResumeSessionGateToleratesSSHOnly(t *testing.T) {
	oldGate := cloudInteractivePromptSessionForSetup
	oldOpener := openCloudOAuthBrowserForSetup
	oldInput := uiInputSupportsTTY
	oldStdout := cloudRemoteResumeStdoutTTY
	oldElevated := cloudRemoteResumeProcessElevated
	oldWSL := nativePromptRunsUnderWSL
	t.Cleanup(func() {
		cloudInteractivePromptSessionForSetup = oldGate
		openCloudOAuthBrowserForSetup = oldOpener
		uiInputSupportsTTY = oldInput
		cloudRemoteResumeStdoutTTY = oldStdout
		cloudRemoteResumeProcessElevated = oldElevated
		nativePromptRunsUnderWSL = oldWSL
	})
	uiInputSupportsTTY = func() bool { return true }
	cloudRemoteResumeStdoutTTY = func() bool { return true }
	cloudRemoteResumeProcessElevated = func() bool { return false }
	nativePromptRunsUnderWSL = func() bool { return false }
	t.Setenv("SSH_CONNECTION", "10.0.0.1 1 10.0.0.2 22")

	oldActive := cloudRemoteResumeActive
	t.Cleanup(func() { cloudRemoteResumeActive = oldActive })
	enableCloudRemoteResumeSession()

	if !cloudInteractivePromptSessionForSetup() {
		t.Fatal("remote-resume gate must tolerate an SSH session with a TTY")
	}
	if secretStoreUIPolicyForSetup(SecretStoreAllowUI) != SecretStoreForbidUI {
		t.Fatal("remote resume must downgrade AllowUI to ForbidUI")
	}
	t.Setenv("SUDO_USER", "root")
	if cloudInteractivePromptSessionForSetup() {
		t.Fatal("remote-resume gate must still refuse sudo launchers")
	}
	t.Setenv("SUDO_USER", "")
	cloudRemoteResumeProcessElevated = func() bool { return true }
	if cloudInteractivePromptSessionForSetup() {
		t.Fatal("remote-resume gate must still refuse elevated processes")
	}
	cloudRemoteResumeProcessElevated = func() bool { return false }
	nativePromptRunsUnderWSL = func() bool { return true }
	if cloudInteractivePromptSessionForSetup() {
		t.Fatal("remote-resume gate must still refuse WSL")
	}
	if err := openCloudOAuthBrowserForSetup(
		context.Background(),
		"https://example.invalid",
	); err == nil {
		t.Fatal("remote-resume must never open an OAuth browser")
	}
}
