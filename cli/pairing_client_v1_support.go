package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// Sentinel errors the setup/uninstall flows branch on.
var (
	errPairingCodeRejected   = errors.New("pairing code is invalid or expired")
	errPairingInactive       = errors.New("no active pairing code; ask a Home Assistant administrator to click Connect a device in NOVA")
	errPinMismatch           = errors.New("relay TLS identity did not match the pinned fingerprint")
	errDeviceUnauthorized    = errors.New("device credential was rejected")
	errRelayNotV1            = errors.New("this relay does not support secure device pairing; update the NOVA Relay App")
	errRelayStandalone       = errors.New("this NOVA Relay is a standalone container and connects with its token only")
	errPairingOutcomeUnknown = errors.New(
		"pairing finish may have reached NOVA Relay; do not submit another code",
	)
)

// parsedDeviceCredential mirrors nova/src/security/device-credential.ts:
// hanova-dev-v1.<deviceId(22)>.<secret(43)>, base64url segments.
type parsedDeviceCredential struct {
	deviceID string
	secret   string
}

var (
	deviceIDPattern         = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)
	deviceCredentialPattern = regexp.MustCompile(`^hanova-dev-v1\.([A-Za-z0-9_-]{22})\.([A-Za-z0-9_-]{43})$`)
)

func validDeviceID(value string) bool {
	return deviceIDPattern.MatchString(value)
}

func parseDeviceCredential(input string) *parsedDeviceCredential {
	if len(input) > 128 {
		return nil
	}
	m := deviceCredentialPattern.FindStringSubmatch(input)
	if m == nil {
		return nil
	}
	return &parsedDeviceCredential{deviceID: m[1], secret: m[2]}
}

// deviceIDOf returns the non-secret device id of a credential, for display and
// for the uninstall "remove this device in NOVA" hint.
func deviceIDOf(credential string) string {
	if p := parseDeviceCredential(credential); p != nil {
		return p.deviceID
	}
	return ""
}

// pairingStatusError maps the relay's error envelope to a typed client error.
func pairingStatusError(status int, body []byte, retryAfter string) error {
	code := relayPairingErrorCode(body) // reuse the existing envelope code reader
	switch {
	case status == 429:
		// Honor the relay's actual Retry-After (the global limiter can keep
		// returning 429 for up to five minutes); do not hardcode 60s.
		return &relayPairingRateLimitError{retryAfterSeconds: pairingRetryAfterSeconds(retryAfter)}
	case status == 409 || code == "PAIRING_INACTIVE" || code == "VALIDATION_ERROR":
		// v1 returns 400/VALIDATION_ERROR when no code is active (deliberately
		// indistinguishable from a malformed request so the LAN cannot detect the
		// window); surface the same "click Connect a device" guidance.
		return errPairingInactive
	case status == 401 || code == "PAIRING_FAILED":
		return errPairingCodeRejected
	case status == 404 || code == "NOT_FOUND":
		// An old relay (<=0.6) has no /pair/v1 routes.
		return errRelayNotV1
	default:
		return fmt.Errorf("relay returned status %d", status)
	}
}

// probePairingV1 checks GET /pair/v1/info so an old relay is detected before a
// pairing attempt, letting the CLI guide an App update instead of failing deep
// in the handshake.
func decodePairInfo(raw []byte) (protocolV1 bool) {
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			ProtocolVersion string `json:"protocol_version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
		return false
	}
	return env.Data.ProtocolVersion == "v1"
}

// relayAdvertisesSignIn reads sign_in from the GET /pair/v1/info capability
// probe; any failure reads as "no" and the step stays the code prompt.
func relayAdvertisesSignIn(relayURL string) bool {
	signIn, _ := relayPairCapabilities(relayURL)
	return signIn
}

// relayPairCapabilities: sign_in, and window (the App approves in a
// computer's own window: spec §1), before any credential exists.
func relayPairCapabilities(relayURL string) (signIn, window bool) {
	request, err := http.NewRequest(http.MethodGet, strings.TrimRight(relayURL, "/")+"/pair/v1/info", nil)
	if err != nil {
		return false, false
	}
	request.URL.User = nil
	response, err := httpClient.Do(request)
	if err != nil {
		return false, false
	}
	defer response.Body.Close()
	body, err := readAllLimited(response.Body, maxRelayPairingResponseBytes)
	var info struct {
		OK   bool `json:"ok"`
		Data struct {
			SignIn bool `json:"sign_in"`
			Window int  `json:"window"`
		} `json:"data"`
	}
	if response.StatusCode != http.StatusOK || err != nil || json.Unmarshal(body, &info) != nil || !info.OK {
		return false, false
	}
	return info.Data.SignIn, info.Data.Window == 1
}
