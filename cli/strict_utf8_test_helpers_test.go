package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
)

type strictInputRelayCapture struct {
	requests atomic.Int32
	bodies   chan []byte
	response func([]byte) []byte
}

func setupStrictInputRelay(t *testing.T) (runtimePaths, *strictInputRelayCapture) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".test-relay-token"))
	t.Setenv("HA_NOVA_NO_UPDATE_NUDGE", "1")

	capture := &strictInputRelayCapture{bodies: make(chan []byte, 8)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.requests.Add(1)
		body, _ := io.ReadAll(r.Body)
		capture.bodies <- body
		w.Header().Set("content-type", "application/json")
		if capture.response != nil {
			_, _ = w.Write(capture.response(body))
			return
		}
		_, _ = fmt.Fprint(w, `{"ok":true,"data":{}}`)
	}))
	t.Cleanup(server.Close)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths: %v", err)
	}
	if err := saveConfig(paths, runtimeConfig{
		HAHost:       "127.0.0.1",
		HAURL:        "http://127.0.0.1:8123",
		RelayBaseURL: server.URL,
	}); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}
	if err := writeRelayAuthToken("test-token"); err != nil {
		t.Fatalf("write relay token: %v", err)
	}
	return paths, capture
}
