package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode"
)

const maxRelayPairingResponseBytes = 32 << 10
const maxRelayPairingTokenBytes = 4_096

var errRelayPairingRejected = errors.New("relay pairing code rejected")
var errRelayPairingUnsupported = errors.New("relay pairing unsupported")
var exchangeRelayPairingCodeForSetup = exchangeRelayPairingCode

// errSetupDevicePaired signals the wizard that the secure (v1) pairing already
// stored a device credential + the secure endpoint; there is no relay token to
// persist, and the connection is already proven by activation.
var errSetupDevicePaired = errors.New("device paired securely")

// Test hooks: default to the real implementations. The wizard tries the secure
// v1 flow first and falls back to the legacy code exchange, so a relay without
// /pair/v1 (or a test with no v1 endpoint) transparently uses the old path.
var probePairingV1ForSetup = probePairingV1
var securePairForSetup = runSecurePairing

type relayPairingRateLimitError struct {
	retryAfterSeconds int
}

func (err *relayPairingRateLimitError) Error() string {
	return fmt.Sprintf("relay pairing rate limited; retry after %d seconds", err.retryAfterSeconds)
}

func normalizeRelayPairingCode(input string) (string, error) {
	code := strings.Map(func(character rune) rune {
		if unicode.IsSpace(character) {
			return -1
		}
		return character
	}, strings.TrimSpace(input))
	if len(code) != 6 {
		return "", errors.New("pairing code must contain exactly six digits")
	}
	for _, character := range code {
		if character < '0' || character > '9' {
			return "", errors.New("pairing code must contain exactly six digits")
		}
	}
	return code, nil
}

