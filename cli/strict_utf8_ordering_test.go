package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRelayRejectsExplicitEmptyJQSelectionsBeforeRequest(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"type":"ping"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, arg := range []string{"--jq=", "--jq-file="} {
		t.Run(arg, func(t *testing.T) {
			exitCode, output := captureCommandOutput(t, func() int {
				return runRelayProxy(paths, "ws", []string{"--data-file", payloadPath, arg})
			})
			if exitCode != 1 || !strings.Contains(output, "nothing was sent") {
				t.Fatalf("exit/output = %d, %q", exitCode, output)
			}
			if got := capture.requests.Load(); got != 0 {
				t.Fatalf("request count = %d, want 0", got)
			}
		})
	}
}

func TestRelayRejectsEmptyPayloadAndOutputSelectionsBeforeRequest(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	cases := map[string]struct {
		endpoint string
		args     []string
	}{
		"data":       {endpoint: "ws", args: []string{"--data="}},
		"data file":  {endpoint: "ws", args: []string{"--data-file="}},
		"body":       {endpoint: "core", args: []string{"--method", "POST", "--path", "/api/test", "--body="}},
		"body file":  {endpoint: "core", args: []string{"--method", "POST", "--path", "/api/test", "--body-file="}},
		"output":     {endpoint: "ws", args: []string{"--out="}},
		"binary out": {endpoint: "core", args: []string{"--method", "GET", "--path", "/api/test", "--out-binary="}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			exitCode, output := captureCommandOutput(t, func() int {
				return runRelayProxy(paths, tc.endpoint, tc.args)
			})
			if exitCode != 1 || !strings.Contains(output, "nothing was sent") {
				t.Fatalf("exit/output = %d, %q", exitCode, output)
			}
			if got := capture.requests.Load(); got != 0 {
				t.Fatalf("request count = %d, want 0", got)
			}
		})
	}
}

func TestRelayRejectsPositionalArgumentsBeforeRequest(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	cases := map[string]struct {
		endpoint string
		args     []string
	}{
		"ws": {endpoint: "ws", args: []string{"unexpected", "--data", `{"type":"ping"}`}},
		"core": {
			endpoint: "core",
			args:     []string{"--method", "DELETE", "--path", "/api/test", "unexpected", "--body", `{}`},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			exitCode, output := captureCommandOutput(t, func() int {
				return runRelayProxy(paths, tc.endpoint, tc.args)
			})
			if exitCode != 1 || !strings.Contains(output, "does not accept positional arguments") {
				t.Fatalf("exit/output = %d, %q", exitCode, output)
			}
			if got := capture.requests.Load(); got != 0 {
				t.Fatalf("request count = %d, want 0", got)
			}
		})
	}
}

func TestRelayCoreEnvelopeUsesJSONEscaping(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	wantPath := "/api/control-\x01"
	exitCode, output := captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "core", []string{"--method", "POST", "--path", wantPath})
	})
	if exitCode != 0 {
		t.Fatalf("exit/output = %d, %q", exitCode, output)
	}
	var body []byte
	select {
	case body = <-capture.bodies:
	default:
		t.Fatal("request body was not captured")
	}
	if !json.Valid(body) {
		t.Fatalf("core envelope is not valid JSON: %q", body)
	}
	var envelope struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Path != wantPath {
		t.Fatalf("path = %q, want %q", envelope.Path, wantPath)
	}
}

func TestRelayCoreRejectsInvalidUTF8PathBeforeRequest(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	paths.ConfigFile = filepath.Join(t.TempDir(), "missing-config.json")
	invalidPath := string([]byte{'/', 0xDC})
	exitCode, output := captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "core", []string{"--method", "GET", "--path", invalidPath})
	})
	if exitCode != 1 || !strings.Contains(output, "core path is not valid UTF-8") || !strings.Contains(output, "nothing was sent") {
		t.Fatalf("exit/output = %d, %q", exitCode, output)
	}
	if got := capture.requests.Load(); got != 0 {
		t.Fatalf("request count = %d, want 0", got)
	}
}

