package main

import (
	"bufio"
	"errors"
	"io"
	"runtime"
	"strings"
)

// "Confirm risky changes (doors, alarm, deletes) here with one click in a
// window?" (0.32 spec, "What a user sees" 1): asked once, right after
// the owner's Home Assistant sign-in and before pairing, so the pairing
// carries the answer (own_computer) and an interrupted setup declares
// nothing. Re-pairing a computer asks the same question here.

// ownComputerAskedForSetup: test seam.
var ownComputerAskedForSetup = ownComputerAsked

// ownComputerAsked: only where a yes can work. The App approves in a
// computer's window (pair info window: 1), and this computer can.
func ownComputerAsked(cfg runtimeConfig) bool {
	if !ownComputerHere() {
		return false
	}
	_, window := relayPairCapabilities(cfg.RelayBaseURL)
	return window
}

// ownComputerHere: a confirmation window can open here, the key isn't in a
// file, and not on a Mac before its old sign-in cleanup reports complete
// (spec §5.3). The setup page asks on this alone before it installs the
// App, then asks the installed App (pageSignIn).
func ownComputerHere() bool {
	store := pairingCredentialStore()
	return runtime.GOOS != "darwin" && store != "" && store != "file" && windowAvailable()
}

// askOwnComputer is the terminal's question. Enter is yes (D1); only a yes
// declares, so no input or any other answer leaves the computer off (an
// answer gets where it confirms instead), and the owner can still turn it
// on on the NOVA page. Back, Exit, and Ctrl+C come back as the error:
// nothing pairs.
func askOwnComputer(reader *bufio.Reader, out io.Writer) (bool, error) {
	renderSetupParagraph(out, setupT("setup.own.question"))
	answer, err := promptWizardLineFromReader(reader, out, setupT("setup.own.prompt"), "")
	if errors.Is(err, io.EOF) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "y", "yes":
		return true, nil
	}
	renderSetupParagraph(out, setupT("setup.own.no"))
	return false, nil
}

// askOwnComputerForSetup: the question where it can work, else no.
func askOwnComputerForSetup(reader *bufio.Reader, out io.Writer, cfg runtimeConfig) (bool, error) {
	if !ownComputerAskedForSetup(cfg) {
		return false, nil
	}
	return askOwnComputer(reader, out)
}
