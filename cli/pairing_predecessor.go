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
// removed at activation: without a question when no other profile here is
// paired, since nothing on this computer then uses the plain id; otherwise
// only after the owner's yes to its name. A pairing that can't ask leaves it
// listed and says so.
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
	if !otherProfilePaired(paths, profile) {
		return armed.Previous.DeviceID, nil
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

// otherProfilePaired: another server profile here holds a device pairing or
// one in progress (unknown counts as yes).
func otherProfilePaired(paths runtimePaths, profile string) bool {
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		return true
	}
	for _, name := range doc.profileNames() {
		cfg, ok := doc.flatProfile(name)
		if name != profile && (!ok || cfg.RelaySecureBaseURL != "" || cfg.PendingSecureBaseURL != "" || cfg.Cloud != nil) {
			return true
		}
	}
	return false
}
