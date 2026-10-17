package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestRelayResponseEnvelopeControlsOutcomeSemantics(t *testing.T) {
	for name, testCase := range map[string]struct {
		body        string
		wantExit    int
		wantUnknown bool
	}{
		"malformed is unknown": {
			body:        `{"data":{"changed":true}}`,
			wantExit:    1,
			wantUnknown: true,
		},
		"explicit failure is definitive": {
			body:        `{"ok":false,"error":{"code":"REJECTED"}}`,
			wantExit:    0,
			wantUnknown: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			paths, _ := setupStrictInputRelay(t)
			server := httptest.NewServer(http.HandlerFunc(func(
				response http.ResponseWriter,
				_ *http.Request,
			) {
				response.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(response, testCase.body)
			}))
			t.Cleanup(server.Close)
			cfg, err := loadConfig(paths)
			if err != nil {
				t.Fatal(err)
			}
			cfg.RelayBaseURL = server.URL
			if err := saveConfig(paths, cfg); err != nil {
				t.Fatal(err)
			}

			exit, output := captureCommandOutput(t, func() int {
				return runRelayProxy(
					paths,
					"ws",
					[]string{"--data", `{"type":"config/area_registry/create","name":"Kitchen"}`},
				)
			})
			if exit != testCase.wantExit {
				t.Fatalf(
					"exit/output = %d, %q; want exit %d",
					exit,
					output,
					testCase.wantExit,
				)
			}
			if got := strings.Contains(output, "OUTCOME_UNKNOWN"); got != testCase.wantUnknown {
				t.Fatalf("unknown classification = %v, output = %q", got, output)
			}
		})
	}
}

func TestRelayAuthenticatedPostRedirectIsNotReplayed(t *testing.T) {
	for _, status := range []int{
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			paths, _ := setupStrictInputRelay(t)
			sourceCalls := 0
			targetCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(
				response http.ResponseWriter,
				request *http.Request,
			) {
				switch request.URL.Path {
				case "/ws":
					sourceCalls++
					if request.Method != http.MethodPost {
						t.Errorf("source method = %s, want POST", request.Method)
					}
					if request.Header.Get("Authorization") != "Bearer test-token" {
						t.Errorf(
							"source Authorization = %q",
							request.Header.Get("Authorization"),
						)
					}
					response.Header().Set("Location", "/redirect-target")
					response.WriteHeader(status)
					_, _ = fmt.Fprint(response, `{"ok":true,"data":{}}`)
				case "/redirect-target":
					targetCalls++
					_, _ = fmt.Fprint(response, `{"ok":true,"data":{}}`)
				default:
					http.NotFound(response, request)
				}
			}))
			t.Cleanup(server.Close)

			cfg, err := loadConfig(paths)
			if err != nil {
				t.Fatal(err)
			}
			cfg.RelayBaseURL = server.URL
			if err := saveConfig(paths, cfg); err != nil {
				t.Fatal(err)
			}

			exit, output := captureCommandOutput(t, func() int {
				return runRelayProxy(
					paths,
					"ws",
					[]string{"--data", `{"type":"config/area_registry/create","name":"Kitchen"}`},
				)
			})
			if exit != 1 || !strings.Contains(output, "OUTCOME_UNKNOWN") {
				t.Fatalf("exit/output = %d, %q", exit, output)
			}
			if sourceCalls != 1 || targetCalls != 0 {
				t.Fatalf(
					"source calls = %d, redirected target calls = %d; want 1, 0",
					sourceCalls,
					targetCalls,
				)
			}
		})
	}
}

func TestRelayHealthBodyReadFailureIsOutcomeUnknown(t *testing.T) {
	paths, _ := setupStrictInputRelay(t)
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		if request.URL.Path != "/health" {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Length", "128")
		response.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(response, `{"ok":true`)
	}))
	t.Cleanup(server.Close)

	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RelayBaseURL = server.URL
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}

	exit, output := captureCommandOutput(t, func() int {
		return runHealth(paths, nil)
	})
	if exit != 1 ||
		!strings.Contains(output, "OUTCOME_UNKNOWN") ||
		!strings.Contains(output, "reading the Relay health response") {
		t.Fatalf("exit/output = %d, %q", exit, output)
	}
}

