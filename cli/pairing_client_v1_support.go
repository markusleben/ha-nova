package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

// Sentinel errors the setup/uninstall flows branch on.
var (
	errPairingCodeRejected = errors.New("pairing code is invalid or expired")
	errPairingInactive     = errors.New("no active pairing code; ask the owner to click Connect a device in NOVA")
	errPinMismatch         = errors.New("relay TLS identity did not match the pinned fingerprint")
	errDeviceUnauthorized  = errors.New("device credential was rejected")
	errRelayNotV1          = errors.New("this relay does not support secure device pairing; update the NOVA Relay App")
)

// parsedDeviceCredential mirrors nova/src/security/device-credential.ts:
// hanova-dev-v1.<deviceId(22)>.<secret(43)>, base64url segments.
type parsedDeviceCredential struct {
	deviceID string
	secret   string
}

var deviceCredentialPattern = regexp.MustCompile(`^hanova-dev-v1\.([A-Za-z0-9_-]{22})\.([A-Za-z0-9_-]{43})$`)

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
func pairingStatusError(status int, body []byte) error {
	code := relayPairingErrorCode(body) // reuse the existing envelope code reader
	switch {
	case status == 429:
		return &relayPairingRateLimitError{retryAfterSeconds: 60}
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
