package main

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestRunSecurePairingCannotOverwriteResumableLocalPending(
	t *testing.T,
) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	pending := validCredential(92)
	if err := writePendingDeviceCredential(pending); err != nil {
		t.Fatal(err)
	}
	cfg := runtimeConfig{
		PendingSecureBaseURL: "https://pending:8792",
		PendingSpkiPin:       "pending-pin",
	}
	originalPair := pairDeviceV1ForPairing
	pairCalls := 0
	pairDeviceV1ForPairing = func(
		*http.Client,
		string,
		string,
		deviceMetadata,
	) (*provisionedCredential, error) {
		pairCalls++
		return nil, errors.New("must not consume a code")
	}
	t.Cleanup(func() { pairDeviceV1ForPairing = originalPair })

	_, err := runSecurePairing(
		"http://relay:8791",
		"123456",
		&cfg,
		func(*runtimeConfig) error { return nil },
		defaultPairingClientInfo(),
	)
	if err == nil ||
		!strings.Contains(err.Error(), "activation is still pending") ||
		pairCalls != 0 {
		t.Fatalf("pending guard err=%v pairCalls=%d", err, pairCalls)
	}
	got, exists, readErr := readPendingDeviceCredential()
	if readErr != nil || !exists || got != pending {
		t.Fatalf(
			"pending credential=%q exists=%v err=%v",
			got,
			exists,
			readErr,
		)
	}
}
