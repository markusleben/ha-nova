package main

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

type traceFailingReadCloser struct {
	err error
}

func (r traceFailingReadCloser) Read([]byte) (int, error) {
	return 0, r.err
}

func (traceFailingReadCloser) Close() error {
	return nil
}

// Regression: `ha-nova trace` must route through the paired device transport, not
// read the legacy relay token directly — so it works on a passwordless install
// and fails closed (re-pair guidance) when a paired credential is missing,
// instead of failing on an absent legacy token or using the wrong transport.
func TestRelayWSJSONRoutesThroughPairedTransport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir()) // no device credential stored
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	paths := runtimePaths{ConfigFile: filepath.Join(home, "config.json")}
	if err := saveConfig(paths, runtimeConfig{
		RelayBaseURL:       "http://192.168.1.5:8791",
		RelaySecureBaseURL: "https://192.168.1.5:8792",
		RelaySpkiPin:       "pin",
	}); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	_, err := relayWSJSON(paths, map[string]any{"type": "ping"})
	if err == nil {
		t.Fatal("expected trace to fail closed on a paired config with no credential")
	}
	if msg := err.Error(); !strings.Contains(msg, "re-pair") && !strings.Contains(msg, "device credential") {
		t.Fatalf("trace did not route through the paired transport: %v", err)
	}
}

func TestReadRelayWSJSONResponseClassifiesUncertainResults(t *testing.T) {
	for name, testCase := range map[string]struct {
		status   int
		body     io.ReadCloser
		maxBytes int64
	}{
		"body read failure": {
			status: http.StatusOK,
			body: traceFailingReadCloser{
				err: errors.New("read failed"),
			},
			maxBytes: 64,
		},
		"oversized body": {
			status:   http.StatusOK,
			body:     io.NopCloser(strings.NewReader("four")),
			maxBytes: 3,
		},
		"malformed JSON": {
			status:   http.StatusOK,
			body:     io.NopCloser(strings.NewReader(`{"ok":`)),
			maxBytes: 64,
		},
		"missing result marker": {
			status:   http.StatusOK,
			body:     io.NopCloser(strings.NewReader(`{"data":{}}`)),
			maxBytes: 64,
		},
	} {
		t.Run(name, func(t *testing.T) {
			response := &http.Response{
				StatusCode: testCase.status,
				Body:       testCase.body,
			}
			_, err := readRelayWSJSONResponse(response, testCase.maxBytes)
			if err == nil || !strings.Contains(err.Error(), "OUTCOME_UNKNOWN") {
				t.Fatalf("error = %v, want OUTCOME_UNKNOWN", err)
			}
		})
	}
}

func TestReadRelayWSJSONResponsePreservesCloudOutcomeUnknown(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body: &cloudIngressLimitedBody{
			ReadCloser: traceFailingReadCloser{
				err: errors.New("ingress_session=must-not-leak"),
			},
			remaining:        64,
			outcomeSensitive: true,
		},
	}
	_, err := readRelayWSJSONResponse(response, 64)
	if !IsCloudErrorCode(err, CloudErrOutcomeUnknown) {
		t.Fatalf("error = %v, want %s", err, CloudErrOutcomeUnknown)
	}
	if strings.Contains(err.Error(), "must-not-leak") {
		t.Fatalf("error leaked transport detail: %v", err)
	}
}

func TestReadRelayWSJSONResponseKeepsExplicitFailureDefinitive(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			`{"ok":false,"error":{"message":"trace unavailable"}}`,
		)),
	}
	_, err := readRelayWSJSONResponse(response, 128)
	if err == nil || strings.Contains(err.Error(), "OUTCOME_UNKNOWN") {
		t.Fatalf("error = %v, want definitive Relay failure", err)
	}
}
