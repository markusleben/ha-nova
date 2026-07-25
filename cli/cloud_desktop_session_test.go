package main

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

func installSafeNativePromptContext(t *testing.T) {
	t.Helper()
	oldElevated := nativePromptProcessElevated
	oldWSL := nativePromptRunsUnderWSL
	nativePromptProcessElevated = func() bool { return false }
	nativePromptRunsUnderWSL = func() bool { return false }
	t.Cleanup(func() {
		nativePromptProcessElevated = oldElevated
		nativePromptRunsUnderWSL = oldWSL
	})
	for _, name := range []string{
		"SSH_CONNECTION",
		"SSH_CLIENT",
		"SSH_TTY",
		"SUDO_USER",
		"SUDO_UID",
		"SUDO_GID",
		"DOAS_USER",
		"PKEXEC_UID",
		"WSL_DISTRO_NAME",
		"WSL_INTEROP",
	} {
		t.Setenv(name, "")
	}
}

func TestNativeSecretPromptContextRejectsRemoteAndElevatedProcesses(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		env      string
		elevated bool
		wsl      bool
	}{
		{name: "SSH connection", env: "SSH_CONNECTION"},
		{name: "SSH client", env: "SSH_CLIENT"},
		{name: "SSH terminal", env: "SSH_TTY"},
		{name: "sudo user", env: "SUDO_USER"},
		{name: "sudo uid", env: "SUDO_UID"},
		{name: "sudo gid", env: "SUDO_GID"},
		{name: "doas", env: "DOAS_USER"},
		{name: "pkexec", env: "PKEXEC_UID"},
		{name: "root or elevated", elevated: true},
		{name: "WSL", wsl: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			installSafeNativePromptContext(t)
			if testCase.env != "" {
				t.Setenv(testCase.env, "present")
			}
			nativePromptProcessElevated = func() bool {
				return testCase.elevated
			}
			nativePromptRunsUnderWSL = func() bool {
				return testCase.wsl
			}
			if nativeSecretPromptBaseContextAvailable() {
				t.Fatal("unsafe process context was allowed to open native secure storage")
			}
		})
	}
}

func TestNativeSecretPromptContextAllowsOrdinaryDesktopProcess(t *testing.T) {
	installSafeNativePromptContext(t)
	if !nativeSecretPromptBaseContextAvailable() {
		t.Fatal("ordinary non-elevated local process was rejected")
	}
}

func TestCompletedWizardDoesNotOfferCloudFromSSHSession(t *testing.T) {
	installSafeNativePromptContext(t)
	t.Setenv("SSH_CONNECTION", "192.0.2.1 12345 192.0.2.2 22")
	oldInputTTY := uiInputSupportsTTY
	oldOutputTTY := writerSupportsTTYForSetup
	oldCoordinator := cloudCoordinatorForSetup
	oldPromptEligible := cloudSetupPromptEligible
	oldReusable := reusableLocalDeviceForCloudSetup
	coordinator := successfulCloudCoordinatorForTest()
	uiInputSupportsTTY = func() bool { return true }
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	cloudCoordinatorForSetup = coordinator
	cloudSetupPromptEligible = func(out io.Writer) bool {
		return nativeSecretPromptSessionAvailable(out)
	}
	reusableLocalDeviceForCloudSetup = func(runtimeConfig) (bool, error) {
		t.Fatal("unsafe wizard reached local device preparation")
		return false, nil
	}
	t.Cleanup(func() {
		uiInputSupportsTTY = oldInputTTY
		writerSupportsTTYForSetup = oldOutputTTY
		cloudCoordinatorForSetup = oldCoordinator
		cloudSetupPromptEligible = oldPromptEligible
		reusableLocalDeviceForCloudSetup = oldReusable
	})

	var output strings.Builder
	_, attempted, exit := maybeOfferCloudForCompletedSetup(
		bufio.NewReader(strings.NewReader("")),
		&output,
		runtimePaths{},
		completedLocalCloudTestConfig(),
		false,
	)
	if attempted || exit != 0 ||
		coordinator.preflightCalls != 0 ||
		coordinator.addCalls != 0 ||
		!strings.Contains(output.String(), "only from an interactive desktop session") {
		t.Fatalf(
			"unsafe wizard attempted=%v exit=%d calls=%d/%d output=%s",
			attempted,
			exit,
			coordinator.preflightCalls,
			coordinator.addCalls,
			output.String(),
		)
	}
}
