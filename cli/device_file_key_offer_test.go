package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// MAR-244 §1: where the keyring can't be used from here, setup and pair ask
// for the private file in the same run; Enter changes nothing.

func TestNamedProfileOffersTheFileKeyInTheSameRun(t *testing.T) {
	withDeviceStorageTestHome(t)
	stubStorageCanaries(t, fmt.Errorf("%w: exec dbus-launch not found", errDesktopKeyringSessionUnavailable))
	resetServerProfileSelection(t)
	setActiveServerProfile("cabin")
	t.Cleanup(func() { deviceCredentialFileModeForced, deviceCredentialFileModeExplicit = false, false })

	_, probeErr := probeDeviceCredentialStorage()
	if probeErr == nil || !strings.Contains(probeErr.Error(), "--credential-store=file") || deviceCredentialFileModeForced {
		t.Fatalf("named profile: %v, forced %v", probeErr, deviceCredentialFileModeForced)
	}
	var out bytes.Buffer
	if yes, err := askFileKeyStore(bufio.NewReader(strings.NewReader("\n")), &out, probeErr); yes || err != nil || deviceCredentialFileModeForced {
		t.Fatalf("Enter: yes %v err %v", yes, err)
	}
	if !strings.Contains(out.String(), "Any program running as your user can read that file") {
		t.Fatalf("offer text: %q", out.String())
	}
	yes, err := askFileKeyStore(bufio.NewReader(strings.NewReader("y\n")), &out, probeErr)
	if !yes || err != nil {
		t.Fatalf("yes: %v %v", yes, err)
	}
	if _, err := useFileKeyStore(); err != nil {
		t.Fatalf("switch: %v", err)
	}
	if probe, err := probeDeviceCredentialStorage(); err != nil || probe.mode != "file" || !deviceCredentialFileModeExplicit {
		t.Fatalf("after the yes: %+v %v", probe, err)
	}
}

func TestLockedKeyringOffersTheFileKeyWhereNoUnlockCan(t *testing.T) {
	withDeviceStorageTestHome(t)
	stubStorageCanaries(t, fmt.Errorf("%w: default collection is locked", errDesktopKeyringLocked))
	_, probeErr := probeDeviceCredentialStorage()
	if !errors.Is(probeErr, errDesktopKeyringLocked) {
		t.Fatalf("locked keyring must still read as locked: %v", probeErr)
	}
	var out bytes.Buffer
	if yes, err := askFileKeyStore(bufio.NewReader(strings.NewReader("y\n")), &out, probeErr); !yes || err != nil {
		t.Fatalf("locked, no unlock here: yes %v err %v out %q", yes, err, out.String())
	}
}

func TestFileKeyOfferNeedsATerminalAndAKeyStoreProblem(t *testing.T) {
	var out bytes.Buffer
	offer := &fileKeyStoreOffer{error: errors.New("keyring locked"), cause: errDesktopKeyringLocked}
	if yes, err := askFileKeyStore(nil, &out, offer); yes || err != nil || out.Len() != 0 {
		t.Fatalf("no terminal: yes %v err %v out %q", yes, err, out.String())
	}
	if yes, err := askFileKeyStore(bufio.NewReader(strings.NewReader("y\n")), &out, errors.New("disk full")); yes || err != nil || out.Len() != 0 {
		t.Fatalf("other error: yes %v err %v out %q", yes, err, out.String())
	}
}
