package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func TestHAOAuthExchangeClassifiesPossiblyIssuedSessionAsOutcomeUnknown(t *testing.T) {
	for name, roundTrip := range map[string]roundTripFunc{
		"response lost": func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection dropped after write")
		},
		"server failed": func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":"server_error"}`)),
			}, nil
		},
		"server status with denial body": func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_grant"}`)),
			}, nil
		},
		"informational status with denial body": func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_grant"}`)),
			}, nil
		},
		"malformed success": func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(
					`{"access_token":"access-secret","refresh_token":"must-not-leak"}`,
				)),
			}, nil
		},
		"malformed client error": func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":`)),
			}, nil
		},
		"missing client error code": func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"message":"rejected"}`)),
			}, nil
		},
		"unknown client error": func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":"proxy_rejected"}`)),
			}, nil
		},
		"duplicate client error code": func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(
					`{"error":"server_error","error":"invalid_grant"}`,
				)),
			}, nil
		},
		"client error body lost": func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     make(http.Header),
				Body: io.NopCloser(iotest.ErrReader(
					errors.New("response body dropped"),
				)),
			}, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			client, err := NewHAOAuthClient(
				"https://unit.ui.nabu.casa",
				&http.Client{Transport: roundTrip},
			)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ExchangeAuthorizationCode(
				context.Background(),
				OAuthAuthorizationCode{
					Code:        "authorization-secret",
					ClientID:    "http://127.0.0.1:43123/ha-nova",
					RedirectURI: "http://127.0.0.1:43123/oauth/callback",
				},
			)
			if !IsCloudErrorCode(err, CloudErrOAuthOutcomeUnknown) {
				t.Fatalf("error = %v", err)
			}
			if strings.Contains(err.Error(), "must-not-leak") ||
				strings.Contains(err.Error(), "authorization-secret") {
				t.Fatalf("outcome error leaked a secret: %v", err)
			}
		})
	}
}

func TestHAOAuthExchangeAcceptsOnlyStrictDefinitiveRejections(t *testing.T) {
	tests := []struct {
		name string
		body string
		code CloudErrorCode
	}{
		{
			name: "invalid grant",
			body: `{"error":"invalid_grant","error_description":"code expired"}`,
			code: CloudErrOAuthInvalidGrant,
		},
		{
			name: "invalid request",
			body: `{"error":"invalid_request"}`,
			code: CloudErrOAuthRejected,
		},
		{
			name: "access denied",
			body: `{"error":"access_denied"}`,
			code: CloudErrForbidden,
		},
		{
			name: "unauthorized client",
			body: `{"error":"unauthorized_client"}`,
			code: CloudErrOAuthRejected,
		},
		{
			name: "unsupported grant type",
			body: `{"error":"unsupported_grant_type"}`,
			code: CloudErrOAuthRejected,
		},
		{
			name: "invalid scope",
			body: `{"error":"invalid_scope"}`,
			code: CloudErrOAuthRejected,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewHAOAuthClient(
				"https://unit.ui.nabu.casa",
				&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusBadRequest,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader(test.body)),
					}, nil
				})},
			)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ExchangeAuthorizationCode(
				context.Background(),
				OAuthAuthorizationCode{
					Code:        "authorization-secret",
					ClientID:    "http://127.0.0.1:43123/ha-nova",
					RedirectURI: "http://127.0.0.1:43123/oauth/callback",
				},
			)
			if !IsCloudErrorCode(err, test.code) {
				t.Fatalf("error = %v, want %s", err, test.code)
			}
		})
	}
}

