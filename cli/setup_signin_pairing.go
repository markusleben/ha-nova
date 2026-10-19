package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
)

// "Sign in with Home Assistant" in the setup pairing step
// (docs/work/2026-10-16-ha-sign-in-pairing-spec.md). Every soft failure
// prints one plain line and stays in the same step: retryable ones offer to
// sign in again, all others continue with the six-digit code.

// Test seams.
var (
	openHASignInIngressForSetup   = openHASignInIngress
	resolveHASignInOriginForSetup = resolveHASignInOrigin
	pairDeviceV2ArmedForSetup     = pairDeviceV2Armed
	signInPairForSetup            = runSecurePairingWithProvisioner
	haSignInOffered               = defaultHASignInOffered
)

// The pairing exchange after the sign-in: arming gets a step's bound
// (haSignInStepTimeout), then info, start, and finish may take as long as the
// Relay keeps the armed slot (PAIR_ARM_TTL_MS, 2 min; pairDeviceV2Armed).
// This caps both. A busy Home Assistant answers each request in 15–30 s
// (0.27 lane F1).
var haSignInPairTimeout = 2 * time.Minute

// haSignInPairingError marks a failure inside the ingress pairing exchange,
// which falls back to the code; failures of the shared local tail do not.
type haSignInPairingError struct{ err error }

func (e *haSignInPairingError) Error() string { return e.err.Error() }
func (e *haSignInPairingError) Unwrap() error { return e.err }

// runHASignInPairingStep returns paired=true on success, or a six-digit code
// the user typed at the prompt; (false, "", nil) means "continue with the
// code prompt"; an error is navigation or a hard stop.
func runHASignInPairingStep(
	reader *bufio.Reader,
	out io.Writer,
	paths runtimePaths,
	cfg *runtimeConfig,
	lifecycleMarker ...[]byte,
) (bool, string, error) {
	origin, err := resolveHASignInOriginForSetup(context.Background(), cfg.HAURL)
	if err != nil {
		renderSetupWarningLine(out, "%s", haSignInFallbackLine(err, HACurrentUser{}))
		renderSetupParagraph(out, "Use a six-digit code from NOVA instead.")
		return false, "", nil
	}
	renderSetupParagraph(out,
		"Your browser opens Home Assistant: sign in as the owner.",
		"(Home Assistant asks to give “http://127.0.0.1:…” access: that's this computer.)",
		"Each computer gets its own connection; remove it in NOVA anytime.",
	)
	again := false
	for {
		answer, err := promptHASignInChoice(reader, out, again)
		if err != nil {
			return false, "", err
		}
		switch answer {
		case "signin":
		case "code":
			return false, "", nil
		case "manual":
			return false, "", errSetupRelayTokenStep
		default: // six digits typed at the fallback prompt
			return false, answer, nil
		}
		paired, retry, err := signInAndPair(out, paths, cfg, origin.String(), lifecycleMarker...)
		if paired || err != nil {
			return paired, "", err
		}
		if !retry {
			renderSetupParagraph(out, "Use a six-digit code from NOVA instead.")
			return false, "", nil
		}
		again = true
	}
}

// promptHASignInChoice returns "signin", "code", "manual", or six digits
// typed at the fallback prompt. The menu has no title: the step title
// above already names the question.
func promptHASignInChoice(reader *bufio.Reader, out io.Writer, again bool) (string, error) {
	spec := setupMenuSpec{
		Prompt:       "Use ↑/↓ or j/k, Enter to select, Esc to go back, Ctrl+C to exit",
		AllowBack:    true,
		DefaultValue: "signin",
		Options: []setupMenuOption{
			{Value: "signin", Label: "Sign in with Home Assistant"},
			{Value: "code", Label: "Use a six-digit code instead"},
		},
	}
	label := "Press Enter to sign in, or type 'code' to use a six-digit code from NOVA"
	if again {
		spec.Options[0].Label = "Sign in again"
		label = "Press Enter to sign in again, or type 'code' to use a six-digit code from NOVA"
	}
	return promptSetupMenu(reader, out, spec, func() (string, error) {
		for {
			answer, err := promptWizardLineFromReader(reader, out, label, "")
			if err != nil {
				return "", err
			}
			switch normalized := strings.ToLower(strings.TrimSpace(answer)); normalized {
			case "", "y", "yes":
				return "signin", nil
			case "code", "manual":
				return normalized, nil
			}
			if code, err := normalizeRelayPairingCode(answer); err == nil {
				return code, nil
			}
			renderSetupErrorLine(out, "Press Enter to sign in, or type 'code'.")
		}
	})
}

func signInAndPair(
	out io.Writer,
	paths runtimePaths,
	cfg *runtimeConfig,
	haOrigin string,
	lifecycleMarker ...[]byte,
) (paired bool, retry bool, err error) {
	ctx := context.Background()
	session, err := openHASignInIngressForSetup(ctx, out, haOrigin)
	if err != nil {
		renderSetupWarningLine(out, "%s", haSignInFallbackLine(err, session.user))
		return false, haSignInRetryable(err), nil
	}
	renderSetupSuccessLine(out, "Signed in to Home Assistant as %s (owner)", haSignInUserName(session.user))
	paired, soft, err := pairWithHASignInSession(out, paths, cfg, session, lifecycleMarker...)
	return paired, errors.Is(soft, errSignInNotOwner), err
}

