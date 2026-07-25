package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOAuthLoopbackFlowUsesBoundExactLoopbackClient(t *testing.T) {
	flow := NewOAuthLoopbackFlow()
	flow.Timeout = 2 * time.Second
	var callbackBody string

	authorization, err := flow.Authorize(
		context.Background(),
		"https://unit.ui.nabu.casa",
		func(ctx context.Context, authorizationURL string) error {
			parsed, err := url.Parse(authorizationURL)
			if err != nil {
				return err
			}
			if parsed.Scheme != "https" || parsed.Host != "unit.ui.nabu.casa" ||
				parsed.Path != "/auth/authorize" {
				t.Errorf("authorization endpoint = %s", parsed.Redacted())
			}
			query := parsed.Query()
			clientID := query.Get("client_id")
			redirectURI := query.Get("redirect_uri")
			if err := ValidateOAuthLoopbackClientID(clientID); err != nil {
				t.Errorf("client_id: %v", err)
			}
			if strings.HasPrefix(clientID, "https://unit.ui.nabu.casa") {
				t.Error("Nabu origin was used as OAuth client_id")
			}
			if !validOAuthRedirectURI(redirectURI, clientID) {
				t.Error("redirect URI does not share the bound loopback origin")
			}
			callback, _ := url.Parse(redirectURI)
			values := callback.Query()
			values.Set("state", query.Get("state"))
			values.Set("code", "single-use-code")
			callback.RawQuery = values.Encode()
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, callback.String(), nil)
			response, err := (&http.Client{Timeout: time.Second}).Do(request)
			if err != nil {
				return err
			}
			defer response.Body.Close()
			body, _ := io.ReadAll(response.Body)
			callbackBody = string(body)
			if response.StatusCode != http.StatusOK {
				t.Errorf("callback status = %d", response.StatusCode)
			}
			for header, want := range map[string]string{
				"Cache-Control":          "no-store",
				"Referrer-Policy":        "no-referrer",
				"X-Content-Type-Options": "nosniff",
				"X-Frame-Options":        "DENY",
			} {
				if got := response.Header.Get(header); got != want {
					t.Errorf("%s = %q, want %q", header, got, want)
				}
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if authorization.Code != "single-use-code" ||
		!validOAuthRedirectURI(authorization.RedirectURI, authorization.ClientID) {
		t.Fatalf("authorization = %+v", authorization)
	}
	if strings.Contains(callbackBody, authorization.Code) {
		t.Fatal("callback page exposed the authorization code")
	}
}

func TestOAuthLoopbackFlowRejectsSpoofedCallbacksWithoutEndingFlow(t *testing.T) {
	flow := NewOAuthLoopbackFlow()
	flow.Timeout = 2 * time.Second
	client := &http.Client{Timeout: time.Second}

	authorization, err := flow.Authorize(
		context.Background(),
		"https://unit.ui.nabu.casa",
		func(ctx context.Context, authorizationURL string) error {
			parsed, _ := url.Parse(authorizationURL)
			query := parsed.Query()
			callback := query.Get("redirect_uri")
			state := query.Get("state")

			check := func(method, target, host string, want int) {
				t.Helper()
				request, _ := http.NewRequestWithContext(ctx, method, target, nil)
				if host != "" {
					request.Host = host
				}
				response, err := client.Do(request)
				if err != nil {
					t.Fatalf("callback request: %v", err)
				}
				defer response.Body.Close()
				if response.StatusCode != want {
					t.Fatalf("%s callback status = %d, want %d", method, response.StatusCode, want)
				}
			}

			check(http.MethodGet, callback+"?state=wrong&code=attacker", "", http.StatusBadRequest)
			check(http.MethodPost, callback+"?state="+url.QueryEscape(state)+"&code=attacker", "", http.StatusMethodNotAllowed)
			check(http.MethodGet, callback+"?state="+url.QueryEscape(state)+"&state="+url.QueryEscape(state)+"&code=attacker", "", http.StatusBadRequest)
			check(http.MethodGet, callback+"?state="+url.QueryEscape(state)+"&code=attacker&bad=%zz", "", http.StatusBadRequest)
			check(http.MethodGet, callback+"?state="+url.QueryEscape(state)+"&code=attacker", "evil.invalid", http.StatusBadRequest)
			check(
				http.MethodGet,
				callback+"?"+strings.Repeat("padding=x&", 1024),
				"",
				http.StatusRequestURITooLong,
			)
			withBody, _ := http.NewRequestWithContext(
				ctx,
				http.MethodGet,
				callback+"?state="+url.QueryEscape(state)+"&code=attacker",
				strings.NewReader("unexpected"),
			)
			withBodyResponse, bodyErr := client.Do(withBody)
			if bodyErr != nil {
				t.Fatalf("callback with body: %v", bodyErr)
			}
			_ = withBodyResponse.Body.Close()
			if withBodyResponse.StatusCode != http.StatusBadRequest {
				t.Fatalf(
					"callback with body status = %d",
					withBodyResponse.StatusCode,
				)
			}
			check(http.MethodGet, callback+"?state="+url.QueryEscape(state)+"&code=valid-code", "", http.StatusOK)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if authorization.Code != "valid-code" {
		t.Fatalf("code = %q", authorization.Code)
	}
}

func TestOAuthLoopbackPreparedHookRunsAfterBindBeforeBrowser(t *testing.T) {
	flow := NewOAuthLoopbackFlow()
	flow.Timeout = 2 * time.Second
	order := make([]string, 0, 2)
	var prepared OAuthLoopbackPreparation
	flow.BeforeBrowser = func(
		ctx context.Context,
		value OAuthLoopbackPreparation,
	) error {
		order = append(order, "prepared")
		prepared = value
		if value.CanonicalOrigin != "https://unit.ui.nabu.casa" ||
			ValidateOAuthLoopbackClientID(value.ClientID) != nil ||
			!validOAuthRedirectURI(value.RedirectURI, value.ClientID) {
			t.Fatalf("preparation = %+v", value)
		}
		request, _ := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			value.RedirectURI+"?state=wrong&code=probe",
			nil,
		)
		response, err := (&http.Client{Timeout: time.Second}).Do(request)
		if err != nil {
			t.Fatalf("listener was not serving before prepared hook: %v", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("prepared-hook probe status = %d", response.StatusCode)
		}
		return nil
	}

	authorization, err := flow.Authorize(
		context.Background(),
		"https://unit.ui.nabu.casa",
		func(ctx context.Context, authorizationURL string) error {
			order = append(order, "browser")
			parsed, _ := url.Parse(authorizationURL)
			query := parsed.Query()
			callback, _ := url.Parse(query.Get("redirect_uri"))
			callbackQuery := callback.Query()
			callbackQuery.Set("state", query.Get("state"))
			callbackQuery.Set("code", "prepared-code")
			callback.RawQuery = callbackQuery.Encode()
			response, requestErr := (&http.Client{Timeout: time.Second}).Get(callback.String())
			if requestErr != nil {
				return requestErr
			}
			_ = response.Body.Close()
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "prepared,browser" ||
		authorization.ClientID != prepared.ClientID ||
		authorization.RedirectURI != prepared.RedirectURI {
		t.Fatalf("order=%v preparation=%+v authorization=%+v", order, prepared, authorization)
	}
}

func TestOAuthLoopbackPreparedHookFailurePreventsBrowser(t *testing.T) {
	flow := NewOAuthLoopbackFlow()
	hookErr := errors.New("checkpoint failed")
	flow.BeforeBrowser = func(context.Context, OAuthLoopbackPreparation) error {
		return hookErr
	}
	browserCalled := false
	_, err := flow.Authorize(
		context.Background(),
		"https://unit.ui.nabu.casa",
		func(context.Context, string) error {
			browserCalled = true
			return nil
		},
	)
	if !errors.Is(err, hookErr) || browserCalled {
		t.Fatalf("hook result: browserCalled=%v err=%v", browserCalled, err)
	}
}

func TestOAuthLoopbackFlowTimeoutAndCancellationAreTyped(t *testing.T) {
	flow := NewOAuthLoopbackFlow()
	flow.Timeout = 20 * time.Millisecond
	_, err := flow.Authorize(
		context.Background(),
		"https://unit.ui.nabu.casa",
		func(context.Context, string) error { return nil },
	)
	if !IsCloudErrorCode(err, CloudErrTimeout) {
		t.Fatalf("timeout error = %v", err)
	}

	openerErr := errors.New("no browser")
	_, err = NewOAuthLoopbackFlow().Authorize(
		context.Background(),
		"https://unit.ui.nabu.casa",
		func(context.Context, string) error { return openerErr },
	)
	if !errors.Is(err, openerErr) || !IsCloudErrorCode(err, CloudErrOAuthProtocol) {
		t.Fatalf("opener error = %v", err)
	}
}

func TestOAuthLoopbackTimeoutIncludesPreBrowserCheckpoint(t *testing.T) {
	flow := NewOAuthLoopbackFlow()
	flow.Timeout = 20 * time.Millisecond
	flow.BeforeBrowser = func(
		ctx context.Context,
		_ OAuthLoopbackPreparation,
	) error {
		<-ctx.Done()
		return ctx.Err()
	}
	browserCalled := false
	_, err := flow.Authorize(
		context.Background(),
		"https://unit.ui.nabu.casa",
		func(context.Context, string) error {
			browserCalled = true
			return nil
		},
	)
	if !IsCloudErrorCode(err, CloudErrTimeout) || browserCalled {
		t.Fatalf("checkpoint timeout: browserCalled=%v err=%v", browserCalled, err)
	}
}

func TestValidateOAuthLoopbackClientIDRejectsUnsafeVariants(t *testing.T) {
	for _, candidate := range []string{
		"https://127.0.0.1:1234/ha-nova",
		"http://localhost:1234/ha-nova",
		"http://127.0.0.1/ha-nova",
		"http://127.0.0.1:1234/oauth/callback",
		"http://127.0.0.1:1234/ha-nova?token=x",
		"http://127.0.0.1:1234/ha-nova?",
		"http://user@127.0.0.1:1234/ha-nova",
	} {
		if err := ValidateOAuthLoopbackClientID(candidate); err == nil {
			t.Errorf("accepted unsafe client ID %q", candidate)
		}
	}
}

func TestOAuthRedirectURIRejectsEmptyQueryMarker(t *testing.T) {
	if validOAuthRedirectURI(
		"http://127.0.0.1:1234/oauth/callback?",
		"http://127.0.0.1:1234/ha-nova",
	) {
		t.Fatal("accepted a non-exact callback URI")
	}
}
