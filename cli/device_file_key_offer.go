package main

import (
	"bufio"
	"errors"
	"io"
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
// where no unlock can be offered here; Enter is no.
func askFileKeyStore(reader *bufio.Reader, out io.Writer, probeErr error) (bool, error) {
	var offer *fileKeyStoreOffer
	if reader == nil || !errors.As(probeErr, &offer) || setupSecureStorageRecoveryAvailableNow(offer.cause) {
		return false, nil
	}
	renderSetupParagraph(out,
		"HA NOVA can't use this computer's keyring from here (no one is logged in to its desktop, or the keyring stays locked).",
		"It can keep this computer's key in a private file instead, readable only by your user account. Any program running as your user can read that file, and this computer then never confirms changes in a window. A server paired through the keyring may need to be paired again.",
	)
	return promptYesNoMenu(reader, out, "Keep this computer's key in a private file?", "Yes, use a private file", "No", false)
}

// useFileKeyStore switches this install to the private file, as
// pair --credential-store=file does: a readable keyring key moves along first,
// and a Keychain key is never copied (errDeviceKeyStaysInKeychain: nothing
// changes).
func useFileKeyStore() (moved bool, err error) {
	moved, err = migrateKeyringDeviceCredentialToFile()
	if err == nil {
		forceDeviceCredentialFileMode()
	}
	return moved, err
}
