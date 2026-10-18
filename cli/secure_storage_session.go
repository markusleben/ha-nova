package main

import (
	"errors"
	"io"
	"os"
	"runtime"
	"strings"
	"syscall"
)

// errorNoSuchLogonSession is Windows ERROR_NO_SUCH_LOGON_SESSION (1312).
// Credential Manager returns it in a network logon session (for example
// PowerShell over key-auth OpenSSH). Matched by number because the message
// text is localized ("Eine angegebene Anmeldesitzung ist nicht vorhanden.").
const errorNoSuchLogonSession = syscall.Errno(1312)

// isWindowsNetworkLogonSessionError matches the Credential Manager failure
// in Windows network logon sessions: there is no interactive logon session to
// hold the credential store. Local console, Windows Terminal, and RDP sessions
// are unaffected. The English text match stays for errors forwarded as text;
// not build-tagged so every platform classifies them and tests run everywhere.
func isWindowsNetworkLogonSessionError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, errorNoSuchLogonSession) ||
		strings.Contains(strings.ToLower(err.Error()), "a specified logon session does not exist")
}

// isSecureStorageUnavailableError: this computer's secure storage refused or
// could not serve HA NOVA. The secure-storage worker reports these with
// CloudError codes for local credentials too, so the code alone never means
// Home Assistant Link.
func isSecureStorageUnavailableError(err error) bool {
	return isWindowsNetworkLogonSessionError(err) ||
		IsCloudErrorCode(err, CloudErrSecretStore) ||
		IsCloudErrorCode(err, CloudErrSecretStoreLocked) ||
		IsCloudErrorCode(err, CloudErrSecretUIForbidden)
}

const secureStorageSessionReason = "HA NOVA can't reach secure storage from this terminal (for example over SSH, or as administrator)"

// secureStorageSessionPlatformOS: the desktop-terminal fix holds on Windows
// (Credential Manager needs an interactive logon) and macOS (the login
// keychain); Linux keeps its own keyring guidance (a keyring nobody unlocks
// needs setup --service, not a desktop).
var secureStorageSessionPlatformOS = runtime.GOOS

// secureStorageSessionNextStep names the machine: over SSH "this computer"
// reads as the one the user sits at.
func secureStorageSessionNextStep() string {
	where := "the computer HA NOVA is installed on"
	if host, err := os.Hostname(); err == nil && strings.TrimSpace(host) != "" {
		where += " (" + strings.TrimSpace(host) + ")"
	}
	return "sign in to the desktop of " + where + " and open a normal terminal there (not as administrator)"
}

// secureStorageSessionProblem reports whether err is a secure-storage failure
// in a session that is not a normal desktop terminal (SSH, administrator,
// WSL, Windows session 0). Then the one fix is a desktop terminal.
func secureStorageSessionProblem(err error) bool {
	return (secureStorageSessionPlatformOS == "windows" || secureStorageSessionPlatformOS == "darwin") &&
		isSecureStorageUnavailableError(err) && !nativeSecretPromptBaseContextAvailable()
}

// secureStorageReportedError: setup already printed this failure with its
// next step; the caller must not print the raw error again as the last line.
type secureStorageReportedError struct{ cause error }

func (e *secureStorageReportedError) Error() string { return e.cause.Error() }
func (e *secureStorageReportedError) Unwrap() error { return e.cause }

// isSetupFailureReported: printed already by renderSetupSecureStorageFailure.
func isSetupFailureReported(err error) bool {
	var reported *secureStorageReportedError
	return errors.As(err, &reported)
}

// renderSetupSecureStorageFailure prints a setup failure and returns the
// error to hand back; a secure-storage failure outside a desktop terminal
// gets the plain next step, the raw error kept as a detail line, and comes
// back marked as reported.
func renderSetupSecureStorageFailure(out io.Writer, prefix string, err error) error {
	if !secureStorageSessionProblem(err) {
		renderSetupErrorLine(out, "%s: %s", prefix, err)
		return err
	}
	renderSetupErrorLine(out, "%s.", secureStorageSessionReason)
	renderSetupParagraphTight(out,
		"Next step: "+secureStorageSessionNextStep()+", then run: ha-nova setup",
		"Details: "+prefix+": "+err.Error(),
	)
	return &secureStorageReportedError{cause: err}
}

// localRelayCredentialError carries a CloudError of the local leg (the
// device credential read). It is never a Home Assistant Link problem, even
// though the shared secure-storage worker reports it with a CloudError code.
type localRelayCredentialError struct{ cause error }

func (e *localRelayCredentialError) Error() string {
	code := "unknown"
	var cloudErr *CloudError
	if errors.As(e.cause, &cloudErr) {
		code = string(cloudErr.Code)
	}
	if secureStorageSessionProblem(e.cause) {
		return secureStorageSessionReason + "; " + secureStorageSessionNextStep() + ", start your AI assistant from that terminal, and try again (" + code + ")"
	}
	if isSecureStorageUnavailableError(e.cause) || IsCloudErrorCode(e.cause, CloudErrSecretPromptCanceled) {
		return "cannot read this computer's saved HA NOVA credential (" + code + "); unlock this computer's keychain or keyring (sign in to its desktop) and try again, or run: ha-nova doctor"
	}
	return "cannot read this computer's saved HA NOVA credential (" + code + "); run: ha-nova doctor"
}

func (e *localRelayCredentialError) Unwrap() error { return e.cause }
