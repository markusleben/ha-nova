package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

var errSetupBack = errors.New("setup back")
var errSetupExit = errors.New("setup exit")
var errSetupClientPrerequisite = errors.New("setup client prerequisite")
var errSetupRelayTokenStep = errors.New("setup relay token step")

// setupPagePairingError: pairing with the setup page's sign-in didn't work;
// step 2 says why (cause), and its Try again may reuse the session.
type setupPagePairingError struct {
	cause   error
	session haSignInSession
}

func (e *setupPagePairingError) Error() string {
	return "setup pairing with the page sign-in did not finish: " + e.cause.Error()
}

var errSetupPairingStep = errors.New("setup pairing step")
var errSetupHostStep = errors.New("setup host step")
var errSetupInstallStep = errors.New("setup relay install step")

func promptWizardLineFromReader(reader *bufio.Reader, out io.Writer, label, defaultValue string) (string, error) {
	fmt.Fprintln(out)
	return promptLineWithOptions(reader, out, label, defaultValue, true)
}

func promptWizardYesNoFromReader(reader *bufio.Reader, out io.Writer, label string, defaultYes bool) (bool, error) {
	return promptYesNoWithOptions(reader, out, label, defaultYes, true)
}
