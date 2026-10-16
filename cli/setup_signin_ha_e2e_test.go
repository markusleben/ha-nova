package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

// Opt-in end-to-end test of Home Assistant sign-in against a REAL, throwaway
// Home Assistant (never a real home). The browser is replaced by Home
// Assistant's own login-flow API, so Home Assistant's real client ID and
// redirect checks, token endpoint, WebSocket, and revocation run unchanged.
//
//	HA_NOVA_SIGNIN_E2E_HA_URL   e.g. http://127.0.0.1:8123
//	HA_NOVA_SIGNIN_E2E_USER / HA_NOVA_SIGNIN_E2E_PASSWORD   a throwaway owner
//	HA_NOVA_SIGNIN_E2E_EXPECT   "no-supervisor" (Container/Core), "paired", or
//	                            "not-owner" (USER is a non-owner admin)
//	HA_NOVA_SIGNIN_E2E_RELAY_URL   relay bootstrap URL (paired only)
//	HA_NOVA_SIGNIN_E2E_APP_SLUG    App slug, e.g. local_ha_nova_relay (paired only)
func TestHASignInAgainstThrowawayHomeAssistant(t *testing.T) {
	haURL := os.Getenv("HA_NOVA_SIGNIN_E2E_HA_URL")
	if haURL == "" {
		t.Skip("set HA_NOVA_SIGNIN_E2E_HA_URL to a throwaway Home Assistant to run the sign-in e2e")
	}
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	if slug := os.Getenv("HA_NOVA_SIGNIN_E2E_APP_SLUG"); slug != "" {
		previous := cloudRemoteBuildIdentityForRuntime
		cloudRemoteBuildIdentityForRuntime = func() cloudRemoteBuildIdentity {
			return cloudRemoteBuildIdentity{Development: true, AppSlug: slug}
		}
		t.Cleanup(func() { cloudRemoteBuildIdentityForRuntime = previous })
	}
	previousOpen := openBrowserForSetup
	openBrowserForSetup = func(target string) error {
		go signInThroughLoginFlow(t, haURL, target)
		return nil
	}
	t.Cleanup(func() { openBrowserForSetup = previousOpen })
	t.Setenv("HA_NOVA_NO_BROWSER", "")

	origin, err := resolveHASignInOrigin(context.Background(), haURL)
	if err != nil {
		t.Fatalf("pre-check of the saved origin: %v", err)
	}
	t.Logf("sign-in origin: %s", origin)
	haURL = origin.String()
	var out bytes.Buffer
	session, err := openHASignInIngress(context.Background(), &out, haURL)
	if strings.Contains(out.String(), "may still list this sign-in") {
		t.Fatalf("Home Assistant session was not revoked: %q", out.String())
	}
	switch os.Getenv("HA_NOVA_SIGNIN_E2E_EXPECT") {
	case "no-supervisor":
		if !errors.Is(err, errSignInAppMissing) {
			t.Fatalf("want the no-App fallback, got %v (%q)", err, out.String())
		}
		t.Logf("fallback line: %s", haSignInFallbackLine(err, session.user))
		return
	case "not-owner":
		if !errors.Is(err, errSignInNotOwner) {
			t.Fatalf("want the non-owner fallback, got %v", err)
		}
		// The relay refuses on its own too: arm through a real ingress session
		// of the same non-owner, skipping the CLI's friendly pre-check.
		if _, err := armSignInPairing(context.Background(), nonOwnerIngress(t, haURL)); !errors.Is(err, errSignInNotOwner) {
			t.Fatalf("relay armed pairing for a non-owner: %v", err)
		}
		return
	case "paired":
	default:
		t.Fatal("set HA_NOVA_SIGNIN_E2E_EXPECT to no-supervisor, not-owner, or paired")
	}
	if err != nil {
		t.Fatalf("sign-in: %v (%q)", err, out.String())
	}
	cfg := &runtimeConfig{}
	deviceID, err := runSecurePairingWithProvisioner(
		os.Getenv("HA_NOVA_SIGNIN_E2E_RELAY_URL"),
		func(meta deviceMetadata) (*provisionedCredential, error) {
			return pairDeviceV2Armed(context.Background(), session.ingress, meta)
		},
		cfg,
		func(*runtimeConfig) error { return nil },
		defaultPairingClientInfo(),
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatalf("pairing through ingress: %v", err)
	}
	current, ok, _ := readDeviceCredential()
	if !ok || deviceIDOf(current) != deviceID {
		t.Fatal("credential not stored")
	}
	// A real relay call over pinned TLS that reaches Home Assistant itself.
	ping, err := http.NewRequest(http.MethodPost, cfg.RelaySecureBaseURL+"/ws", strings.NewReader(`{"type":"ping"}`))
	if err != nil {
		t.Fatal(err)
	}
	ping.Header.Set("Authorization", "Bearer "+current)
	ping.Header.Set("Content-Type", "application/json")
	response, err := spkiPinnedClient(cfg.RelaySpkiPin).Do(ping)
	if err != nil {
		t.Fatalf("relay /ws ping over pinned TLS: %v", err)
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("relay /ws ping = %d %s", response.StatusCode, body)
	}
	// The ping opened the relay's Home Assistant WebSocket; health says so.
	health, err := http.NewRequest(http.MethodGet, cfg.RelaySecureBaseURL+"/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	health.Header.Set("Authorization", "Bearer "+current)
	response, err = spkiPinnedClient(cfg.RelaySpkiPin).Do(health)
	if err != nil {
		t.Fatalf("relay /health over pinned TLS: %v", err)
	}
	body, _ = io.ReadAll(io.LimitReader(response.Body, 4096))
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"ha_ws_connected":true`) {
		t.Fatalf("relay /health = %d %s", response.StatusCode, body)
	}
	t.Logf("paired device %s via %s (pin %s)", deviceID, cfg.RelaySecureBaseURL, cfg.RelaySpkiPin)
}

// nonOwnerIngress repeats the sign-in steps without the owner pre-check.
func nonOwnerIngress(t *testing.T, haURL string) *CloudIngressClient {
	t.Helper()
	ctx := context.Background()
	origin, err := parseLocalHAOrigin(haURL)
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := haSignInOAuthFlow().AuthorizeOrigin(ctx, origin, func(_ context.Context, target string) error {
		go signInThroughLoginFlow(t, haURL, target)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	oauth := newHAOAuthClientForOrigin(origin, haSignInHTTPClient)
	token, err := oauth.ExchangeAuthorizationCode(ctx, authorization)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := revokeAndVerifyCloudAuthorizationWithClient(context.Background(), oauth, token.RefreshToken, authorization.ClientID); err != nil {
			t.Errorf("revoke: %v", err)
		}
	})
	socket, err := dialHAWebSocketForOrigin(ctx, origin, token.AccessToken, HAWebSocketClientOptions{HTTPClient: haSignInHTTPClient})
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	app, err := socket.NOVAAppInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	session, err := socket.CreateIngressSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ingress, err := newCloudIngressClientForOrigin(origin, app.IngressEntry, session.Session, haSignInHTTPClient)
	if err != nil {
		t.Fatal(err)
	}
	return ingress
}

// signInThroughLoginFlow does what the user's browser does on the Home
// Assistant login page, then follows the redirect back to the CLI.
func signInThroughLoginFlow(t *testing.T, haURL, authorizeURL string) {
	authorize, err := url.Parse(authorizeURL)
	if err != nil {
		t.Errorf("authorize URL: %v", err)
		return
	}
	query := authorize.Query()
	clientID, redirectURI := query.Get("client_id"), query.Get("redirect_uri")
	var flow struct {
		FlowID string `json:"flow_id"`
	}
	if err := postJSON(haURL+"/auth/login_flow", map[string]any{
		"client_id":    clientID,
		"handler":      []any{"homeassistant", nil},
		"redirect_uri": redirectURI,
		"type":         "authorize",
	}, &flow); err != nil {
		t.Errorf("start login flow: %v", err)
		return
	}
	var result struct {
		Type   string `json:"type"`
		Result string `json:"result"`
	}
	if err := postJSON(haURL+"/auth/login_flow/"+flow.FlowID, map[string]any{
		"client_id": clientID,
		"username":  os.Getenv("HA_NOVA_SIGNIN_E2E_USER"),
		"password":  os.Getenv("HA_NOVA_SIGNIN_E2E_PASSWORD"),
	}, &result); err != nil || result.Type != "create_entry" {
		t.Errorf("finish login flow: %v (%s)", err, result.Type)
		return
	}
	response, err := http.Get(redirectURI + "?" + url.Values{
		"code":  {result.Result},
		"state": {query.Get("state")},
	}.Encode())
	if err != nil {
		t.Error("callback request failed") // the URL error would carry the code
		return
	}
	response.Body.Close()
}

func postJSON(target string, body any, out any) error {
	encoded, _ := json.Marshal(body)
	response, err := http.Post(target, "application/json", bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", target, response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(out)
}
