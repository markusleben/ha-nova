package main

import (
	"errors"
	"io"
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

const secureStorageSessionReason = "HA NOVA can't use this computer's secure storage from this terminal (for example over SSH)"
const secureStorageSessionNextStep = "open a normal terminal on this computer's desktop (not over SSH, not as administrator)"

// secureStorageSessionProblem reports whether err is a secure-storage failure
// in a session that is not a normal desktop terminal (SSH, administrator,
// WSL, Windows session 0). Then the one fix is a desktop terminal.
func secureStorageSessionProblem(err error) bool {
	return isSecureStorageUnavailableError(err) && !nativeSecretPromptBaseContextAvailable()
}

// renderSetupSecureStorageFailure prints a setup failure; a secure-storage
// failure outside a desktop terminal gets the plain next step, with the raw
// error kept as a detail line.
func renderSetupSecureStorageFailure(out io.Writer, prefix string, err error) {
	if !secureStorageSessionProblem(err) {
		renderSetupErrorLine(out, "%s: %s", prefix, err)
		return
	}
	renderSetupErrorLine(out, "%s.", secureStorageSessionReason)
	renderSetupParagraphTight(out,
		"Next step: "+secureStorageSessionNextStep+" and run: ha-nova setup",
		"Details: "+prefix+": "+err.Error(),
	)
}

// localRelayCredentialError carries a secure-storage failure of the local
// leg (the device credential read). It is never a Home Assistant Link
// problem, even though the shared worker reports it with a CloudError code.
type localRelayCredentialError struct{ cause error }

func (e *localRelayCredentialError) Error() string {
	if secureStorageSessionProblem(e.cause) {
		return secureStorageSessionReason + "; " + secureStorageSessionNextStep + " and run it there (details: " + e.cause.Error() + ")"
	}
	return "cannot read this computer's saved HA NOVA credential: " + e.cause.Error()
}

func (e *localRelayCredentialError) Unwrap() error { return e.cause }
