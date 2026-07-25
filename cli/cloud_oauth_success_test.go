package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestOAuthTokenSuccessRejectsEveryDuplicateKnownField(t *testing.T) {
	authorizationBodies := map[string]string{
		"access_token":  `{"access_token":"access-a","access_token":"access-b","refresh_token":"refresh","token_type":"Bearer","expires_in":1800}`,
		"refresh_token": `{"access_token":"access","refresh_token":"refresh-a","refresh_token":"refresh-b","token_type":"Bearer","expires_in":1800}`,
		"token_type":    `{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","token_type":"Bearer","expires_in":1800}`,
		"expires_in":    `{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","expires_in":1800,"expires_in":1800}`,
	}
	refreshBodies := map[string]string{
		"access_token":  `{"access_token":"access-a","access_token":"access-b","token_type":"Bearer","expires_in":1800}`,
		"refresh_token": `{"access_token":"access","refresh_token":"refresh-a","refresh_token":"refresh-b","token_type":"Bearer","expires_in":1800}`,
		"token_type":    `{"access_token":"access","token_type":"Bearer","token_type":"Bearer","expires_in":1800}`,
		"expires_in":    `{"access_token":"access","token_type":"Bearer","expires_in":1800,"expires_in":1800}`,
	}
	for field, body := range authorizationBodies {
		t.Run("authorization_code/"+field, func(t *testing.T) {
			err := runOAuthTokenSuccessFlow(t, body, true)
			if !IsCloudErrorCode(err, CloudErrOAuthOutcomeUnknown) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	for field, body := range refreshBodies {
		t.Run("refresh/"+field, func(t *testing.T) {
			err := runOAuthTokenSuccessFlow(t, body, false)
			if !IsCloudErrorCode(err, CloudErrOAuthProtocol) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestOAuthTokenSuccessRejectsMalformedStructureAndTrailingData(
	t *testing.T,
) {
	for name, body := range map[string]string{
		"array":         `["access","refresh","Bearer",1800]`,
		"incomplete":    `{"access_token":"access"`,
		"wrong type":    `{"access_token":{"secret":"access"},"refresh_token":"refresh","token_type":"Bearer","expires_in":1800}`,
		"trailing data": `{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","expires_in":1800} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := runOAuthTokenSuccessFlow(
				t,
				body,
				true,
			); !IsCloudErrorCode(err, CloudErrOAuthOutcomeUnknown) {
				t.Fatalf("authorization-code error = %v", err)
			}
		})
	}
	for name, body := range map[string]string{
		"array":         `["access","Bearer",1800]`,
		"incomplete":    `{"access_token":"access"`,
		"wrong type":    `{"access_token":{"secret":"access"},"token_type":"Bearer","expires_in":1800}`,
		"trailing data": `{"access_token":"access","token_type":"Bearer","expires_in":1800} {}`,
	} {
		t.Run("refresh/"+name, func(t *testing.T) {
			if err := runOAuthTokenSuccessFlow(
				t,
				body,
				false,
			); !IsCloudErrorCode(err, CloudErrOAuthProtocol) {
				t.Fatalf("refresh error = %v", err)
			}
		})
	}
}

func TestOAuthTokenSuccessAllowsInformationalHomeAssistantFields(t *testing.T) {
	authorizationBody := `{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","expires_in":1800,"ha_auth_provider":"homeassistant","metadata":{"source":"HA"}}`
	if err := runOAuthTokenSuccessFlow(
		t,
		authorizationBody,
		true,
	); err != nil {
		t.Fatalf("authorization-code error = %v", err)
	}
	refreshBody := `{"access_token":"access","token_type":"Bearer","expires_in":"1800","ha_auth_provider":"homeassistant"}`
	if err := runOAuthTokenSuccessFlow(
		t,
		refreshBody,
		false,
	); err != nil {
		t.Fatalf("refresh error = %v", err)
	}
}

func runOAuthTokenSuccessFlow(
	t *testing.T,
	body string,
	authorizationCode bool,
) error {
	t.Helper()
	client, err := NewHAOAuthClient(
		"https://unit.ui.nabu.casa",
		&http.Client{Transport: roundTripFunc(
			func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(body)),
				}, nil
			},
		)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if authorizationCode {
		_, err = client.ExchangeAuthorizationCode(
			context.Background(),
			OAuthAuthorizationCode{
				Code:        "authorization-secret",
				ClientID:    "http://127.0.0.1:43123/ha-nova",
				RedirectURI: "http://127.0.0.1:43123/oauth/callback",
			},
		)
		return err
	}
	_, err = client.Refresh(
		context.Background(),
		"refresh-input",
		"http://127.0.0.1:43123/ha-nova",
	)
	return err
}
