package main

import (
	"os"
	"strings"
	"testing"
)

func TestDarwinKeychainRootIsTheSigningIdentifier(t *testing.T) {
	for _, script := range []string{
		"../scripts/release/build-sign-darwin-binaries.sh",
		"../scripts/release/verify-macos-signature.sh",
	} {
		data, err := os.ReadFile(script)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `IDENTIFIER="`+darwinKeychainRoot+`"`) {
			t.Errorf("%s does not sign as %s; the new slots and the new identity switch together", script, darwinKeychainRoot)
		}
	}
}

func TestDarwinKeychainSlotsLiveUnderTheNewRootOnly(t *testing.T) {
	t.Setenv("HA_NOVA_KEYRING_SERVICE", "")
	root := darwinKeychainRoot + "/"
	cases := map[[2]string]string{
		{deviceCredentialService, "markus"}:                            root + "device/default",
		{deviceCredentialService + ".markus-home", "markus"}:           root + "device/markus-home",
		{deviceCredentialPendingService, "markus"}:                     root + "device-pending/default",
		{deviceCredentialPendingService + ".markus-home", "markus"}:    root + "device-pending/markus-home",
		{deviceCredentialProbeService, "markus"}:                       root + "probe/default",
		{keyringServiceName, "markus"}:                                 root + "relay-token/default",
		{oauthSecretCurrentService, "p-1"}:                             root + "link-current/p-1",
		{oauthSecretPendingService, "p-1"}:                             root + "link-pending/p-1",
		{oauthSecretRetiringService, "p-1"}:                            root + "link-retiring/p-1",
		{oauthSecretPreflightServicePrefix + "0123abcd", "probe-0123"}: root + "link-preflight/0123abcd",
	}
	seen := map[string]bool{}
	for in, want := range cases {
		got := darwinKeychainSlot(in[0], in[1])
		if got != want {
			t.Errorf("darwinKeychainSlot(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
		if seen[got] {
			t.Errorf("two slots share %q", got)
		}
		seen[got] = true
		if strings.HasPrefix(got, "ha-nova") {
			t.Errorf("%q maps to a historic slot name", in[0])
		}
	}
}

func TestDarwinKeychainSlotRefusesUnknownAndEmptyNames(t *testing.T) {
	t.Setenv("HA_NOVA_KEYRING_SERVICE", "")
	for _, in := range [][2]string{
		{"ha-nova.something-else", "markus"},
		{"", "markus"},
		{oauthSecretCurrentService, ""},
		{deviceCredentialService + ".", "markus"},
	} {
		if got := darwinKeychainSlot(in[0], in[1]); got != "" {
			t.Errorf("darwinKeychainSlot(%q, %q) = %q, want a refusal", in[0], in[1], got)
		}
	}
}

func TestKeyringServiceOverrideOnlyPicksARelayTokenSlotOnMacOS(t *testing.T) {
	for _, override := range []string{"ha-nova.test.private-rc", "ha-nova.test/../../device/default"} {
		t.Setenv("HA_NOVA_KEYRING_SERVICE", override)
		got := darwinKeychainSlot(override, "markus")
		want := darwinKeychainRoot + "/relay-token/override-"
		if !strings.HasPrefix(got, want) || strings.Count(got, "/") != 2 {
			t.Errorf("override %q maps to %q, want one slot under %q", override, got, want)
		}
		if device := darwinKeychainSlot(deviceCredentialService, "markus"); device != darwinKeychainRoot+"/device/default" {
			t.Errorf("with override %q the device slot moved to %q", override, device)
		}
	}
}

func TestKeyringServiceOverrideNamingADeviceOrLinkSlotIsRefusedOnMacOS(t *testing.T) {
	for _, override := range []string{
		deviceCredentialService,
		deviceCredentialPendingService + ".markus-home",
		deviceCredentialProbeService,
		oauthSecretCurrentService,
		oauthSecretPreflightServicePrefix + "0123abcd",
	} {
		t.Setenv("HA_NOVA_KEYRING_SERVICE", override)
		if got := darwinKeychainSlot(override, "markus"); got != "" {
			t.Errorf("override %q maps to %q, want a refusal so it never aliases that slot", override, got)
		}
	}
}
