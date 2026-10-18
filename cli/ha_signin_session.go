package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Home Assistant sign-in for local pairing
// (docs/work/2026-10-16-ha-sign-in-pairing-spec.md). The Home Assistant tokens
// exist only in this process: the access token authenticates the WebSocket,
// and the refresh token is revoked as soon as the ingress session exists,
// before any pairing request.

const haSignInTimeout = 5 * time.Minute

// Bounds everything after the browser returns, so an unresponsive Home
// Assistant still reaches the fallback and the deferred revocation.
var haSignInStepTimeout = 30 * time.Second

var (
	errSignInBrowser    = errors.New("browser sign-in did not complete")
	errSignInStopped    = errors.New("sign-in stopped")
	errSignInAppMissing = errors.New("Home Assistant did not find a running NOVA App")
)

// Test seams.
var (
	haSignInOAuthFlow = func() *OAuthLoopbackFlow {
		flow := NewOAuthLoopbackFlow()
		flow.Timeout = haSignInTimeout
		return flow
	}
	// Ctrl+C during sign-in stops it instead of the whole setup.
	haSignInInterrupt = func(ctx context.Context) (context.Context, context.CancelFunc) {
		return signal.NotifyContext(ctx, os.Interrupt)
	}
	// Like the relay client, sign-in never sends tokens through a proxy.
	haSignInHTTPClient = func() *http.Client {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		return cloudNoRedirectHTTPClient(&http.Client{Transport: transport}, 2*time.Minute)
	}()
)

type haSignInSession struct {
	ingress *CloudIngressClient
	user    HACurrentUser
}

// parseLocalHAOrigin accepts the saved Home Assistant URL only as a plain
// origin; every sign-in request goes to exactly this origin.
func parseLocalHAOrigin(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Opaque != "" || parsed.User != nil || parsed.Hostname() == "" ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawPath != "" ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return nil, newCloudError(CloudErrInvalidInput, "validate Home Assistant origin", err)
	}
	if port := parsed.Port(); port != "" {
		if value, err := strconv.Atoi(port); err != nil || value < 1 || value > 65535 {
			return nil, newCloudError(CloudErrInvalidInput, "validate Home Assistant origin", err)
		}
	}
	return &url.URL{Scheme: parsed.Scheme, Host: strings.ToLower(parsed.Host)}, nil
}

// The loopback callback needs a browser on this computer, and the relay must
// serve sign-in, so nobody signs in for nothing.
func defaultHASignInOffered(out io.Writer, cfg runtimeConfig) bool {
	return haSignInPossible(out, cfg) && relayAdvertisesSignIn(cfg.RelayBaseURL)
}

// haSignInPossible: a person at this computer's desktop can finish a browser
// sign-in against the saved local Home Assistant address.
func haSignInPossible(out io.Writer, cfg runtimeConfig) bool {
	if !uiInputSupportsTTY() || !writerSupportsTTY(out) {
		return false
	}
	for _, name := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return false
		}
	}
	if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" &&
		os.Getenv("WAYLAND_DISPLAY") == "" && os.Getenv("WSL_DISTRO_NAME") == "" {
		return false
	}
	_, err := parseLocalHAOrigin(cfg.HAURL)
	return err == nil
}

// relayAdvertisesSignIn reads sign_in from the GET /pair/v1/info capability
// probe; any failure reads as "no" and the step stays the code prompt.
func relayAdvertisesSignIn(relayURL string) bool {
	request, err := http.NewRequest(http.MethodGet, strings.TrimRight(relayURL, "/")+"/pair/v1/info", nil)
	if err != nil {
		return false
	}
	request.URL.User = nil
	response, err := httpClient.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	body, err := readAllLimited(response.Body, maxRelayPairingResponseBytes)
	var info struct {
		OK   bool `json:"ok"`
		Data struct {
			SignIn bool `json:"sign_in"`
		} `json:"data"`
	}
	return response.StatusCode == http.StatusOK && err == nil &&
		json.Unmarshal(body, &info) == nil && info.OK && info.Data.SignIn
}