func TestSnapshotAndJQRejectEmptyFileSelectors(t *testing.T) {
	paths := testSnapshotPaths(t)
	exitCode, output := captureCommandOutput(t, func() int {
		return runSnapshotSave(paths, []string{"--data-file="})
	})
	if exitCode != 1 || !strings.Contains(output, "non-empty path") {
		t.Fatalf("snapshot exit/output = %d, %q", exitCode, output)
	}

	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"input":       {args: []string{"--file", "", "."}, want: "--file requires a non-empty path"},
		"filter":      {args: []string{"--jq-file", ""}, want: "--jq-file requires a non-empty path"},
		"positionals": {args: []string{".", "ignored"}, want: "Usage: relay jq"},
	} {
		t.Run(name, func(t *testing.T) {
			exitCode, output := captureCommandOutput(t, func() int { return runJQ(tc.args) })
			if exitCode != 1 || !strings.Contains(output, tc.want) {
				t.Fatalf("jq exit/output = %d, %q", exitCode, output)
			}
		})
	}
}

func TestRelayInputPreflightWinsBeforeMissingCredential(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, "missing-token"))
	t.Setenv("HA_NOVA_NO_UPDATE_NUDGE", "1")
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(paths, runtimeConfig{
		HAHost:       "127.0.0.1",
		HAURL:        "http://127.0.0.1:8123",
		RelayBaseURL: "http://127.0.0.1:1",
	}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	invalidPayloadPath := filepath.Join(dir, "invalid.json")
	validPayloadPath := filepath.Join(dir, "valid.json")
	if err := os.WriteFile(invalidPayloadPath, append([]byte(`{"title":"`), 0xDC), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(validPayloadPath, []byte(`{"type":"ping"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		args []string
		want string
	}{
		"payload": {args: []string{"--data-file", invalidPayloadPath}, want: "not valid UTF-8"},
		"jq":      {args: []string{"--data-file", validPayloadPath, "--jq", ".["}, want: "jq parse error"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			exitCode, output := captureCommandOutput(t, func() int {
				return runRelayProxy(paths, "ws", tc.args)
			})
			if exitCode != 1 || !strings.Contains(output, tc.want) || !strings.Contains(output, "nothing was sent") {
				t.Fatalf("exit/output = %d, %q", exitCode, output)
			}
		})
	}
}

func TestRelayRuntimeJQFailureWarnsAgainstRetry(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"type":"ping"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "ws", []string{"--data-file", payloadPath, "--jq", `error("after-write")`})
	})
	if exitCode != 1 || !strings.Contains(output, "already sent") || !strings.Contains(output, "do not retry automatically") {
		t.Fatalf("exit/output = %d, %q", exitCode, output)
	}
	if got := capture.requests.Load(); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}
}

func TestRelayOutputFailureWarnsAgainstRetry(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "payload.json")
	blockedParent := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(payloadPath, []byte(`{"type":"ping"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blockedParent, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "ws", []string{
			"--data-file", payloadPath,
			"--out", filepath.Join(blockedParent, "response.json"),
		})
	})
	if exitCode != 1 || !strings.Contains(output, "already sent") || !strings.Contains(output, "do not retry automatically") {
		t.Fatalf("exit/output = %d, %q", exitCode, output)
	}
	if got := capture.requests.Load(); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}
}

func TestRelayUnknownOutcomeWarnsAgainstRetry(t *testing.T) {
	paths, _ := setupStrictInputRelay(t)
	var requests atomic.Int32
	dropped := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		_, _ = io.Copy(io.Discard, request.Body)
		connection, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = connection.Close()
		}
	}))
	t.Cleanup(dropped.Close)
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RelayBaseURL = dropped.URL
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "ws", []string{"--data", `{"type":"config/area_registry/create","name":"Kitchen"}`})
	})
	if exitCode != 1 {
		t.Fatalf("exit/output = %d, %q", exitCode, output)
	}
	for _, want := range []string{"outcome is unknown", "may have reached the Relay", "do not retry automatically"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %s", want, output)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("received requests = %d, want 1", got)
	}
}
