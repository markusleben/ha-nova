package main

import (
	"bufio"
	"context"
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

// errSetupDevicePaired signals the wizard that the secure (v1) pairing already
// stored a device credential + the secure endpoint; there is no relay token to
// persist, and the connection is already proven by activation.
var errSetupDevicePaired = errors.New("device paired securely")

// Test hooks: default to the real implementations. The wizard pairs through
// secure v1; a standalone container connects with its token only.
var probePairingV1ForSetup = probePairingV1
var securePairForSetup = runSecurePairingAfterInteractivePreflight
var probeRelayPairingForSetup = probeRelayPairing

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

// relayPairingAnswer: what GET /pair/v1/info says about the Relay behind an
// address.
type relayPairingAnswer int

const (
	relayAnswerNone       relayPairingAnswer = iota // no HTTP answer at all
	relayAnswerSecure                               // a NOVA App with secure pairing
	relayAnswerStandalone                           // the standalone container's own token gate
	relayAnswerOther                                // a proxy, redirect, or another service
)

// probeRelayPairing tells a standalone container (its own 401 UNAUTHORIZED
// envelope) from any other answer, so setup asks for the Relay token only
// where a NOVA Relay asked for it.
func probeRelayPairing(relayBaseURL string) relayPairingAnswer {
	info := readPairInfo(context.Background(), relayBaseURL, true)
	switch {
	case !info.answered:
		return relayAnswerNone
	case info.v1:
		return relayAnswerSecure
	case info.status == http.StatusUnauthorized && relayPairingErrorCode(info.body) == "UNAUTHORIZED":
		return relayAnswerStandalone
	}
	return relayAnswerOther
}

// probePairingV1 reports whether the relay supports secure device pairing
// (GET /pair/v1/info). Any failure or non-v1 answer is false; the pairing
// step then reads the answer again in detail (probeRelayPairing).
func probePairingV1(relayBaseURL string) bool {
	return probePairingV1Context(context.Background(), relayBaseURL)
}

// probePairingV1ForNotice: the legacy-access notice's probe, bounded by
// its caller (check-update, update).
var probePairingV1ForNotice = probePairingV1Context

func probePairingV1Context(ctx context.Context, relayBaseURL string) bool {
	return readPairInfo(ctx, relayBaseURL, false).v1
}

// legacyTokenStoreUnavailable is set by the caller when the OS keyring is
// missing AND no service-token file is configured, so a standalone Relay's
// shared token (which needs one of those) cannot be stored on this box.
func runSetupPairingFlow(reader *bufio.Reader, out io.Writer, paths runtimePaths, cfg *runtimeConfig, legacyTokenStoreUnavailable bool, lifecycleMarker ...[]byte) (string, error) {
	// A named profile never keeps the legacy shared token: refuse that
	// exchange before a one-time code is spent, as the add-server flow does.
	legacyTokenStoreUnavailable = legacyTokenStoreUnavailable || activeServerProfile() != defaultServerProfileName
	installed := setupInstalledSignIn // taken once, used or not
	setupInstalledSignIn = nil
	guardPairingMutation := func() error {
		return requireSettledDeviceCredentialRetirement(
			paths,
			activeServerProfile(),
		)
	}
	guard := func() error {
		if err := guardPairingMutation(); err != nil {
			return err
		}
		return validateLocalDeviceReplacementAllowedWithPolicy(
			*cfg,
			explicitPairingSecretStoreUIPolicy(),
		)
	}
	guardErr := withSetupLifecycleGuard(paths, lifecycleMarker, guard)
	var terminal *bufio.Reader // only a person at a terminal answers the key store question
	if terminalReader() != nil {
		terminal = reader
	}
	if keyStoreFirst(guardErr, false, terminal, out,
		func(step func() error) error { return withSetupLifecycleGuard(paths, lifecycleMarker, step) }, guardPairingMutation,
		func() (deviceStorageProbe, error) {
			ctx, cancel := boundedNativeOAuthSecretContext(context.Background(), SecretStoreForbidUI)
			defer cancel()
			return probeDeviceCredentialStorageWithPolicy(ctx, SecretStoreForbidUI)
		}) {
		legacyTokenStoreUnavailable = true // the legacy shared token needs the keyring this run can't use
		guardErr = withSetupLifecycleGuard(paths, lifecycleMarker, guard)
	}
	if guardErr != nil {
		return "", renderSetupSecureStorageFailure(out, "Local pairing cannot start", guardErr)
	}
	// Storage first: never send the user to fetch a one-time code that a broken
	// credential backend would burn. Headless systems switch to the private-file
	// fallback here, with a visible note.
	var probe deviceStorageProbe
	probeOnce := func() error {
		ui := explicitPairingSecretStoreUIPolicy()
		ctx, cancel := boundedNativeOAuthSecretContext(
			context.Background(),
			ui,
		)
		defer cancel()
		var err error
		probe, err = probeDeviceCredentialStorageWithPolicy(
			ctx,
			ui,
		)
		return err
	}
	probeErr := withSetupLifecycleGuard(paths, lifecycleMarker, probeOnce)
	if yes, err := askFileKeyStore(terminal, out, probeErr); err != nil {
		return "", err
	} else if yes { // the owner's opt-in, as pair --credential-store=file
		legacyTokenStoreUnavailable = true
		probeErr = withSetupLifecycleGuard(paths, lifecycleMarker, func() error {
			if err := guardPairingMutation(); err != nil {
				return err
			}
			if moved, err := useFileKeyStore(); err != nil {
				return err
			} else if moved {
				renderSetupParagraph(out, "Moved this install's device credential into private file storage.")
			}
			return probeOnce()
		})
	}
	if probeErr != nil {
		return "", fmt.Errorf("device credential storage unavailable: %w",
			renderSetupSecureStorageFailure(out, "This system cannot store the device credential yet", probeErr))
	}
	if probe.note != "" {
		renderSetupParagraph(out, probe.note)
	}

	// Determine the pairing mode up front, before any one-time code is spent.
	// probePairingV1 returns false for ANY failure — a Relay without secure
	// pairing, one still starting, OR an unreachable one. Re-probe with a
	// detailed result so the cases are told apart: recovered v1 → pair;
	// the standalone container's token gate → token only; any other answer →
	// check the address; unreachable → the code prompt still offers 'manual',
	// and a code gets the retry hint.
	secure := probePairingV1ForSetup(cfg.RelayBaseURL)
	if !secure {
		answer := probeRelayPairingForSetup(cfg.RelayBaseURL)
		switch {
		case answer == relayAnswerSecure:
			secure = true // a transient first probe recovered
		case installed != nil && installed.fromPage:
			// The page's sign-in can't pair yet: step 2 says the App isn't ready,
			// instead of a code prompt the page never hands over.
			return "", &setupPagePairingError{cause: &pairingBeforeFinishError{err: errSignInRelayNotReady}, session: *installed}
		case answer == relayAnswerStandalone:
			return "", setupRelayTokenOnly(out, legacyTokenStoreUnavailable)
		case answer == relayAnswerOther:
			return "", setupRelayNotNova(out)
		case legacyTokenStoreUnavailable:
			return "", setupRelayUnreachable(out)
		}
	}

	if secure {
		if err := prepareSecurePairingClientInstallID(paths, cfg, lifecycleMarker...); err != nil {
			return "", renderSetupSecureStorageFailure(out, "Could not prepare this device before pairing", err)
		}
	}

	// "Sign in and install NOVA" already holds the owner's ingress session:
	// pair with it instead of a second sign-in.
	if secure && installed != nil {
		if !installed.fromPage { // the page asked with its sign-in button
			own, err := askOwnComputerForSetup(reader, out, *cfg)
			if err != nil {
				return "", err
			}
			installed.own = own
		}
		paired, soft, err := pairWithHASignInSession(reader, out, paths, cfg, *installed, lifecycleMarker...)
		if paired {
			renderSetupSuccessLine(out, "This computer is connected")
			return "", errSetupDevicePaired
		}
		if err != nil {
			return "", err
		}
		if installed.fromPage {
			return "", &setupPagePairingError{cause: soft, session: *installed}
		}
	}

	typedCode := ""
	codeChosen := setupPairWithCode
	setupPairWithCode = false
	if secure && !codeChosen && haSignInOffered(out, *cfg) {
		paired, code, err := runHASignInPairingStep(reader, out, paths, cfg, lifecycleMarker...)
		if paired {
			renderSetupSuccessLine(out, "This computer is connected")
			return "", errSetupDevicePaired
		}
		if err != nil {
			return "", err
		}
		typedCode = code
	}

	if typedCode == "" {
		if err := promptToOpenNOVAForPairing(reader, out, cfg.HAURL); err != nil {
			return "", err
		}
	}

	for {
		entered := typedCode
		typedCode = ""
		if entered == "" {
			var err error
			entered, err = promptWizardLineFromReader(reader, out, "Six-digit code from NOVA (or type 'manual')", "")
			if err != nil {
				return "", err
			}
		}
		if strings.EqualFold(strings.TrimSpace(entered), "manual") {
			return "", errSetupRelayTokenStep
		}
		code, err := normalizeRelayPairingCode(entered)
		if err != nil {
			renderSetupErrorLine(out, "%s", err)
			continue
		}
		if !secure {
			return "", setupRelayUnreachable(out)
		}

		var perr error
		lockErr := withSetupLifecycleGuard(paths, lifecycleMarker, func() error {
			if err := guardPairingMutation(); err != nil {
				return err
			}
			save := func(c *runtimeConfig) error {
				return saveSetupConfigIfCurrent(
					paths,
					*c,
					lifecycleMarker,
				)
			}
			_, perr = securePairForSetup(cfg.RelayBaseURL, code, cfg, save, defaultPairingClientInfo())
			return perr
		})
		if lockErr != nil {
			perr = lockErr
		}
		switch {
		case perr == nil:
			renderSetupSuccessLine(out, "This computer is connected")
			return "", errSetupDevicePaired
		case errors.Is(perr, errPairingCodeRejected):
			renderSetupErrorLine(out, "That code was not accepted. Click \"Connect a device\" in NOVA for a fresh code.")
			continue
		case errors.Is(perr, errPairingInactive):
			renderSetupErrorLine(out, "No active code. In NOVA, click \"Connect a device\" first, then enter the code shown.")
			continue
		case errors.Is(perr, errPinMismatch):
			renderSetupErrorLine(out, "The relay's secure identity did not match. Try again; if it repeats, someone may be intercepting the connection.")
			return "", perr
		case errors.Is(perr, errRelayStandalone):
			return "", setupRelayTokenOnly(out, legacyTokenStoreUnavailable)
		case errors.Is(perr, errRelayNotV1):
			return "", setupRelayNotNova(out)
		default:
			var rateLimit *relayPairingRateLimitError
			if errors.As(perr, &rateLimit) {
				renderSetupErrorLine(out, "Too many attempts. Wait at least %d seconds, then use the current code.", rateLimit.retryAfterSeconds)
				continue
			}
			return "", renderSetupSecureStorageFailure(out, "Could not pair", perr)
		}
	}
}

func setupRelayUnreachable(out io.Writer) error {
	renderSetupErrorLine(out, "Could not reach the NOVA App to check secure pairing support. Make sure it is running, then run setup again.")
	return errors.New("relay not reachable to determine secure pairing support")
}

func setupRelayNotNova(out io.Writer) error {
	renderSetupErrorLine(out, "This address answered, but not as a NOVA Relay. Check the Relay address, or wait until the NOVA App has started, then run setup again.")
	return errors.New("relay address did not answer as a NOVA Relay")
}

// setupRelayTokenOnly: a standalone container connects with its token only
// (the token step explains it), or fails here when this run can't store that
// token.
func setupRelayTokenOnly(out io.Writer, tokenStoreUnavailable bool) error {
	if tokenStoreUnavailable {
		renderSetupErrorLine(out, "This NOVA Relay is a standalone container and connects only with its token, which this setup can't store here. Set it up as the default server on a computer with a key store.")
		return errors.New("standalone relay connects with its token only and no store is available for it")
	}
	return errSetupStandaloneToken
}
