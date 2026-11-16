package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
)

// A named profile paired before named profiles got their own install id left
// its entry on the App under this computer's plain id, which its re-pair as
// "<id>-<name>" doesn't replace (pairingClientInstallID; spec §5.3). On the
// owner's sign-in pairing the App names that one entry, and the pairing has it
// removed at activation only after the owner's yes to its name: another
// computer with a copied config, or this computer's default profile, may
// still use that id. A pairing that can't ask leaves it listed and says so.
func signInPredecessorToRetire(
	reader *bufio.Reader,
	out io.Writer,
	paths runtimePaths,
	cfg runtimeConfig,
	ingress *CloudIngressClient,
) (string, error) {
	profile := activeServerProfile()
	if profile == defaultServerProfileName || cfg.ClientInstallID == "" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), haSignInStepTimeout)
	defer cancel()
	armed, err := armSignInPairingRequest(ctx, ingress, CloudEndpointPairArm, signInArmRequest{PreviousInstallID: cfg.ClientInstallID})
	if err != nil || armed.Previous == nil {
		return "", nil // the pairing's own arm reports a failure; an App before 0.32 names nothing
	}
	name := terminalSafeName(armed.Previous.Name)
	if reader == nil {
		renderSetupParagraph(out, setupT("setup.retire.listed", "name", name))
		return "", nil
	}
	renderSetupParagraph(out, setupT("setup.retire.question", "name", name))
	answer, err := promptWizardLineFromReader(reader, out, setupT("setup.own.prompt"), "")
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if err == nil {
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "y", "yes":
			return armed.Previous.DeviceID, nil
		}
	}
	renderSetupParagraph(out, setupT("setup.retire.kept", "name", name))
	return "", nil
}
