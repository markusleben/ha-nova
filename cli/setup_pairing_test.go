package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestExchangeRelayPairingCodeUsesBodyWithoutBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/pair" {
			t.Fatalf("request = %s %s, want POST /pair", request.Method, request.URL.Path)
		}
		if authorization := request.Header.Get("Authorization"); authorization != "" {
			t.Fatalf("Authorization = %q, want empty", authorization)
		}
		var payload struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode pairing request: %v", err)
		}
		if payload.Code != "123456" {
			t.Fatalf("pairing code = %q, want normalized six digits", payload.Code)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":{"relay_token":"paired-relay-token"}}`))
	}))
	defer server.Close()

	baseURLWithCredentials := strings.Replace(server.URL, "http://", "http://user:password@", 1)
	token, err := exchangeRelayPairingCode(server.Client(), baseURLWithCredentials, "123 456")
	if err != nil {
		t.Fatalf("exchangeRelayPairingCode() error = %v", err)
	}
	if token != "paired-relay-token" {
		t.Fatalf("token = %q, want paired-relay-token", token)
	}
}

func TestExchangeRelayPairingCodeKeepsFailuresSecretAndBounded(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		retryAfter string
		wantError  error
		wantRetry  int
	}{
		{name: "rejected", status: http.StatusUnauthorized, wantError: errRelayPairingRejected},
		{name: "unsupported", status: http.StatusNotFound, wantError: errRelayPairingUnsupported},
		{name: "rate limited", status: http.StatusTooManyRequests, retryAfter: "999", wantRetry: 300},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.retryAfter != "" {
					w.Header().Set("Retry-After", test.retryAfter)
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"error":{"message":"must-not-leak"}}`))
			}))
			defer server.Close()

			_, err := exchangeRelayPairingCode(server.Client(), server.URL, "654321")
			if test.wantError != nil && !errors.Is(err, test.wantError) {
				t.Fatalf("error = %v, want %v", err, test.wantError)
			}
			if strings.Contains(err.Error(), "654321") || strings.Contains(err.Error(), "must-not-leak") {
				t.Fatalf("pairing error leaked secret material: %v", err)
			}
			if test.wantRetry > 0 {
				var rateLimit *relayPairingRateLimitError
				if !errors.As(err, &rateLimit) || rateLimit.retryAfterSeconds != test.wantRetry {
					t.Fatalf("rate limit error = %#v, want retry %d", err, test.wantRetry)
				}
			}
		})
	}
}

func TestExchangeRelayPairingCodeDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Add(1)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	_, err := exchangeRelayPairingCode(server.Client(), server.URL, "123456")
	if err == nil || !strings.Contains(err.Error(), "HTTP 307") {
		t.Fatalf("error = %v, want HTTP 307 without redirect", err)
	}
	if redirected.Load() != 0 {
		t.Fatalf("redirect target received %d request(s), want 0", redirected.Load())
	}
}

func TestInteractiveSetupPairsStoresTokenAndVerifiesHealthAndWS(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"codex": true})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer haServer.Close()

	const pairedToken = "paired-token-never-render"
	var pairCalls atomic.Int32
	var healthCalls atomic.Int32
	var wsCalls atomic.Int32
	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/pair":
			pairCalls.Add(1)
			if request.Header.Get("Authorization") != "" {
				t.Error("pairing request must not send Authorization")
			}
			var payload struct {
				Code string `json:"code"`
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Errorf("decode pairing request: %v", err)
			}
			if payload.Code != "123456" {
				t.Errorf("pairing code = %q, want 123456", payload.Code)
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"relay_token":"` + pairedToken + `"}}`))
		case "/health":
			healthCalls.Add(1)
			assertPairedBearer(t, request, pairedToken)
			_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
		case "/ws":
			wsCalls.Add(1)
			assertPairedBearer(t, request, pairedToken)
			_, _ = w.Write([]byte(`{"ok":true,"data":{"type":"pong"}}`))
		default:
			http.NotFound(w, request)
		}
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error = %v", err)
	}
	input := joinSetupInputs(setupWizardPairingPrompts("123 456"))
	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(
			paths,
			runtimeConfig{},
			loadStateOrDefault(paths),
			"codex",
			normalizeHostInput(haServer.URL),
			haServer.URL,
			relayServer.URL,
			"",
			false,
		)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	output := stdout + stderr
	for _, want := range []string{"Set up Home Assistant Access Token", "Pair this device", "This device is paired", "Relay /ws ping succeeded", "Setup complete!"} {
		if !strings.Contains(output, want) {
			t.Fatalf("wizard output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "123456") || strings.Contains(output, pairedToken) {
		t.Fatalf("wizard output leaked pairing credentials:\n%s", output)
	}
	if pairCalls.Load() != 1 || healthCalls.Load() != 1 || wsCalls.Load() != 1 {
		t.Fatalf("calls pair/health/ws = %d/%d/%d, want 1/1/1", pairCalls.Load(), healthCalls.Load(), wsCalls.Load())
	}
	storedToken, err := readRelayAuthToken()
	if err != nil || storedToken != pairedToken {
		t.Fatalf("stored token = %q, err = %v", storedToken, err)
	}
	config, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(config), "123456") || strings.Contains(string(config), pairedToken) {
		t.Fatalf("normal config persisted pairing credentials: %s", config)
	}
}

func assertPairedBearer(t *testing.T, request *http.Request, token string) {
	t.Helper()
	if got := request.Header.Get("Authorization"); got != "Bearer "+token {
		t.Errorf("Authorization = %q, want paired bearer", got)
	}
}
