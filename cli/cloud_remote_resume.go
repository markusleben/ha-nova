package main

import (
	"context"
	"os"
	"strings"
)

// cloudRemoteResumeCheckpointReady reports whether the selected profile holds
// a pending Cloud authorization at "cloud_verified" or later. Only such a
// checkpoint may be resumed from a remote (SSH) session: the OAuth token
// already exists — the flow refreshes it without a browser — and every
// secret-store operation after explicit preflight is a no-UI operation by
// the cloudSetupCoordinator contract.
func cloudRemoteResumeCheckpointReady(cfg runtimeConfig) bool {
	if cfg.Cloud == nil || cfg.Cloud.Pending == nil {
		return false
	}
	switch cfg.Cloud.State {
	case cloudStateCloudVerified, cloudStateDeviceBoundOrPaired:
		return true
	default:
		return false
	}
}

// enableCloudRemoteResumeSession relaxes ONLY the SSH detection of the
// desktop-session gate for one explicitly requested checkpoint resume.
// Elevated processes and WSL stay refused, a TTY is still required, and the
// OAuth browser opener is replaced by a hard failure so a broken token
// refresh can never silently fall back to a desktop-only round — the
// operator is told to run the remaining OAuth round on a desktop instead.
var cloudRemoteResumeStdoutTTY = stdoutIsInteractiveTTY

// cloudRemoteResumeProcessElevated checks real process elevation only —
// on Windows it tolerates session 0 (see the platform implementation).
var cloudRemoteResumeProcessElevated = platformRemoteResumeProcessElevated

// cloudRemoteResumeActive gates the secret-store UI mode: a remote resume
// must never trigger a native credential prompt (on macOS a Keychain prompt
// raised from an SSH session would authorize remote-triggered access), so
// every store preflight downgrades AllowUI to ForbidUI while it is set.
var cloudRemoteResumeActive bool

// cloudRemoteResumeElevationEnvPresent keeps the ORIGINAL gate's
// privilege-escalation environment checks: only the SSH markers are
// tolerated in a remote resume — sudo/doas/pkexec launchers stay refused.
func cloudRemoteResumeElevationEnvPresent() bool {
	for _, name := range []string{
		"SUDO_USER",
		"SUDO_UID",
		"SUDO_GID",
		"DOAS_USER",
		"PKEXEC_UID",
	} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return true
		}
	}
	return false
}

// secretStoreUIPolicyForSetup downgrades AllowUI to ForbidUI while a remote
// resume is active: no native credential prompt may ever be raised from a
// remote session. Outside that mode the requested policy passes through.
func secretStoreUIPolicyForSetup(ui SecretStoreUIPolicy) SecretStoreUIPolicy {
	if cloudRemoteResumeActive {
		return SecretStoreForbidUI
	}
	return ui
}

func enableCloudRemoteResumeSession() {
	cloudRemoteResumeActive = true
	cloudInteractivePromptSessionForSetup = func() bool {
		return uiInputSupportsTTY() &&
			cloudRemoteResumeStdoutTTY() &&
			!cloudRemoteResumeElevationEnvPresent() &&
			!cloudRemoteResumeProcessElevated() &&
			!nativePromptRunsUnderWSL()
	}
	openCloudOAuthBrowserForSetup = func(
		_ context.Context,
		_ string,
	) error {
		return newCloudError(
			CloudErrUnsupportedPlatform,
			"open the OAuth browser in a remote resume session — the saved "+
				"authorization could not be refreshed, so the remaining OAuth "+
				"round needs an interactive desktop session",
			nil,
		)
	}
}
