package main

import (
	"context"
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

// pairInfo is one read of the App's unauthenticated GET /pair/v1/info.
// answered: any HTTP reply came. body holds a 200's body, and a 401's for
// a caller that asked for it; nil otherwise or when it couldn't be read.
// v1, the two capabilities, and the version are decoded independently,
// so a malformed field in one group never hides another.
type pairInfo struct {
	answered       bool
	status         int
	body           []byte
	v1             bool
	signIn, window bool
	relayVersion   string
}

// readPairInfo is every caller's one read of the pairing capabilities; each
// asks what it needs of the same answer. Only a caller telling a standalone
// container's token gate apart waits for a 401's body (unauthorizedBody).
func readPairInfo(ctx context.Context, relayURL string, unauthorizedBody bool) pairInfo {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(relayURL, "/")+"/pair/v1/info", nil)
	if err != nil {
		return pairInfo{}
	}
	req.URL.User = nil // never forward copied URL userinfo as Basic auth
	resp, err := httpClient.Do(req)
	if err != nil {
		return pairInfo{}
	}
	defer resp.Body.Close()
	info := pairInfo{answered: true, status: resp.StatusCode}
	if resp.StatusCode != http.StatusOK && !(unauthorizedBody && resp.StatusCode == http.StatusUnauthorized) {
		return info
	}
	if info.body, err = readAllLimited(resp.Body, maxRelayPairingResponseBytes); err != nil {
		info.body = nil
		return info
	}
	if resp.StatusCode != http.StatusOK {
		return info
	}
	var protocol struct {
		OK   bool `json:"ok"`
		Data struct {
			ProtocolVersion string `json:"protocol_version"`
		} `json:"data"`
	}
	info.v1 = json.Unmarshal(info.body, &protocol) == nil && protocol.OK && protocol.Data.ProtocolVersion == "v1"
	var capabilities struct {
		OK   bool `json:"ok"`
		Data struct {
			SignIn bool `json:"sign_in"`
			Window int  `json:"window"`
		} `json:"data"`
	}
	if json.Unmarshal(info.body, &capabilities) == nil && capabilities.OK {
		info.signIn, info.window = capabilities.Data.SignIn, capabilities.Data.Window == 1
	}
	var version struct {
		Data struct {
			RelayVersion string `json:"relay_version"`
		} `json:"data"`
	}
	_ = json.Unmarshal(info.body, &version) // best effort: a bad version reads as unknown
	info.relayVersion = strings.TrimSpace(version.Data.RelayVersion)
	return info
}

// relayAdvertisesSignIn reads sign_in from the pairing capabilities; any
// failure reads as "no" and the step stays the code prompt.
func relayAdvertisesSignIn(relayURL string) bool {
	return readPairInfo(context.Background(), relayURL, false).signIn
}

// relayPairCapabilities: sign_in, and window (the App approves in a
// computer's own window: spec §1), before any credential exists.
func relayPairCapabilities(relayURL string) (signIn, window bool) {
	info := readPairInfo(context.Background(), relayURL, false)
	return info.signIn, info.window
}
