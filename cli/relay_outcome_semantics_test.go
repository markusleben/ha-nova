package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
					[]string{"--data", `{"type":"ping"}`},
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