func exchangeRelayPairingCode(client *http.Client, relayBaseURL, code string) (string, error) {
	normalizedCode, err := normalizeRelayPairingCode(code)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(struct {
		Code string `json:"code"`
	}{Code: normalizedCode})
	if err != nil {
		return "", err
	}

	url := strings.TrimRight(relayBaseURL, "/") + "/pair"
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	request.URL.User = nil
	request.Header.Set("Content-Type", "application/json")

	pairingClient := *client
	pairingClient.Jar = nil
	pairingClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	response, err := pairingClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("pairing request failed: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxRelayPairingResponseBytes+1))
	if err != nil {
		return "", errors.New("cannot read pairing response")
	}
	if len(body) > maxRelayPairingResponseBytes {
		return "", errors.New("pairing response exceeded the size limit")
	}

	switch response.StatusCode {
	case http.StatusUnauthorized:
		switch relayPairingErrorCode(body) {
		case "PAIRING_FAILED":
			return "", errRelayPairingRejected
		case "UNAUTHORIZED":
			// Relay versions from before pairing protect /pair with the normal
			// bearer middleware. Route those users to update/manual-token help
			// instead of asking them to retry a valid NOVA code forever.
			return "", errRelayPairingUnsupported
		default:
			return "", errors.New("relay returned an invalid pairing error response")
		}
	case http.StatusTooManyRequests:
		return "", &relayPairingRateLimitError{
			retryAfterSeconds: pairingRetryAfterSeconds(response.Header.Get("Retry-After")),
		}
	case http.StatusNotFound:
		return "", errRelayPairingUnsupported
	case http.StatusOK:
		var envelope struct {
			OK   bool `json:"ok"`
			Data struct {
				RelayToken string `json:"relay_token"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil || !envelope.OK {
			return "", errors.New("relay returned an invalid pairing response")
		}
		token := strings.TrimSpace(envelope.Data.RelayToken)
		if token == "" || len(token) > maxRelayPairingTokenBytes || strings.IndexFunc(token, unicode.IsControl) >= 0 {
			return "", errors.New("relay returned an invalid pairing response")
		}
		return token, nil
	default:
		return "", fmt.Errorf("relay pairing failed with HTTP %d", response.StatusCode)
	}
}

func relayPairingErrorCode(body []byte) string {
	var envelope struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.OK {
		return ""
	}
	return envelope.Error.Code
}

func pairingRetryAfterSeconds(header string) int {
	seconds, err := strconv.Atoi(strings.TrimSpace(header))
	if err != nil || seconds < 1 {
		return 60
	}
	if seconds > 300 {
		return 300
	}
	return seconds
}

// probePairingV1 reports whether the relay supports secure device pairing
// (GET /pair/v1/info). Any error or non-v1 answer returns false so the wizard
// falls back to the legacy code exchange.
func probePairingV1(relayBaseURL string) bool {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(relayBaseURL, "/")+"/pair/v1/info", nil)
	if err != nil {
		return false
	}
	req.URL.User = nil // do not forward copied URL userinfo as a Basic auth header
	resp, err := httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	body, err := readAllLimited(resp.Body, maxRelayPairingResponseBytes)
	if err != nil {
		return false
	}
	return decodePairInfo(body)
}

// legacyTokenStoreUnavailable is set by the caller when the OS keyring is
// missing AND no service-token file is configured, so the legacy /pair token
// (which needs one of those) cannot be stored on this box.
func runSetupPairingFlow(reader *bufio.Reader, out io.Writer, paths runtimePaths, cfg *runtimeConfig, legacyTokenStoreUnavailable bool) (string, error) {
	// Storage first: never send the user to fetch a one-time code that a broken
	// credential backend would burn. Headless systems switch to the private-file
	// fallback here, with a visible note.
	probe, probeErr := probeDeviceCredentialStorage()
	if probeErr != nil {
		renderSetupErrorLine(out, "This system cannot store the device credential yet: %s", probeErr)
		return "", fmt.Errorf("device credential storage unavailable: %w", probeErr)
	}
	if probe.note != "" {
		renderSetupParagraph(out, probe.note)
	}

	// Determine the pairing mode up front (the relay URL is known here). Secure v1
	// pairing stores a file-backed device credential and needs no keyring. The
	// legacy /pair fallback instead returns a SHARED token that must go into the
	// OS keyring or a service-token file — so on a keyless box with neither AND a
	// pre-v1 relay, fail NOW, before the pairing UI prompts for and consumes a
	// one-time code, rather than after the exchange fails to store the token.
	secure := probePairingV1ForSetup(cfg.RelayBaseURL)
	if !secure && legacyTokenStoreUnavailable {
		renderSetupErrorLine(out, "This NOVA Relay is too old for secure device pairing, and this system has no key store for the legacy shared token. Update the NOVA Relay App to enable secure pairing.")
		return "", fmt.Errorf("relay predates secure pairing and no store is available for the legacy token")
	}

	renderSetupParagraph(out,
		"Open NOVA in the Home Assistant sidebar and click \"Connect a device\" to get a six-digit code.",
		`If NOVA is not in the sidebar, open the NOVA Relay app page and choose "Open Web UI".`,
	)
	renderSetupLink(out, "This will open:", haRelayAppPageURL(cfg.HAURL))
	if _, err := promptWizardLineFromReader(reader, out, "Press Enter to open NOVA", ""); err != nil {
		return "", err
	}
	openAnnouncedBrowserURL(out, haRelayAppPageURL(cfg.HAURL))

	for {
		entered, err := promptWizardLineFromReader(reader, out, "Six-digit code from NOVA (or type 'manual')", "")
		if err != nil {
			return "", err
		}
		if strings.EqualFold(strings.TrimSpace(entered), "manual") {
			return "", errSetupRelayTokenStep
		}
		code, err := normalizeRelayPairingCode(entered)
		if err != nil {
			renderSetupErrorLine(out, "%s", err)
			continue
		}

		if secure {
			save := func(c *runtimeConfig) error { return saveConfig(paths, *c) }
			_, perr := securePairForSetup(cfg.RelayBaseURL, code, cfg, save, defaultPairingClientInfo())
			switch {
			case perr == nil:
				renderSetupSuccessLine(out, "This device is paired securely with NOVA Relay")
				return "", errSetupDevicePaired
			case errors.Is(perr, errPairingCodeRejected):
				renderSetupErrorLine(out, "That code was not accepted. Click \"Connect a device\" in NOVA for a fresh code.")
				continue
			case errors.Is(perr, errPairingInactive):
				renderSetupErrorLine(out, "No active code. In NOVA, click \"Connect a device\" first, then enter the code shown.")
				continue
			case errors.Is(perr, errPinMismatch):
				renderSetupErrorLine(out, "The relay's secure identity did not match. Try again; if it repeats, someone may be intercepting the connection.")
				continue
			case errors.Is(perr, errRelayNotV1):
				// The relay turned out not to support v1 mid-flow. Falling through to
				// the legacy /pair exchange needs a keyring for its shared token; on a
				// box without one, fail now (same guard as up front) rather than
				// consuming another code that could never be persisted.
				if legacyTokenStoreUnavailable {
					renderSetupErrorLine(out, "This NOVA Relay is too old for secure device pairing, and this system has no key store for the legacy shared token. Update the NOVA Relay App to enable secure pairing.")
					return "", fmt.Errorf("relay predates secure pairing mid-flow and no store is available for the legacy token")
				}
				secure = false // relay changed under us; fall through to legacy
			default:
				var rateLimit *relayPairingRateLimitError
				if errors.As(perr, &rateLimit) {
					renderSetupErrorLine(out, "Too many attempts. Wait at least %d seconds, then use the current code.", rateLimit.retryAfterSeconds)
					continue
				}
				renderSetupErrorLine(out, "Could not pair: %s", perr)
				continue
			}
		}

		token, err := exchangeRelayPairingCodeForSetup(httpClient, cfg.RelayBaseURL, code)
		switch {
		case err == nil:
			renderSetupSuccessLine(out, "This device is paired with NOVA Relay")
			return token, nil
		case errors.Is(err, errRelayPairingRejected):
			renderSetupErrorLine(out, "That code was not accepted. Refresh NOVA and enter the current code.")
		case errors.Is(err, errRelayPairingUnsupported):
			renderSetupErrorLine(out, "This NOVA Relay version does not support pairing yet. Update the App, or type 'manual' to use an explicit relay token.")
		default:
			var rateLimit *relayPairingRateLimitError
			if errors.As(err, &rateLimit) {
				renderSetupErrorLine(out, "Too many pairing attempts. Wait at least %d seconds, then use the current NOVA code.", rateLimit.retryAfterSeconds)
				continue
			}
			renderSetupErrorLine(out, "Could not pair with NOVA Relay: %s", err)
		}
	}
}