// resolveHASignInOrigin proves, before any browser opens, that this computer
// reaches the public login API (trusted TLS) and returns the origin sign-in
// must use. Under Supervisor, Home Assistant 2026.10+ serves port 80 and
// answers the legacy port 8123 with a 307 to the same URL on the new port
// (core http/server.py); exactly one such same-host redirect is followed.
func resolveHASignInOrigin(ctx context.Context, haURL string) (*url.URL, error) {
	origin, err := parseLocalHAOrigin(haURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	location, err := probeHAAuthProviders(ctx, origin)
	if err != nil || location == nil {
		return origin, err
	}
	rejected := newCloudError(CloudErrRedirectRejected, "reach Home Assistant", nil)
	if !strings.EqualFold(location.Hostname(), origin.Hostname()) || location.Path != "/auth/providers" {
		return nil, rejected
	}
	moved, err := parseLocalHAOrigin(location.Scheme + "://" + location.Host)
	if err != nil {
		return nil, rejected
	}
	if again, err := probeHAAuthProviders(ctx, moved); err != nil || again != nil {
		if err == nil {
			err = rejected
		}
		return nil, err
	}
	return moved, nil
}

// probeHAAuthProviders returns nil on 200 or the redirect target.
func probeHAAuthProviders(ctx context.Context, origin *url.URL) (*url.URL, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, origin.String()+"/auth/providers", nil)
	if err != nil {
		return nil, newCloudError(CloudErrInvalidInput, "reach Home Assistant", err)
	}
	response, err := haSignInHTTPClient.Do(request)
	if err != nil {
		return nil, cloudRequestError("reach Home Assistant", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	switch {
	case isHTTPRedirect(response.StatusCode):
		location, err := response.Location()
		if err != nil {
			return nil, newCloudHTTPError(CloudErrRedirectRejected, "reach Home Assistant", response.StatusCode, false)
		}
		return location, nil
	case response.StatusCode != http.StatusOK:
		return nil, newCloudHTTPError(CloudErrHAProtocol, "reach Home Assistant", response.StatusCode, false)
	}
	return nil, nil
}

// haSignInPrepare runs while the owner's sign-in is still open, after the
// owner check and before the ingress session; setup uses it to install the
// NOVA App (ha_supervisor_app.go). It gets its own time budget.
type haSignInPrepare func(ctx context.Context, socket haWSCaller, user HACurrentUser) error

func openHASignInIngress(ctx context.Context, out io.Writer, haURL string) (haSignInSession, error) {
	return openHASignInIngressWith(ctx, out, haURL, nil)
}

func openHASignInIngressWith(
	ctx context.Context,
	out io.Writer,
	haURL string,
	prepare haSignInPrepare,
) (haSignInSession, error) {
	origin, err := parseLocalHAOrigin(haURL)
	if err != nil {
		return haSignInSession{}, err
	}
	ctx, stop := haSignInInterrupt(ctx)
	defer stop()
	flow := haSignInOAuthFlow()
	waiting := "Waiting for you to sign in, Ctrl+C to use a code instead"
	if prepare != nil {
		waiting = "Waiting for you to sign in, Ctrl+C to stop"
	}
	authorization, err := runSetupCountdownSpinnerWithResult(
		out,
		waiting,
		flow.Timeout,
		500*time.Millisecond,
		func() (OAuthAuthorizationCode, error) {
			return flow.AuthorizeOrigin(ctx, origin, func(_ context.Context, target string) error {
				openHASignInBrowser(out, target)
				return nil
			})
		},
	)
	armSetupNextPromptSkipsStaleBlankInput()
	if err != nil {
		if ctx.Err() != nil {
			return haSignInSession{}, errors.Join(errSignInStopped, err)
		}
		return haSignInSession{}, errors.Join(errSignInBrowser, err)
	}
	finish := func() (haSignInResult, error) { return finishHASignIn(ctx, origin, authorization, prepare) }
	var result haSignInResult
	if prepare == nil {
		result, err = runSetupSpinnerWithResult(out, "Checking your sign-in with Home Assistant...", setupSpinnerDebounce, finish)
	} else {
		// prepare shows its own progress; a spinner around it would garble it.
		fmt.Fprintln(out, "  Checking your sign-in with Home Assistant...")
		result, err = finish()
	}
	if result.sessionMayRemain {
		renderSetupWarningLine(out,
			"Home Assistant may still list this sign-in (%s). You can remove it under Profile → Security → Refresh tokens: %s",
			authorization.ClientID, haProfileSecurityURL(haURL))
	}
	if ctx.Err() != nil {
		// Ctrl+C wins even when the steps finished meanwhile.
		return haSignInSession{user: result.session.user}, errors.Join(errSignInStopped, context.Canceled)
	}
	return result.session, err
}

type haSignInResult struct {
	session          haSignInSession
	sessionMayRemain bool
}

func finishHASignIn(
	parent context.Context,
	origin *url.URL,
	authorization OAuthAuthorizationCode,
	prepare haSignInPrepare,
) (result haSignInResult, err error) {
	ctx, cancel := context.WithTimeout(parent, haSignInStepTimeout)
	defer cancel()
	oauth := newHAOAuthClientForOrigin(origin, haSignInHTTPClient)
	token, err := oauth.ExchangeAuthorizationCode(ctx, authorization)
	if err != nil {
		// Home Assistant may have created the session without answering.
		result.sessionMayRemain = IsCloudErrorCode(err, CloudErrOAuthOutcomeUnknown)
		return result, err
	}
	// Revoked on every path once a token exists; revocation also ends the
	// access token, so Home Assistant keeps no HA NOVA session.
	defer func() {
		revokeCtx, cancelRevoke := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancelRevoke()
		result.sessionMayRemain = revokeAndVerifyCloudAuthorizationWithClient(
			revokeCtx, oauth, token.RefreshToken, authorization.ClientID) != nil
	}()

	socket, err := dialHAWebSocketForOrigin(ctx, origin, token.AccessToken,
		HAWebSocketClientOptions{HTTPClient: haSignInHTTPClient})
	if err != nil {
		return result, err
	}
	defer socket.Close()
	user, err := socket.CurrentUser(ctx)
	if err != nil {
		return result, err
	}
	result.session.user = user
	if !user.IsOwner {
		return result, errSignInNotOwner
	}
	if prepare != nil {
		prepareCtx, cancelPrepare := context.WithTimeout(parent, haAppInstallTimeout)
		err := prepare(prepareCtx, socket, user)
		cancelPrepare()
		if err != nil {
			return result, err
		}
		var cancelAfter context.CancelFunc
		ctx, cancelAfter = context.WithTimeout(parent, haSignInStepTimeout)
		defer cancelAfter()
	}
	app, err := socket.NOVAAppInfo(ctx)
	if err != nil {
		return result, errors.Join(errSignInAppMissing, err)
	}
	root, err := app.MachineIngressRoot()
	if err != nil {
		return result, errors.Join(errSignInAppMissing, err)
	}
	session, err := socket.CreateIngressSession(ctx)
	if err != nil {
		return result, err
	}
	ingress, err := newCloudIngressClientForOrigin(origin, root, session.Session, haSignInHTTPClient)
	if err != nil {
		return result, err
	}
	result.session.ingress = ingress
	return result, nil
}

// A launched browser command does not prove a browser appeared, so the link
// is always shown; it carries only the one-time state, no credential.
func openHASignInBrowser(out io.Writer, target string) {
	fmt.Fprintln(out, "  Opening Home Assistant in your browser...")
	_ = openBrowserForSetup(target)
	renderSetupLink(out, "If no browser opened, open this link on this computer:", target)
}
