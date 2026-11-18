package main

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// The key store's way out in the same run (MAR-244 §1): where a named profile
// or a locked keyring stops pairing and no desktop session here can unlock it,
// setup and ha-nova pair ask whether to keep this computer's key in a private
// file, as pair --credential-store=file does, instead of naming that command.

// fileKeyStoreOffer: a probe error whose way out is the private file. It reads
// and unwraps as the probe's own error.
type fileKeyStoreOffer struct {
	error
	cause error // the keyring's answer: an unlock setup can offer here wins over the file
}

func (e *fileKeyStoreOffer) Unwrap() error { return e.error }

// askFileKeyStore asks only in a terminal, only for such an error, and only
// where no unlock can be offered here. Only choosing Yes with the arrow keys,
// or typing exactly y or yes at the plain prompt, opts in: the menu's y key
// doesn't pick it, so stray typing never switches the store.
func askFileKeyStore(reader *bufio.Reader, out io.Writer, probeErr error) (bool, error) {
	var offer *fileKeyStoreOffer
	if reader == nil || !errors.As(probeErr, &offer) || setupSecureStorageRecoveryAvailableNow(offer.cause) {
		return false, nil
	}
	renderSetupParagraph(out,
		"HA NOVA can't use this computer's keyring from here (no one is logged in to its desktop, or the keyring stays locked).",
		"It can keep this computer's key in a private file instead, readable only by your user account. Any program running as your user can read that file, and this computer then never confirms changes in a window. A server paired through the keyring may need to be paired again.",
	)
	const title = "Keep this computer's key in a private file?"
	answer, err := promptSetupMenu(reader, out, setupMenuSpec{
		Title:        title,
		Prompt:       "Use ↑/↓ or j/k, Enter to select, Ctrl+C to cancel",
		DefaultValue: "no",
		Options:      []setupMenuOption{{Value: "file", Label: "Yes, use a private file"}, {Value: "no", Label: "No"}},
	}, func() (string, error) {
		line, err := promptLineWithOptions(reader, out, title+" [y/N]", "", false)
		if typed := strings.ToLower(strings.TrimSpace(line)); typed == "y" || typed == "yes" {
			return "file", err
		}
		return "no", err
	})
	return err == nil && answer == "file", err
}

// useFileKeyStore switches this install to the private file, as
// pair --credential-store=file does: a readable keyring key moves along first,
// and a Keychain key is never copied (errDeviceKeyStaysInKeychain: nothing
// changes).
func useFileKeyStore() (moved bool, err error) {
	moved, err = migrateKeyringDeviceCredentialToFile()
	if err == nil {
		forceDeviceCredentialFileMode()
		deviceKeyFileChosen = true
	}
	return moved, err
}

// deviceKeyFileChosen: this process switched the device key to the private
// file because the keyring can't be used here, so the legacy shared token
// (which needs that keyring) can't be stored either (setup's pairing step).
var deviceKeyFileChosen bool

// keyStoreFirst: the pairing guard couldn't read the keyring from here (no
// desktop session, no Secret Service, or a locked keyring), so the store is
// chosen first and the caller runs the guard again against it: the default
// profile's file fallback (the storage probe, with its note), an explicit
// --credential-store=file, or the person's yes at a terminal. A switch runs
// under the caller's lock after its mutation guard (config and retirement),
// and moves every readable keyring key first, as the flag always did. A
// pending activation in a keyring nobody can read here can't resume here
// either. false: nothing changed, and the guard's error stands.
func keyStoreFirst(guardErr error, explicitFile bool, reader *bufio.Reader, out io.Writer, locked func(func() error) error, mutationGuard func() error, probe func() (deviceStorageProbe, error)) bool {
	if guardErr == nil || deviceSecretFileBacked() || !(isDesktopKeyringSessionUnavailableError(guardErr) || isDesktopKeyringUnavailableError(guardErr) ||
		errors.Is(guardErr, errDesktopKeyringLocked) || errors.Is(guardErr, errDesktopKeyringInitializationRequired)) {
		return false
	}
	switchStore := func() bool {
		if err := locked(func() error {
			if err := mutationGuard(); err != nil {
				return err
			}
			_, err := useFileKeyStore()
			return err
		}); err != nil {
			renderSetupErrorLine(out, "%s", keyringErrorText(err))
			return false
		}
		return true
	}
	if explicitFile {
		return switchStore()
	}
	var found deviceStorageProbe
	probeErr := locked(func() (err error) {
		found, err = probe()
		return err
	})
	if probeErr == nil {
		if found.note != "" {
			renderSetupParagraph(out, found.note)
		}
		return deviceSecretFileBacked()
	}
	if yes, err := askFileKeyStore(reader, out, probeErr); err != nil || !yes {
		return false
	}
	return switchStore()
}
