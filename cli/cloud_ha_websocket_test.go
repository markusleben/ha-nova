package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type fakeHAWebSocketConnection struct {
	reads   [][]byte
	writes  [][]byte
	closed  bool
	readErr error
}

func (c *fakeHAWebSocketConnection) Read(context.Context) ([]byte, error) {
	if c.readErr != nil {
		err := c.readErr
		c.readErr = nil
		return nil, err
	}
	if len(c.reads) == 0 {
		return nil, errors.New("unexpected read")
	}
	data := c.reads[0]
	c.reads = c.reads[1:]
	return data, nil
}

func (c *fakeHAWebSocketConnection) Write(_ context.Context, data []byte) error {
	c.writes = append(c.writes, append([]byte(nil), data...))
	return nil
}

func (c *fakeHAWebSocketConnection) Close() error {
	c.closed = true
	return nil
}

type fakeHAWebSocketDialer struct {
	conn      HAWebSocketConnection
	status    int
	err       error
	endpoint  string
	client    *http.Client
	readLimit int64
}

func (d *fakeHAWebSocketDialer) Dial(
	_ context.Context,
	endpoint string,
	client *http.Client,
	readLimit int64,
) (HAWebSocketConnection, int, error) {
	d.endpoint = endpoint
	d.client = client
	d.readLimit = readLimit
	return d.conn, d.status, d.err
}

func TestHAWebSocketClientAuthenticatesAndCallsAllowlistedAPIs(t *testing.T) {
	expires := "2030-01-01T00:00:00Z"
	appSlug, err := selectedCloudNOVAAppSlug()
	if err != nil {
		t.Fatal(err)
	}
	conn := &fakeHAWebSocketConnection{reads: [][]byte{
		[]byte(`{"type":"auth_required","ha_version":"2026.7.0"}`),
		[]byte(`{"type":"auth_ok","ha_version":"2026.7.0"}`),
		[]byte(`{"id":1,"type":"result","success":true,"result":{"id":"user-1","name":"NOVA","is_owner":true,"is_admin":true}}`),
		[]byte(`{"id":2,"type":"result","success":true,"result":[{"id":"refresh-1","client_id":"http://127.0.0.1:43123/ha-nova","is_current":true,"type":"normal","expire_at":"` + expires + `"}]}`),
		[]byte(`{"id":3,"type":"result","success":true,"result":{"logged_in":true,"active_subscription":true,"remote_connected":true,"remote_domain":"unit.ui.nabu.casa","remote_certificate_status":"ready","remote_certificate":{"common_name":"unit.ui.nabu.casa","alternative_names":["home.example.com"]},"prefs":{"remote_enabled":true}}}`),
		[]byte(`{"id":4,"type":"result","success":true,"result":{"slug":"` + appSlug + `","state":"started","version":"1.2.3","ingress":true,"ingress_entry":"/api/hassio_ingress/abcdefghijklmnopqrstuvwxyzABCDEFGH123456789","ingress_url":"/api/hassio_ingress/abcdefghijklmnopqrstuvwxyzABCDEFGH123456789/home","options":{"must_not_be_retained":"secret"}}}`),
		[]byte(`{"id":5,"type":"result","success":true,"result":{"session":"` + strings.Repeat("a", 128) + `"}}`),
	}}
	dialer := &fakeHAWebSocketDialer{conn: conn}
	client, err := DialHAWebSocket(
		context.Background(),
		"https://unit.ui.nabu.casa",
		"access-secret",
		HAWebSocketClientOptions{Dialer: dialer},
	)
	if err != nil {
		t.Fatalf("DialHAWebSocket: %v", err)
	}
	defer client.Close()
	if dialer.endpoint != "wss://unit.ui.nabu.casa/api/websocket" ||
		dialer.readLimit != haWebSocketMaxMessageBytes ||
		dialer.client == nil || dialer.client.CheckRedirect == nil || dialer.client.Jar != nil {
		t.Fatalf("unsafe dial options: endpoint=%q limit=%d client=%+v", dialer.endpoint, dialer.readLimit, dialer.client)
	}
	user, err := client.CurrentUser(context.Background())
	if err != nil || user.ID != "user-1" || !user.IsOwner {
		t.Fatalf("CurrentUser = %+v, %v", user, err)
	}
	tokens, err := client.RefreshTokens(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	current, err := VerifyCurrentOAuthRefreshToken(
		tokens,
		"http://127.0.0.1:43123/ha-nova",
		time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC),
	)
	if err != nil || current.ID != "refresh-1" {
		t.Fatalf("current refresh = %+v, %v", current, err)
	}
	status, err := client.CloudStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	origin, _ := ResolveCanonicalNabuOrigin(
		context.Background(),
		"https://home.example.com",
		&fakeCloudResolver{canonical: "unit.ui.nabu.casa."},
	)
	if err := status.ValidateForOrigin(origin); err != nil {
		t.Fatalf("Cloud status: %v", err)
	}
	info, err := client.NOVAAppInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	root, err := info.MachineIngressRoot()
	if err != nil || strings.HasSuffix(root, "/home") {
		t.Fatalf("machine ingress root = %q, %v", root, err)
	}
	session, err := client.CreateIngressSession(context.Background())
	if err != nil || len(session.Session) != 128 {
		t.Fatalf("session = %+v, %v", session, err)
	}

	if len(conn.writes) != 6 {
		t.Fatalf("writes = %d", len(conn.writes))
	}
	var authentication map[string]any
	if err := json.Unmarshal(conn.writes[0], &authentication); err != nil ||
		authentication["type"] != "auth" || authentication["access_token"] != "access-secret" {
		t.Fatalf("authentication frame = %s", conn.writes[0])
	}
	var addonCommand map[string]any
	_ = json.Unmarshal(conn.writes[4], &addonCommand)
	if addonCommand["endpoint"] != "/addons/"+appSlug+"/info" ||
		addonCommand["method"] != "get" {
		t.Fatalf("addon command = %v", addonCommand)
	}
}

