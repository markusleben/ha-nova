package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync/atomic"
	"time"
)

const (
	oauthLoopbackClientPath   = "/ha-nova"
	oauthLoopbackCallbackPath = "/oauth/callback"
	oauthLoopbackMaxHeader    = 8 << 10
)

type OAuthBrowserOpener func(context.Context, string) error

type OAuthAuthorizationCode struct {
	Code        string
	ClientID    string
	RedirectURI string
}

type OAuthLoopbackPreparation struct {
	CanonicalOrigin string
	ClientID        string
	RedirectURI     string
}

type OAuthLoopbackPrepareHook func(context.Context, OAuthLoopbackPreparation) error

type OAuthLoopbackFlow struct {
	Listen        func(network, address string) (net.Listener, error)
	Random        io.Reader
	Timeout       time.Duration
	BeforeBrowser OAuthLoopbackPrepareHook
}

func NewOAuthLoopbackFlow() *OAuthLoopbackFlow {
	return &OAuthLoopbackFlow{
		Listen:  net.Listen,
		Random:  rand.Reader,
		Timeout: 10 * time.Minute,
	}
}

func (f *OAuthLoopbackFlow) Authorize(
	ctx context.Context,
	canonicalOrigin string,
	openBrowser OAuthBrowserOpener,
) (OAuthAuthorizationCode, error) {
	origin, err := ParseCanonicalNabuOrigin(canonicalOrigin)
	if err != nil {
		return OAuthAuthorizationCode{}, err
	}
	if f == nil || f.Listen == nil || f.Random == nil || openBrowser == nil {
		return OAuthAuthorizationCode{}, newCloudError(CloudErrInvalidInput, "start OAuth authorization", nil)
	}
	timeout := f.Timeout
	if timeout <= 0 || timeout > 10*time.Minute {
		timeout = 10 * time.Minute
	}
	authorizationCtx, cancelAuthorization := context.WithTimeout(ctx, timeout)
	defer cancelAuthorization()

	listener, err := f.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return OAuthAuthorizationCode{}, newCloudError(CloudErrNetwork, "bind OAuth callback", err)
	}
	defer listener.Close()
	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok || !tcpAddr.IP.Equal(net.ParseIP("127.0.0.1")) || tcpAddr.Port < 1 {
		return OAuthAuthorizationCode{}, newCloudError(CloudErrOAuthProtocol, "bind OAuth callback", nil)
	}

	host := net.JoinHostPort("127.0.0.1", strconv.Itoa(tcpAddr.Port))
	clientID := "http://" + host + oauthLoopbackClientPath
	redirectURI := "http://" + host + oauthLoopbackCallbackPath
	if err := ValidateOAuthLoopbackClientID(clientID); err != nil {
		return OAuthAuthorizationCode{}, err
	}
	stateBytes := make([]byte, 32)
	if _, err := io.ReadFull(f.Random, stateBytes); err != nil {
		return OAuthAuthorizationCode{}, newCloudError(CloudErrOAuthProtocol, "create OAuth state", err)
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	authorizationURL := buildHAAuthorizationURL(origin, clientID, redirectURI, state)

	type callbackResult struct {
		code string
		err  error
	}
	resultCh := make(chan callbackResult, 1)
	var completed atomic.Bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setOAuthCallbackHeaders(w.Header())
		switch {
		case r.Host != host:
			http.Error(w, "Invalid callback host.", http.StatusBadRequest)
		case len(r.RequestURI) > oauthLoopbackMaxHeader:
			http.Error(w, "Authorization callback is too large.", http.StatusRequestURITooLong)
		case r.URL.Path != oauthLoopbackCallbackPath || r.URL.RawPath != "":
			http.NotFound(w, r)
		case r.Method != http.MethodGet:
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
		case r.ContentLength != 0 || len(r.TransferEncoding) != 0:
			http.Error(w, "Authorization callback must not contain a body.", http.StatusBadRequest)
		case completed.Load():
			http.Error(w, "Authorization callback already handled.", http.StatusGone)
		default:
			query, queryErr := url.ParseQuery(r.URL.RawQuery)
			result, valid := validateOAuthCallback(query, state)
			if queryErr != nil || !valid {
				http.Error(w, "Invalid authorization callback.", http.StatusBadRequest)
				return
			}
			if !completed.CompareAndSwap(false, true) {
				http.Error(w, "Authorization callback already handled.", http.StatusGone)
				return
			}
			resultCh <- result
			if result.err != nil {
				http.Error(w, "Authorization was not completed.", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "<!doctype html><html><head><meta charset=\"utf-8\"><title>HA NOVA</title></head><body><p>Authorization complete. You can close this tab.</p></body></html>")
		}
	})
	server := &http.Server{
		Handler:           handler,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       5 * time.Second,
		MaxHeaderBytes:    oauthLoopbackMaxHeader,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	serveErrCh := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			serveErrCh <- err
		}
	}()
	if err := authorizationCtx.Err(); err != nil {
		shutdownOAuthCallbackServer(server)
		return OAuthAuthorizationCode{}, newCloudError(
			CloudErrTimeout,
			"prepare OAuth authorization",
			err,
		)
	}
	if f.BeforeBrowser != nil {
		err := f.BeforeBrowser(authorizationCtx, OAuthLoopbackPreparation{
			CanonicalOrigin: origin.String(),
			ClientID:        clientID,
			RedirectURI:     redirectURI,
		})
		if err != nil {
			shutdownOAuthCallbackServer(server)
			if authorizationCtx.Err() != nil {
				return OAuthAuthorizationCode{}, newCloudError(
					CloudErrTimeout,
					"prepare OAuth authorization",
					authorizationCtx.Err(),
				)
			}
			return OAuthAuthorizationCode{}, err
		}
	}
	if err := authorizationCtx.Err(); err != nil {
		shutdownOAuthCallbackServer(server)
		return OAuthAuthorizationCode{}, newCloudError(
			CloudErrTimeout,
			"prepare OAuth authorization",
			err,
		)
	}
	if err := openBrowser(authorizationCtx, authorizationURL); err != nil {
		shutdownOAuthCallbackServer(server)
		if authorizationCtx.Err() != nil {
			return OAuthAuthorizationCode{}, newCloudError(
				CloudErrTimeout,
				"open OAuth authorization",
				authorizationCtx.Err(),
			)
		}
		return OAuthAuthorizationCode{}, newCloudError(CloudErrOAuthProtocol, "open OAuth authorization", err)
	}

	select {
	case result := <-resultCh:
		shutdownOAuthCallbackServer(server)
		if result.err != nil {
			return OAuthAuthorizationCode{}, result.err
		}
		return OAuthAuthorizationCode{
			Code:        result.code,
			ClientID:    clientID,
			RedirectURI: redirectURI,
		}, nil
	case err := <-serveErrCh:
		return OAuthAuthorizationCode{}, newCloudError(CloudErrNetwork, "serve OAuth callback", err)
	case <-authorizationCtx.Done():
		shutdownOAuthCallbackServer(server)
		return OAuthAuthorizationCode{}, newCloudError(
			CloudErrTimeout,
			"wait for OAuth callback",
			authorizationCtx.Err(),
		)
	}
}

