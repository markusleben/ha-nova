package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const oauthMaxResponseBytes = 64 << 10

type HAOAuthToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

type HAOAuthClient struct {
	origin string
	http   *http.Client
	now    func() time.Time
}

func NewHAOAuthClient(canonicalOrigin string, httpClient *http.Client) (*HAOAuthClient, error) {
	origin, err := ParseCanonicalNabuOrigin(canonicalOrigin)
	if err != nil {
		return nil, err
	}
	return &HAOAuthClient{
		origin: origin.String(),
		http:   cloudNoRedirectHTTPClient(httpClient, 15*time.Second),
		now:    time.Now,
	}, nil
}

func (c *HAOAuthClient) ExchangeAuthorizationCode(ctx context.Context, authorization OAuthAuthorizationCode) (HAOAuthToken, error) {
	if err := ValidateOAuthLoopbackClientID(authorization.ClientID); err != nil {
		return HAOAuthToken{}, err
	}
	if !validSecretText(authorization.Code, 4096) ||
		!validOAuthRedirectURI(authorization.RedirectURI, authorization.ClientID) {
		return HAOAuthToken{}, newCloudError(CloudErrInvalidInput, "exchange OAuth authorization code", nil)
	}
	form := make(url.Values, 4)
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", authorization.ClientID)
	form.Set("code", authorization.Code)
	form.Set("redirect_uri", authorization.RedirectURI)
	token, err := c.requestToken(ctx, form, true)
	if err != nil {
		return HAOAuthToken{}, err
	}
	return token, nil
}

func (c *HAOAuthClient) Refresh(ctx context.Context, refreshToken, clientID string) (HAOAuthToken, error) {
	if !validSecretText(refreshToken, 2048) {
		return HAOAuthToken{}, newCloudError(CloudErrInvalidInput, "refresh OAuth token", nil)
	}
	if err := ValidateOAuthLoopbackClientID(clientID); err != nil {
		return HAOAuthToken{}, err
	}
	form := make(url.Values, 3)
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", clientID)
	form.Set("refresh_token", refreshToken)
	return c.requestToken(ctx, form, false)
}

func (c *HAOAuthClient) Revoke(ctx context.Context, refreshToken string) error {
	if !validSecretText(refreshToken, 2048) {
		return newCloudError(CloudErrInvalidInput, "revoke OAuth token", nil)
	}
	form := make(url.Values, 1)
	form.Set("token", refreshToken)
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.origin+"/auth/revoke",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return newCloudError(CloudErrOAuthProtocol, "revoke OAuth token", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return cloudRequestError("revoke OAuth token", err)
	}
	defer response.Body.Close()
	if isHTTPRedirect(response.StatusCode) {
		return newCloudHTTPError(CloudErrRedirectRejected, "revoke OAuth token", response.StatusCode, false)
	}
	if _, err := readCloudResponse(response.Body, oauthMaxResponseBytes, "read OAuth revocation response"); err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return newCloudHTTPError(CloudErrOAuthRejected, "revoke OAuth token", response.StatusCode, response.StatusCode >= 500)
	}
	return nil
}

