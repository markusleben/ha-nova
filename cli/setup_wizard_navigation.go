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
