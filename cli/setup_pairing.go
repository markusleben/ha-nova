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

func runSetupPairingFlow(reader *bufio.Reader, out io.Writer, cfg runtimeConfig) (string, error) {
	renderSetupParagraph(out,
		"Open NOVA Home Base in the Home Assistant sidebar and find the current six-digit pairing code.",
		`If Home Base is not in the sidebar, open the NOVA Relay app page and choose "Open Web UI".`,
	)
	renderSetupLink(out, "This will open:", haRelayAppPageURL(cfg.HAURL))
	if _, err := promptWizardLineFromReader(reader, out, "Press Enter to open NOVA Relay", ""); err != nil {
		return "", err
	}
	openAnnouncedBrowserURL(out, haRelayAppPageURL(cfg.HAURL))

	for {
		entered, err := promptWizardLineFromReader(reader, out, "Six-digit Home Base pairing code (or type 'manual')", "")
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

		token, err := exchangeRelayPairingCodeForSetup(httpClient, cfg.RelayBaseURL, code)
		switch {
		case err == nil:
			renderSetupSuccessLine(out, "This device is paired with NOVA Relay")
			return token, nil
		case errors.Is(err, errRelayPairingRejected):
			renderSetupErrorLine(out, "That code was not accepted. Refresh Home Base and enter the current code.")
		case errors.Is(err, errRelayPairingUnsupported):
			renderSetupErrorLine(out, "This NOVA Relay version does not support pairing yet. Update the App, or type 'manual' to use an explicit relay token.")
		default:
			var rateLimit *relayPairingRateLimitError
			if errors.As(err, &rateLimit) {
				renderSetupErrorLine(out, "Too many pairing attempts. Wait at least %d seconds, then use the current Home Base code.", rateLimit.retryAfterSeconds)
				continue
			}
			renderSetupErrorLine(out, "Could not pair with NOVA Relay: %s", err)
		}
	}
}
