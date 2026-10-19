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

// errSetupPairingOnPage: pairing with the setup page's sign-in didn't work;
// the page says so on step 2.
var errSetupPairingOnPage = errors.New("setup pairing with the page sign-in did not finish")
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