func ValidateOAuthLoopbackClientID(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.Opaque != "" ||
		parsed.User != nil || parsed.Hostname() != "127.0.0.1" ||
		parsed.Port() == "" || parsed.Path != oauthLoopbackClientPath ||
		parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery ||
		parsed.Fragment != "" {
		return newCloudError(CloudErrInvalidInput, "validate OAuth client ID", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 ||
		parsed.Host != net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) {
		return newCloudError(CloudErrInvalidInput, "validate OAuth client ID", err)
	}
	return nil
}

func buildHAAuthorizationURL(origin *url.URL, clientID, redirectURI, state string) string {
	target := *origin
	target.Path = "/auth/authorize"
	query := make(url.Values, 4)
	query.Set("response_type", "code")
	query.Set("client_id", clientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("state", state)
	target.RawQuery = query.Encode()
	return target.String()
}

func validateOAuthCallback(query url.Values, expectedState string) (struct {
	code string
	err  error
}, bool) {
	result := struct {
		code string
		err  error
	}{}
	for key, values := range query {
		if key != "code" && key != "state" && key != "error" && key != "error_description" {
			return result, false
		}
		if len(values) != 1 {
			return result, false
		}
	}
	states, ok := query["state"]
	if !ok || len(states) != 1 || states[0] != expectedState {
		return result, false
	}
	if oauthErrors, ok := query["error"]; ok {
		if _, hasCode := query["code"]; hasCode || oauthErrors[0] == "" {
			return result, false
		}
		code := CloudErrOAuthRejected
		if oauthErrors[0] == "access_denied" {
			code = CloudErrOAuthCanceled
		}
		result.err = newCloudError(code, "complete OAuth authorization", nil)
		return result, true
	}
	codes, ok := query["code"]
	if !ok || len(codes) != 1 || !validSecretText(codes[0], 4096) {
		return result, false
	}
	if _, hasDescription := query["error_description"]; hasDescription {
		return result, false
	}
	result.code = codes[0]
	return result, true
}

func setOAuthCallbackHeaders(header http.Header) {
	header.Set("Cache-Control", "no-store")
	header.Set("Pragma", "no-cache")
	header.Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
	header.Set("Cross-Origin-Opener-Policy", "same-origin")
	header.Set("Content-Type", "text/html; charset=utf-8")
}

func shutdownOAuthCallbackServer(server *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}