func TestRelayHealthRedirectIsNotFollowedOrAccepted(t *testing.T) {
	for _, status := range []int{
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			paths, _ := setupStrictInputRelay(t)
			targetCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(
				response http.ResponseWriter,
				request *http.Request,
			) {
				switch request.URL.Path {
				case "/health":
					response.Header().Set("Location", "/redirect-target")
					response.WriteHeader(status)
				case "/redirect-target":
					targetCalls++
					_, _ = fmt.Fprint(response, `{"ok":true,"data":{"status":"ok"}}`)
				default:
					http.NotFound(response, request)
				}
			}))
			t.Cleanup(server.Close)

			cfg, err := loadConfig(paths)
			if err != nil {
				t.Fatal(err)
			}
			cfg.RelayBaseURL = server.URL
			if err := saveConfig(paths, cfg); err != nil {
				t.Fatal(err)
			}

			exit, output := captureCommandOutput(t, func() int {
				return runHealth(paths, nil)
			})
			if exit != 1 || !strings.Contains(output, "OUTCOME_UNKNOWN") {
				t.Fatalf("exit/output = %d, %q", exit, output)
			}
			if targetCalls != 0 {
				t.Fatalf("redirected target calls = %d, want 0", targetCalls)
			}
		})
	}
}

// Suite r101 Q4: a read whose --out failed said "may have succeeded".
func TestRelayReadFailureSaysNothingChanged(t *testing.T) {
	for name, testCase := range map[string]struct {
		endpoint    string
		args        []string
		wantUnknown bool
	}{
		"core GET":   {"core", []string{"--method", "GET", "--path", "/api/states"}, false},
		"ws read":    {"ws", []string{"--data", `{"type":"config/area_registry/list"}`}, false},
		"ws write":   {"ws", []string{"--data", `{"type":"config/area_registry/create","name":"Kitchen"}`}, true},
		"core write": {"core", []string{"--method", "POST", "--path", "/api/template", "--body", `{"template":"x"}`}, true},
	} {
		t.Run(name, func(t *testing.T) {
			paths, _ := setupStrictInputRelay(t)
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(response, `{"ok":true,"data":[]}`)
			}))
			t.Cleanup(server.Close)
			cfg, err := loadConfig(paths)
			if err != nil {
				t.Fatal(err)
			}
			cfg.RelayBaseURL = server.URL
			if err := saveConfig(paths, cfg); err != nil {
				t.Fatal(err)
			}
			blocked := t.TempDir() + "/not-a-directory"
			if err := os.WriteFile(blocked, []byte("file"), 0o600); err != nil {
				t.Fatal(err)
			}
			out := blocked + "/out.json"
			exit, output := captureCommandOutput(t, func() int {
				return runRelayProxy(paths, testCase.endpoint, append(testCase.args, "--out", out))
			})
			if exit != 1 || !strings.Contains(output, "writing --out") {
				t.Fatalf("exit/output = %d, %q", exit, output)
			}
			if got := strings.Contains(output, "OUTCOME_UNKNOWN"); got != testCase.wantUnknown {
				t.Fatalf("unknown = %v, output = %q", got, output)
			}
			if !testCase.wantUnknown && !strings.Contains(output, "READ_FAILED") || !testCase.wantUnknown && !strings.Contains(output, "nothing in Home Assistant was changed") {
				t.Fatalf("read failure wording missing: %q", output)
			}
		})
	}
}

func TestRawRelayReadClassifiesOnlyReads(t *testing.T) {
	for body, want := range map[string]bool{
		`{"method":"get","path":"/api/states"}`:                                true,
		`{"method":"POST","path":"/api/template"}`:                             false,
		`{"path":"/api/states"}`:                                               false,
		`{"type":"get_states"}`:                                                true,
		`{"type":"input_boolean/create","name":"x"}`:                           false,
		`{"type":"nope"}`:                                                      false,
		`{"message":{"type":"subscribe_events","event_type":"state_changed"}}`: true,
		`{"message":{"type":"input_boolean/delete"}}`:                          false,
		`{"type":"get_states","message":{"type":"input_boolean/delete"}}`:      false,
		`{"type":"input_boolean/delete","message":{"type":"get_states"}}`:      false,
		`not json`: false,
	} {
		endpoint := "ws"
		if strings.Contains(body, "path") {
			endpoint = "core"
		}
		if got := rawRelayRead(endpoint, []byte(body)); got != want {
			t.Errorf("rawRelayRead(%s, %s) = %v, want %v", endpoint, body, got, want)
		}
	}
}