// pairWithHASignInSession connects this computer through the owner's ingress
// session, from sign-in or from "Sign in and install NOVA". soft is a failure
// of the ingress exchange, already said in one line; the caller decides
// what comes next.
func pairWithHASignInSession(
	out io.Writer,
	paths runtimePaths,
	cfg *runtimeConfig,
	session haSignInSession,
	lifecycleMarker ...[]byte,
) (paired bool, soft error, err error) {
	_, pairErr := runSetupSpinnerWithResult(out, "Connecting this computer...", setupSpinnerDebounce, func() (struct{}, error) {
		var pairErr error
		lockErr := withSetupLifecycleGuard(paths, lifecycleMarker, func() error {
			if err := requireSettledDeviceCredentialRetirement(paths, activeServerProfile()); err != nil {
				return err
			}
			save := func(c *runtimeConfig) error {
				return saveSetupConfigIfCurrent(paths, *c, lifecycleMarker)
			}
			provision := func(meta deviceMetadata) (*provisionedCredential, error) {
				// From here, not from before the lock: the budget is the exchange's.
				ctx, cancel := context.WithTimeout(context.Background(), haSignInStepTimeout+haSignInPairTimeout)
				defer cancel()
				provisioned, err := pairDeviceV2ArmedForSetup(ctx, session.ingress, meta)
				if err != nil {
					return nil, &haSignInPairingError{err: err}
				}
				return provisioned, nil
			}
			_, pairErr = signInPairForSetup(cfg.RelayBaseURL, provision, cfg, save, defaultPairingClientInfo(), SecretStoreForbidUI)
			return pairErr
		})
		if lockErr != nil {
			pairErr = lockErr
		}
		return struct{}{}, pairErr
	})
	var exchange *haSignInPairingError
	switch {
	case pairErr == nil:
		setupSignedInUser = session.user.ID // the phone step proposes this user's phone
		setupOwnerIngress = session.ingress // chat apps (step 3) use the owner's session
		return true, nil, nil
	case errors.As(pairErr, &exchange):
		renderSetupWarningLine(out, "%s", haSignInFallbackLine(pairErr, session.user))
		return false, pairErr, nil
	case errors.Is(pairErr, errPinMismatch):
		renderSetupErrorLine(out, "The relay's secure identity did not match. Try again; if it repeats, someone may be intercepting the connection.")
		return false, nil, pairErr
	default:
		renderSetupErrorLine(out, "Could not finish connecting this computer. Make sure the NOVA App is running, then run ha-nova setup again.")
		return false, nil, pairErr
	}
}

// haSignInSlow: Home Assistant, or the Relay's owner check through it, did
// not answer in time. The same sign-in can try again.
func haSignInSlow(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errSignInOwnerCheckSlow) ||
		IsCloudErrorCode(err, CloudErrTimeout) || IsCloudErrorCode(err, CloudErrNetwork)
}

// Retrying the sign-in can help when the user stopped, waited too long,
// picked the wrong account, or Home Assistant was briefly unreachable.
func haSignInRetryable(err error) bool {
	return errors.Is(err, errSignInStopped) || errors.Is(err, errSignInBrowser) ||
		errors.Is(err, errSignInNotOwner) ||
		IsCloudErrorCode(err, CloudErrNetwork) || IsCloudErrorCode(err, CloudErrTimeout)
}

func haSignInFallbackLine(err error, user HACurrentUser) string {
	var certificate *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	var pairing *haSignInPairingError
	switch {
	case errors.Is(err, errSignInStopped):
		return "Sign-in stopped."
	case errors.Is(err, errSignInBrowser) &&
		(IsCloudErrorCode(err, CloudErrOAuthCanceled) || IsCloudErrorCode(err, CloudErrOAuthRejected)):
		return "Sign-in was canceled."
	case errors.Is(err, errSignInBrowser) && IsCloudErrorCode(err, CloudErrTimeout):
		return "No sign-in arrived within five minutes."
	case errors.Is(err, errSignInNotOwner):
		if terminalSafeName(user.Name) == "" {
			return "The account you signed in with is not the Home Assistant owner. Only the owner can connect a computer."
		}
		return fmt.Sprintf("Signed in as %s, who is not the Home Assistant owner. Only the owner can connect a computer.", terminalSafeName(user.Name))
	case errors.Is(err, errSignInAppMissing) && haSignInSlow(err):
		return "Could not finish the sign-in: Home Assistant answered too slowly."
	case errors.Is(err, errSignInAppMissing):
		return "Home Assistant did not find a running NOVA App, so sign-in cannot connect this computer."
	case errors.Is(err, errSignInRelayUnsupported):
		return "Your NOVA App does not support sign-in yet. Update it in Home Assistant when convenient."
	case errors.Is(err, errSignInRelayNotReady):
		return "The NOVA App is not ready to connect computers yet. Open NOVA in Home Assistant for details, then run ha-nova setup again."
	case errors.As(err, &pairing) && haSignInSlow(err):
		return "Home Assistant answered too slowly, so connecting this computer did not finish."
	case errors.As(err, &pairing):
		return "Connecting through Home Assistant did not finish."
	case errors.As(err, &certificate), errors.As(err, &authority):
		return "Could not finish the sign-in: Home Assistant's HTTPS certificate is not trusted on this computer."
	case IsCloudErrorCode(err, CloudErrRedirectRejected):
		return "Could not finish the sign-in: Home Assistant answered from a different address. Use the address Home Assistant shows in its own settings."
	case IsCloudErrorCode(err, CloudErrNetwork), IsCloudErrorCode(err, CloudErrTimeout),
		IsCloudErrorCode(err, CloudErrOAuthOutcomeUnknown):
		return "Could not finish the sign-in: Home Assistant did not answer."
	default:
		return "Could not finish the sign-in with Home Assistant."
	}
}

func haSignInUserName(user HACurrentUser) string {
	if name := terminalSafeName(user.Name); name != "" {
		return name
	}
	return "the owner"
}

// Names come from Home Assistant; control characters (terminal escape
// sequences) and format characters (bidi overrides, zero-width) never reach
// the terminal.
func terminalSafeName(name string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, name))
}