func TestMachineIngressRootAcceptsExactSupervisorUIURLForms(t *testing.T) {
	appSlug, err := selectedCloudNOVAAppSlug()
	if err != nil {
		t.Fatal(err)
	}
	const ingressRoot = "/api/hassio_ingress/abcdefghijklmnopqrstuvwxyzABCDEFGH123456789"
	for _, ingressURL := range []string{
		ingressRoot + "/home",
		ingressRoot + "//home",
	} {
		t.Run(ingressURL, func(t *testing.T) {
			info := HAAddonInfo{
				Slug:         appSlug,
				State:        "started",
				Version:      "1.2.3",
				Ingress:      true,
				IngressEntry: ingressRoot,
				IngressURL:   ingressURL,
			}
			root, err := info.MachineIngressRoot()
			if err != nil || root != ingressRoot {
				t.Fatalf("MachineIngressRoot() = %q, %v", root, err)
			}
		})
	}
}

func TestMachineIngressRootRejectsNormalizedLookalikes(t *testing.T) {
	appSlug, err := selectedCloudNOVAAppSlug()
	if err != nil {
		t.Fatal(err)
	}
	const ingressRoot = "/api/hassio_ingress/abcdefghijklmnopqrstuvwxyzABCDEFGH123456789"
	for _, ingressURL := range []string{
		ingressRoot + "/./home",
		ingressRoot + "/other/../home",
		ingressRoot + "///home",
	} {
		t.Run(ingressURL, func(t *testing.T) {
			info := HAAddonInfo{
				Slug:         appSlug,
				State:        "started",
				Version:      "1.2.3",
				Ingress:      true,
				IngressEntry: ingressRoot,
				IngressURL:   ingressURL,
			}
			if _, err := info.MachineIngressRoot(); !IsCloudErrorCode(
				err,
				CloudErrAppNotReady,
			) {
				t.Fatalf("MachineIngressRoot() error = %v", err)
			}
		})
	}
}

func TestCloudStatusClassifiesInactiveSubscriptionSeparately(t *testing.T) {
	origin, err := cloudOriginFromCanonical("https://unit.ui.nabu.casa")
	if err != nil {
		t.Fatal(err)
	}
	status := HACloudStatus{
		LoggedIn:           true,
		ActiveSubscription: false,
	}
	status.Prefs.RemoteEnabled = true
	err = status.ValidateForOrigin(origin)
	if !IsCloudErrorCode(err, CloudErrSubscriptionInactive) {
		t.Fatalf("inactive subscription error = %v", err)
	}
	problem := cloudProblemForError(err)
	if problem.Code != cloudProblemSubscription ||
		problem.Remediation != cloudRemediationManagePlan {
		t.Fatalf("inactive subscription problem = %+v", problem)
	}
}

