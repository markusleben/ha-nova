package main

import (
	"bufio"
	"io"
	"runtime"
	"strings"
)

// "Only I use this account on this computer: confirm risky changes here in
// the window" (0.32 spec, "What a user sees" 1): asked once, right after
// the owner's Home Assistant sign-in and before pairing, so the pairing
// carries the answer (own_computer) and an interrupted setup declares
// nothing. Re-pairing a computer asks the same question here.

// ownComputerAskedForSetup: test seam.
var ownComputerAskedForSetup = ownComputerAsked

// ownComputerAsked: only where a yes can work. The App approves in a
// computer's window (pair info window: 1), the key isn't in a file, and on
// a Mac not before its old sign-in cleanup reports complete (spec §5.3).
func ownComputerAsked(cfg runtimeConfig) bool {
	store := pairingCredentialStore()
	if runtime.GOOS == "darwin" || store == "" || store == "file" {
		return false
	}
	_, window := relayPairCapabilities(cfg.RelayBaseURL)
	return window
}

// askOwnComputer is the terminal's question. Enter is yes (D1); only a yes
// declares, so Ctrl+C or any other answer leaves the computer off, and the
// owner can still turn it on with "This is my computer" on the NOVA page.
func askOwnComputer(reader *bufio.Reader, out io.Writer) bool {
	renderSetupParagraph(out, setupT("setup.own.question"))
	answer, err := promptWizardLineFromReader(reader, out, setupT("setup.own.prompt"), "")
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "y", "yes":
		return true
	}
	return false
}