func (c *HAOAuthClient) requestToken(ctx context.Context, form url.Values, requireRefresh bool) (HAOAuthToken, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.origin+"/auth/token",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return HAOAuthToken{}, newCloudError(CloudErrOAuthProtocol, "request OAuth token", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		if requireRefresh {
			return HAOAuthToken{}, newCloudError(
				CloudErrOAuthOutcomeUnknown,
				"exchange OAuth authorization code",
				cloudRequestError("request OAuth token", err),
			)
		}
		return HAOAuthToken{}, cloudRequestError("request OAuth token", err)
	}
	defer response.Body.Close()
	if isHTTPRedirect(response.StatusCode) {
		if requireRefresh {
			return HAOAuthToken{}, newCloudHTTPError(
				CloudErrOAuthOutcomeUnknown,
				"exchange OAuth authorization code",
				response.StatusCode,
				false,
			)
		}
		return HAOAuthToken{}, newCloudHTTPError(CloudErrRedirectRejected, "request OAuth token", response.StatusCode, false)
	}
	data, err := readCloudResponse(response.Body, oauthMaxResponseBytes, "read OAuth token response")
	if err != nil {
		if requireRefresh {
			return HAOAuthToken{}, newCloudError(
				CloudErrOAuthOutcomeUnknown,
				"exchange OAuth authorization code",
				err,
			)
		}
		return HAOAuthToken{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		rejection, rejectionErr := decodeOAuthTokenRejection(data)
		if requireRefresh {
			if response.StatusCode < http.StatusBadRequest ||
				response.StatusCode >= http.StatusInternalServerError {
				return HAOAuthToken{}, newCloudHTTPError(
					CloudErrOAuthOutcomeUnknown,
					"exchange OAuth authorization code",
					response.StatusCode,
					false,
				)
			}
			if rejectionErr != nil {
				return HAOAuthToken{}, newCloudError(
					CloudErrOAuthOutcomeUnknown,
					"exchange OAuth authorization code",
					rejectionErr,
				)
			}
			switch rejection {
			case "invalid_grant":
				return HAOAuthToken{}, newCloudHTTPError(CloudErrOAuthInvalidGrant, "request OAuth token", response.StatusCode, false)
			case "access_denied":
				return HAOAuthToken{}, newCloudHTTPError(CloudErrForbidden, "request OAuth token", response.StatusCode, false)
			case "invalid_request", "unauthorized_client",
				"unsupported_grant_type", "invalid_scope":
				return HAOAuthToken{}, newCloudHTTPError(CloudErrOAuthRejected, "request OAuth token", response.StatusCode, false)
			default:
				return HAOAuthToken{}, newCloudHTTPError(
					CloudErrOAuthOutcomeUnknown,
					"exchange OAuth authorization code",
					response.StatusCode,
					false,
				)
			}
		}
		switch rejection {
		case "invalid_grant":
			return HAOAuthToken{}, newCloudHTTPError(CloudErrOAuthInvalidGrant, "request OAuth token", response.StatusCode, false)
		case "access_denied":
			return HAOAuthToken{}, newCloudHTTPError(CloudErrForbidden, "request OAuth token", response.StatusCode, false)
		default:
			return HAOAuthToken{}, newCloudHTTPError(CloudErrOAuthRejected, "request OAuth token", response.StatusCode, response.StatusCode >= 500)
		}
	}

	payload, err := decodeOAuthTokenSuccess(data)
	if err == nil &&
		(!validSecretText(payload.AccessToken, 8192) ||
			!strings.EqualFold(payload.TokenType, "Bearer") ||
			(requireRefresh &&
				!validSecretText(payload.RefreshToken, 2048)) ||
			(!requireRefresh && payload.RefreshToken != "")) {
		err = errors.New("OAuth token response has invalid fields")
	}
	if err != nil {
		if requireRefresh {
			return HAOAuthToken{}, newCloudError(
				CloudErrOAuthOutcomeUnknown,
				"exchange OAuth authorization code",
				err,
			)
		}
		return HAOAuthToken{}, newCloudError(
			CloudErrOAuthProtocol,
			"decode OAuth token response",
			err,
		)
	}
	expiresIn, err := parseOAuthExpiresIn(payload.ExpiresIn)
	if err != nil {
		if requireRefresh {
			return HAOAuthToken{}, newCloudError(
				CloudErrOAuthOutcomeUnknown,
				"exchange OAuth authorization code",
				err,
			)
		}
		return HAOAuthToken{}, err
	}
	return HAOAuthToken{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		ExpiresAt:    c.now().UTC().Add(expiresIn),
	}, nil
}

func decodeOAuthTokenRejection(data []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return "", errors.New("OAuth rejection is not a JSON object")
	}
	seen := make(map[string]bool, 3)
	errorCode := ""
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return "", err
		}
		key, ok := keyToken.(string)
		if !ok || seen[key] {
			return "", errors.New("OAuth rejection has duplicate fields")
		}
		seen[key] = true
		switch key {
		case "error", "error_description", "error_uri":
			var value string
			if err := decoder.Decode(&value); err != nil {
				return "", err
			}
			if key == "error" {
				errorCode = value
			}
		default:
			return "", errors.New("OAuth rejection has unknown fields")
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return "", errors.New("OAuth rejection JSON object is incomplete")
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return "", err
	}
	if !validSecretText(errorCode, 128) {
		return "", errors.New("OAuth rejection is missing a valid error code")
	}
	return errorCode, nil
}

func parseOAuthExpiresIn(raw json.RawMessage) (time.Duration, error) {
	var seconds int64
	if err := json.Unmarshal(raw, &seconds); err != nil {
		var text string
		if stringErr := json.Unmarshal(raw, &text); stringErr != nil {
			return 0, newCloudError(CloudErrOAuthProtocol, "decode OAuth token expiry", err)
		}
		parsed, parseErr := strconv.ParseInt(text, 10, 64)
		if parseErr != nil {
			return 0, newCloudError(CloudErrOAuthProtocol, "decode OAuth token expiry", parseErr)
		}
		seconds = parsed
	}
	if seconds < 1 || seconds > int64((24*time.Hour)/time.Second) {
		return 0, newCloudError(CloudErrOAuthProtocol, "validate OAuth token expiry", nil)
	}
	return time.Duration(seconds) * time.Second, nil
}

func validOAuthRedirectURI(redirectURI, clientID string) bool {
	redirect, err := url.Parse(redirectURI)
	if err != nil || redirect.Scheme != "http" || redirect.Opaque != "" ||
		redirect.Hostname() != "127.0.0.1" ||
		redirect.Path != oauthLoopbackCallbackPath || redirect.RawPath != "" ||
		redirect.RawQuery != "" || redirect.ForceQuery || redirect.Fragment != "" ||
		redirect.User != nil {
		return false
	}
	client, err := url.Parse(clientID)
	return err == nil && redirect.Scheme == client.Scheme && redirect.Host == client.Host
}