func TestHAOAuthExchangeClassifiesRedirectAsUnknownWithoutFollowing(t *testing.T) {
	requests := 0
	client, err := NewHAOAuthClient(
		"https://unit.ui.nabu.casa",
		&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			if request.URL.String() != "https://unit.ui.nabu.casa/auth/token" {
				t.Fatalf("OAuth redirect was followed to %s", request.URL.Redacted())
			}
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": []string{"https://evil.invalid/steal"}},
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		})},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ExchangeAuthorizationCode(
		context.Background(),
		OAuthAuthorizationCode{
			Code:        "authorization-secret",
			ClientID:    "http://127.0.0.1:43123/ha-nova",
			RedirectURI: "http://127.0.0.1:43123/oauth/callback",
		},
	)
	if !IsCloudErrorCode(err, CloudErrOAuthOutcomeUnknown) {
		t.Fatalf("error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestHAOAuthExchangeKeepsSecretsOutOfURLHeadersAndCookies(t *testing.T) {
	var captured *http.Request
	client, err := NewHAOAuthClient("https://unit.ui.nabu.casa", &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			captured = request.Clone(request.Context())
			body, _ := io.ReadAll(request.Body)
			captured.Body = io.NopCloser(strings.NewReader(string(body)))
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(
					`{"access_token":"access-secret","refresh_token":"refresh-secret","token_type":"Bearer","expires_in":1800,"ha_auth_provider":"homeassistant"}`,
				)),
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC) }
	token, err := client.ExchangeAuthorizationCode(context.Background(), OAuthAuthorizationCode{
		Code:        "authorization-secret",
		ClientID:    "http://127.0.0.1:43123/ha-nova",
		RedirectURI: "http://127.0.0.1:43123/oauth/callback",
	})
	if err != nil {
		t.Fatalf("ExchangeAuthorizationCode: %v", err)
	}
	if token.AccessToken != "access-secret" || token.RefreshToken != "refresh-secret" ||
		!token.ExpiresAt.Equal(time.Date(2029, 1, 1, 0, 30, 0, 0, time.UTC)) {
		t.Fatalf("token = %+v", token)
	}
	if captured.URL.String() != "https://unit.ui.nabu.casa/auth/token" ||
		captured.Header.Get("Authorization") != "" || len(captured.Cookies()) != 0 {
		t.Fatalf("unsafe request metadata: url=%s headers=%v", captured.URL.Redacted(), captured.Header)
	}
	body, _ := io.ReadAll(captured.Body)
	form, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatal(err)
	}
	if form.Get("code") != "authorization-secret" ||
		form.Get("client_id") != "http://127.0.0.1:43123/ha-nova" ||
		form.Get("redirect_uri") != "http://127.0.0.1:43123/oauth/callback" {
		t.Fatalf("form = %v", form)
	}
}

func TestHAOAuthRefreshClassifiesInvalidGrantWithoutLeakingToken(t *testing.T) {
	client, err := NewHAOAuthClient("https://unit.ui.nabu.casa", &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(request.Body)
			if strings.Contains(request.URL.String(), "very-secret-refresh") ||
				request.Header.Get("Authorization") != "" ||
				!strings.Contains(string(body), "very-secret-refresh") {
				t.Fatalf("refresh token placement is unsafe")
			}
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_grant","error_description":"very-secret-refresh"}`)),
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Refresh(
		context.Background(),
		"very-secret-refresh",
		"http://127.0.0.1:43123/ha-nova",
	)
	if !IsCloudErrorCode(err, CloudErrOAuthInvalidGrant) {
		t.Fatalf("refresh error = %v", err)
	}
	if strings.Contains(err.Error(), "very-secret-refresh") {
		t.Fatal("typed error leaked refresh token")
	}
}

func TestHAOAuthRevokeUsesDedicatedEndpointAndRejectsRedirect(t *testing.T) {
	var requestPath string
	var requestBody string
	client, err := NewHAOAuthClient("https://unit.ui.nabu.casa", &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requestPath = request.URL.Path
			body, _ := io.ReadAll(request.Body)
			requestBody = string(body)
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": []string{"https://evil.invalid/steal"}},
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	err = client.Revoke(context.Background(), "refresh-secret")
	if !IsCloudErrorCode(err, CloudErrRedirectRejected) {
		t.Fatalf("revoke error = %v", err)
	}
	form, _ := url.ParseQuery(requestBody)
	if requestPath != "/auth/revoke" || form.Get("token") != "refresh-secret" ||
		form.Get("action") != "" {
		t.Fatalf("revoke request path=%q form=%v", requestPath, form)
	}
}