func TestHAWebSocketClientRejectsRedirectAndInvalidAuthWithoutSecretLeak(t *testing.T) {
	redirectDialer := &fakeHAWebSocketDialer{
		status: http.StatusFound,
		err:    errors.New("handshake redirected"),
	}
	_, err := DialHAWebSocket(
		context.Background(),
		"https://unit.ui.nabu.casa",
		"access-secret",
		HAWebSocketClientOptions{Dialer: redirectDialer},
	)
	if !IsCloudErrorCode(err, CloudErrRedirectRejected) {
		t.Fatalf("redirect error = %v", err)
	}

	conn := &fakeHAWebSocketConnection{reads: [][]byte{
		[]byte(`{"type":"auth_required"}`),
		[]byte(`{"type":"auth_invalid","message":"access-secret"}`),
	}}
	_, err = DialHAWebSocket(
		context.Background(),
		"https://unit.ui.nabu.casa",
		"access-secret",
		HAWebSocketClientOptions{Dialer: &fakeHAWebSocketDialer{conn: conn}},
	)
	if !IsCloudErrorCode(err, CloudErrUnauthorized) || strings.Contains(err.Error(), "access-secret") {
		t.Fatalf("invalid auth error = %v", err)
	}
	if !conn.closed {
		t.Fatal("failed authentication left WebSocket open")
	}

	oversized := &fakeHAWebSocketConnection{
		readErr: fmt.Errorf(
			"%w: peer detail must not surface",
			websocket.ErrMessageTooBig,
		),
	}
	_, err = DialHAWebSocket(
		context.Background(),
		"https://unit.ui.nabu.casa",
		"access-secret",
		HAWebSocketClientOptions{
			Dialer: &fakeHAWebSocketDialer{conn: oversized},
		},
	)
	if !IsCloudErrorCode(err, CloudErrResponseTooLarge) ||
		strings.Contains(err.Error(), "peer detail") {
		t.Fatalf("oversized authentication error = %v", err)
	}
	if !oversized.closed {
		t.Fatal("oversized authentication left WebSocket open")
	}
}

func TestVerifyCurrentOAuthRefreshTokenRequiresExpiryAndExactClient(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	base := []HARefreshTokenMetadata{{
		ID: "refresh-1", ClientID: "http://127.0.0.1:43123/ha-nova",
		IsCurrent: true, Type: "normal", ExpiresAt: &expires,
	}}
	if _, err := VerifyCurrentOAuthRefreshToken(base, base[0].ClientID, time.Now()); err != nil {
		t.Fatalf("valid token: %v", err)
	}
	broken := append([]HARefreshTokenMetadata(nil), base...)
	broken[0].ExpiresAt = nil
	if _, err := VerifyCurrentOAuthRefreshToken(broken, base[0].ClientID, time.Now()); !IsCloudErrorCode(err, CloudErrOAuthInvalidGrant) {
		t.Fatalf("missing expiry error = %v", err)
	}
}

func TestHAWebSocketClientRejectsAmbiguousResultEnvelopes(t *testing.T) {
	for name, response := range map[string]string{
		"success with error": `{
			"id":1,
			"type":"result",
			"success":true,
			"result":{"id":"user-1"},
			"error":{"code":"unauthorized"}
		}`,
		"failure without error": `{
			"id":1,
			"type":"result",
			"success":false
		}`,
		"failure with result": `{
			"id":1,
			"type":"result",
			"success":false,
			"result":{"changed":true},
			"error":{"code":"failed"}
		}`,
	} {
		t.Run(name, func(t *testing.T) {
			conn := &fakeHAWebSocketConnection{
				reads: [][]byte{[]byte(response)},
			}
			client := &HAWebSocketClient{
				conn: conn,
				gate: make(chan struct{}, 1),
			}
			client.gate <- struct{}{}
			var result map[string]any
			err := client.Call(
				context.Background(),
				"auth/current_user",
				nil,
				&result,
			)
			if !IsCloudErrorCode(err, CloudErrHAProtocol) {
				t.Fatalf("ambiguous result error = %v", err)
			}
		})
	}
}
