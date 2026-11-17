package main

import (
	"net/url"
	"strings"
)

// darwinKeychainRoot starts every macOS Keychain slot from 0.32 on. No
// historic slot starts with it (they all start with "ha-nova"), so the binary
// signed as com.markusleben.ha-nova.cli2 creates each item fresh and an
// earlier release can't read one without a Keychain prompt.
const darwinKeychainRoot = "com.markusleben.ha-nova.cli2"

// darwinKeychainSlot is the macOS Keychain service that holds a historic
// slot: <root>/<kind>/<profile>. Link slots name their profile by the store's
// account. HA_NOVA_KEYRING_SERVICE only picks a relay-token slot under the
// root, never a historic one, and an override that names a device or Link
// slot is refused, so it never aliases one. "" refuses a slot nobody mapped.
func darwinKeychainSlot(service, account string) string {
	if override := relayAuthTokenServiceName(); override != keyringServiceName && service == override {
		if strings.HasPrefix(override, deviceCredentialService) || strings.HasPrefix(override, "ha-nova.oauth.") {
			return ""
		}
		return darwinKeychainRoot + "/relay-token/" + url.PathEscape("override-"+override)
	}
	kind, profile := "", defaultServerProfileName
	switch {
	case service == oauthSecretCurrentService:
		kind, profile = "link-current", account
	case service == oauthSecretPendingService:
		kind, profile = "link-pending", account
	case service == oauthSecretRetiringService:
		kind, profile = "link-retiring", account
	case strings.HasPrefix(service, oauthLegacyCleanupServicePrefix):
		kind, profile = "link-legacy-"+strings.TrimPrefix(service, oauthLegacyCleanupServicePrefix), account
	case strings.HasPrefix(service, oauthSecretPreflightServicePrefix):
		kind, profile = "link-preflight", strings.TrimPrefix(service, oauthSecretPreflightServicePrefix)
	case service == keyringServiceName:
		kind = "relay-token"
	case service == deviceCredentialProbeService:
		kind = "probe"
	case service == deviceCredentialService:
		kind = "device"
	case service == deviceCredentialPendingService:
		kind = "device-pending"
	default:
		if profile = deviceCredentialProfileFromService(service); profile == "" {
			return ""
		}
		kind = "device"
		if strings.HasPrefix(service, deviceCredentialPendingService+".") {
			kind = "device-pending"
		}
	}
	if profile == "" {
		return ""
	}
	return darwinKeychainRoot + "/" + kind + "/" + url.PathEscape(profile)
}
