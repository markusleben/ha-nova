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
		"Sign in with Home Assistant to connect this computer. Your browser opens the Home Assistant login: sign in with the owner's username and password.",
		"Home Assistant warns \"You're about to give http://127.0.0.1:…/ha-nova access\": that is HA NOVA on this computer.",
		"This computer gets its own connection, which you can remove in NOVA at any time.",
	)
	label := "Press Enter to sign in, or type 'code' to use a six-digit code from NOVA"
	for {
		answer, err := promptWizardLineFromReader(reader, out, label, "")
		if err != nil {
			return false, "", err
		}
		switch normalized := strings.ToLower(strings.TrimSpace(answer)); normalized {
		case "", "y", "yes":
		case "code":
			return false, "", nil
		case "manual":
			return false, "", errSetupRelayTokenStep
		default:
			if code, err := normalizeRelayPairingCode(answer); err == nil {
				return false, code, nil
			}
			renderSetupErrorLine(out, "Press Enter to sign in, or type 'code'.")
			continue
		}
		paired, retry, err := signInAndPair(out, paths, cfg, origin.String(), lifecycleMarker...)
		if paired || err != nil {
			return paired, "", err
		}
		if !retry {
			renderSetupParagraph(out, "Use a six-digit code from NOVA instead.")
			return false, "", nil
		}
		label = "Press Enter to sign in again, or type 'code' to use a six-digit code from NOVA"
	}
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
	// The ingress pairing exchange gets the same bound as the other steps
	// after the browser returns.
	ctx, cancel := context.WithTimeout(ctx, haSignInStepTimeout)
	defer cancel()
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
	var soft *haSignInPairingError
	switch {
	case pairErr == nil:
		setupSignedInUser = session.user.ID // the phone step proposes this user's phone
		return true, false, nil
	case errors.As(pairErr, &soft):
		renderSetupWarningLine(out, "%s", haSignInFallbackLine(pairErr, session.user))
		return false, errors.Is(pairErr, errSignInNotOwner), nil
	case errors.Is(pairErr, errPinMismatch):
		renderSetupErrorLine(out, "The relay's secure identity did not match. Try again; if it repeats, someone may be intercepting the connection.")
		return false, false, pairErr
	default:
		renderSetupErrorLine(out, "Could not finish connecting this computer. Make sure the NOVA Relay App is running, then run ha-nova setup again.")
		return false, false, pairErr
	}
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
	case errors.Is(err, errSignInAppMissing):
		return "Home Assistant did not find a running NOVA Relay App, so sign-in cannot connect this computer."
	case errors.Is(err, errSignInRelayUnsupported):
		return "Your NOVA Relay App does not support sign-in yet. Update it in Home Assistant when convenient."
	case errors.Is(err, errSignInRelayNotReady):
		return "NOVA Relay is not ready to connect computers yet. Open NOVA in Home Assistant for details, then run ha-nova setup again."
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
