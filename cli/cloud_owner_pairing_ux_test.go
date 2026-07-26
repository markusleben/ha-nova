package main

import (
	"bufio"
	"strings"
	"testing"
)

func TestRemoteOwnerPairingKeepsOAuthAndOwnerBrowserSessionsSeparate(
	t *testing.T,
) {
	previousOpen := openBrowserForSetup
	browserCalls := 0
	openBrowserForSetup = func(string) error {
		browserCalls++
		return nil
	}
	t.Cleanup(func() {
		openBrowserForSetup = previousOpen
	})
	capabilityURL := "https://unit.ui.nabu.casa/api/hassio_ingress/secret"
	var output strings.Builder

	code, err := promptRemoteOwnerPairingCode(
		bufio.NewReader(strings.NewReader("123456\n")),
		&output,
		cloudRemotePairingPrompt{AppURL: capabilityURL},
	)
	if err != nil || code != "123456" {
		t.Fatalf("owner pairing code=%q err=%v", code, err)
	}
	for _, required := range []string{
		"OAuth sign-in is complete",
		"separate private/incognito window",
		"Home Assistant Owner",
		"standard Home Assistant account is preferred",
		"Owner OAuth account is also supported",
	} {
		if !strings.Contains(output.String(), required) {
			t.Fatalf("owner pairing UX missing %q: %s", required, output.String())
		}
	}
	if browserCalls != 0 {
		t.Fatalf("owner pairing reused OAuth browser %d times", browserCalls)
	}
	if strings.Contains(output.String(), capabilityURL) ||
		strings.Contains(output.String(), "/api/hassio_ingress/") {
		t.Fatalf("owner pairing exposed capability URL: %s", output.String())
	}
}

func TestRemoteOwnerPairingRetryIsConciseAndActionable(t *testing.T) {
	var output strings.Builder

	code, err := promptRemoteOwnerPairingCode(
		bufio.NewReader(strings.NewReader("654321\n")),
		&output,
		cloudRemotePairingPrompt{
			AppURL:      "https://unit.ui.nabu.casa/api/hassio_ingress/secret",
			RetryReason: rejectedOwnerPairingCodeMessage,
		},
	)
	if err != nil || code != "654321" {
		t.Fatalf("retry pairing code=%q err=%v", code, err)
	}
	for _, required := range []string{
		rejectedOwnerPairingCodeMessage,
		"Generate a fresh code",
		"New six-digit code",
	} {
		if !strings.Contains(output.String(), required) {
			t.Fatalf("owner retry UX missing %q: %s", required, output.String())
		}
	}
	if strings.Contains(output.String(), "OAuth sign-in is complete") {
		t.Fatalf("owner retry repeated initial instructions: %s", output.String())
	}
}
