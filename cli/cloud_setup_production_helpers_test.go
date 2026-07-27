package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func productionCloudTestStore(
	t *testing.T,
	backend *memoryOAuthSecretBackend,
) *KeyringOAuthSecretStore {
	t.Helper()
	store, err := NewOAuthSecretStore(backend, "profile-1")
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time {
		return time.Now().UTC()
	}
	return store
}

func productionCloudTestEnvelope() OAuthSecretEnvelope {
	expires := time.Now().UTC().Add(24 * time.Hour)
	return OAuthSecretEnvelope{
		Generation:            productionCloudTestGeneration,
		CanonicalOrigin:       productionCloudTestOrigin,
		ClientID:              productionCloudTestClientID,
		RefreshToken:          "refresh-secret",
		RefreshTokenID:        "refresh-1",
		RefreshTokenExpiresAt: &expires,
		HAUserID:              "user-1",
		RelayInstanceID:       "relay-1",
	}
}

func resetProductionCloudPolicies(backend *memoryOAuthSecretBackend) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.policies = nil
	backend.operations = nil
}

func assertProductionCloudPolicies(
	t *testing.T,
	backend *memoryOAuthSecretBackend,
	want SecretStoreUIPolicy,
) {
	t.Helper()
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.policies) == 0 {
		t.Fatal("expected native secret-store calls")
	}
	for index, policy := range backend.policies {
		if policy != want {
			t.Fatalf("secret-store call %d policy = %q, want %q", index, policy, want)
		}
	}
}

func productionCloudMappedClient(
	t *testing.T,
	server *httptest.Server,
) *http.Client {
	t.Helper()
	serverAddress := server.Listener.Addr().String()
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(
		ctx context.Context,
		network string,
		_ string,
	) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, serverAddress)
	}
	transport.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true, // Test server certificate cannot cover the canonical Nabu Casa host.
	}
	return &http.Client{Transport: transport}
}

func newProductionCloudProtocolServer(t *testing.T) *httptest.Server {
	t.Helper()
	appSlug, err := selectedCloudNOVAAppSlug()
	if err != nil {
		t.Fatal(err)
	}
	handler := http.NewServeMux()
	handler.HandleFunc("/auth/token", func(response http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil ||
			request.Form.Get("grant_type") != "refresh_token" ||
			request.Form.Get("refresh_token") != "refresh-secret" ||
			request.Form.Get("client_id") != productionCloudTestClientID {
			http.Error(response, "invalid token request", http.StatusBadRequest)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{
			"access_token":"access-secret",
			"token_type":"Bearer",
			"expires_in":3600
		}`)
	})
	handler.HandleFunc("/api/websocket", func(response http.ResponseWriter, request *http.Request) {
		serveProductionCloudWebSocket(t, response, request, appSlug)
	})
	handler.HandleFunc(productionCloudTestIngressRoot+CloudPathRelayInfo, func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		cookie, err := request.Cookie("ingress_session")
		if err != nil || cookie.Value != strings.Repeat("a", 128) ||
			request.Header.Get("Authorization") != "" {
			http.Error(response, "invalid ingress session", http.StatusUnauthorized)
			return
		}
		response.Header().Set(relayVersionHeader, "1.2.3")
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{
			"ok":true,
			"data":{
				"protocol_version":"v1",
				"relay_instance_id":"relay-1",
				"relay_version":"1.2.3",
				"capabilities":{
					"device_user_binding":true,
					"pairing_v2":true,
					"functional_routes":["health","ws","core","files","backups"],
					"cleanup_routes":["device_revoke_self"]
				}
			}
		}`)
	})
	handler.HandleFunc(productionCloudTestIngressRoot+CloudPathDeviceRevoke, func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		cookie, err := request.Cookie("ingress_session")
		credential := strings.TrimPrefix(
			request.Header.Get("Authorization"),
			"Bearer ",
		)
		parsed := parseDeviceCredential(credential)
		var body struct {
			RelayInstanceID string `json:"relay_instance_id"`
		}
		if err != nil ||
			cookie.Value != strings.Repeat("a", 128) ||
			parsed == nil ||
			json.NewDecoder(request.Body).Decode(&body) != nil ||
			body.RelayInstanceID != "relay-1" {
			http.Error(response, "invalid device revocation", http.StatusUnauthorized)
			return
		}
		response.Header().Set(relayVersionHeader, "1.2.3")
		response.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(
			response,
			`{"ok":true,"data":{"device_id":%q,"revoked":true,"changed":true}}`,
			parsed.deviceID,
		)
	})
	return httptest.NewTLSServer(handler)
}

func serveProductionCloudWebSocket(
	t *testing.T,
	response http.ResponseWriter,
	request *http.Request,
	appSlug string,
) {
	t.Helper()
	connection, err := websocket.Accept(response, request, nil)
	if err != nil {
		t.Errorf("accept WebSocket: %v", err)
		return
	}
	defer connection.Close(websocket.StatusNormalClosure, "")
	ctx := request.Context()
	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"type":"auth_required"}`)); err != nil {
		t.Errorf("write auth challenge: %v", err)
		return
	}
	_, authentication, err := connection.Read(ctx)
	if err != nil || !strings.Contains(string(authentication), `"access_token":"access-secret"`) {
		t.Errorf("authentication = %s, %v", authentication, err)
		return
	}
	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"type":"auth_ok"}`)); err != nil {
		t.Errorf("write auth result: %v", err)
		return
	}

	expiry := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	results := map[string]string{
		"auth/current_user": `{"id":"user-1","name":"NOVA","is_owner":false,"is_admin":false}`,
		"auth/refresh_tokens": `[{
			"id":"refresh-1",
			"client_id":"` + productionCloudTestClientID + `",
			"is_current":true,
			"type":"normal",
			"expire_at":"` + expiry + `"
		}]`,
		"cloud/status": `{
			"logged_in":true,
			"active_subscription":true,
			"remote_connected":true,
			"remote_domain":"unit.ui.nabu.casa",
			"remote_certificate_status":"ready",
			"remote_certificate":{
				"common_name":"unit.ui.nabu.casa",
				"alternative_names":["unit.ui.nabu.casa"]
			},
			"prefs":{"remote_enabled":true}
		}`,
		"supervisor/api": `{
			"slug":"` + appSlug + `",
			"state":"started",
			"version":"1.2.3",
			"ingress":true,
			"ingress_entry":"` + productionCloudTestIngressRoot + `",
			"ingress_url":"` + productionCloudTestIngressRoot + haNOVAIngressUIEntry + `"
		}`,
	}
	for index := 1; index <= 5; index++ {
		_, data, readErr := connection.Read(ctx)
		if readErr != nil {
			t.Errorf("read command %d: %v", index, readErr)
			return
		}
		var command struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &command); err != nil || command.ID != int64(index) {
			t.Errorf("command %d = %s, %v", index, data, err)
			return
		}
		result, ok := results[command.Type]
		if command.Type == "supervisor/api" && index == 5 {
			result = `{"session":"` + strings.Repeat("a", 128) + `"}`
			ok = true
		}
		if !ok {
			t.Errorf("unexpected command type %q at %d", command.Type, index)
			return
		}
		message := fmt.Sprintf(
			`{"id":%d,"type":"result","success":true,"result":%s}`,
			command.ID,
			result,
		)
		if err := connection.Write(ctx, websocket.MessageText, []byte(message)); err != nil {
			t.Errorf("write command %d result: %v", index, err)
			return
		}
	}
}
