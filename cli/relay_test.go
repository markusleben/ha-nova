package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRelayPayloadAcceptsSingleQuotedInlineJSON(t *testing.T) {
	payload, err := loadRelayPayload(relayRequestOptions{
		InlineJSON: `'{"type":"ping"}'`,
	})
	if err != nil {
		t.Fatalf("loadRelayPayload() error: %v", err)
	}
	if string(payload) != `{"type":"ping"}` {
		t.Fatalf("payload = %q", string(payload))
	}
}

func TestLoadRelayPayloadAcceptsDoubleQuotedWrappedJSON(t *testing.T) {
	payload, err := loadRelayPayload(relayRequestOptions{
		InlineJSON: `"{\"type\":\"ping\"}"`,
	})
	if err != nil {
		t.Fatalf("loadRelayPayload() error: %v", err)
	}
	if string(payload) != `"{\"type\":\"ping\"}"` {
		t.Fatalf("payload = %q", string(payload))
	}

	payload, err = loadRelayPayload(relayRequestOptions{
		InlineJSON: `"{"type":"ping"}"`,
	})
	if err != nil {
		t.Fatalf("loadRelayPayload() error: %v", err)
	}
	if string(payload) != `{"type":"ping"}` {
		t.Fatalf("payload = %q", string(payload))
	}
}

func TestLoadRelayPayloadKeepsValidPrimitiveJSON(t *testing.T) {
	payload, err := loadRelayPayload(relayRequestOptions{
		InlineJSON: `true`,
	})
	if err != nil {
		t.Fatalf("loadRelayPayload() error: %v", err)
	}
	if string(payload) != `true` {
		t.Fatalf("payload = %q", string(payload))
	}
}

func TestLoadRelayPayloadRejectsInvalidInlineJSONLocally(t *testing.T) {
	_, err := loadRelayPayload(relayRequestOptions{
		InlineJSON: `{type:"ping"}`,
	})
	if err == nil {
		t.Fatal("expected invalid inline JSON to fail locally")
	}
	if !strings.Contains(err.Error(), "inline JSON payload is not valid JSON") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyJQFilterAcceptsSingleQuotedWrappedFilter(t *testing.T) {
	res, err := applyJQFilter(`'.data.unique_id'`, []byte(`{"data":{"unique_id":"abc123"}}`), true)
	if err != nil {
		t.Fatalf("applyJQFilter() error: %v", err)
	}
	if strings.TrimSpace(res.output) != "abc123" {
		t.Fatalf("unexpected output: %q", res.output)
	}
}

func TestApplyJQFilterAcceptsWrappedRegexFilter(t *testing.T) {
	input := `{"data":{"entities":[{"ei":"light.kitchen"},{"ei":"switch.kitchen"}]}}`
	filter := `"[.data.entities[] | select(.ei | test(\"^light\\\\.\")) | .ei]"`

	res, err := applyJQFilter(filter, []byte(input), false)
	if err != nil {
		t.Fatalf("applyJQFilter() error: %v", err)
	}
	if !strings.Contains(res.output, "light.kitchen") {
		t.Fatalf("unexpected output: %q", res.output)
	}
	if strings.Contains(res.output, "switch.kitchen") {
		t.Fatalf("unexpected output: %q", res.output)
	}
}

func TestRelayCoreReturnsNonzeroForUpstream5xx(t *testing.T) {
	paths := setupRelayProxyTest(t, 503)

	exitCode, _ := captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "core", []string{"--method", "GET", "--path", "/api/states"})
	})

	if exitCode != 1 {
		t.Fatalf("runRelayProxy() exit = %d, want 1", exitCode)
	}
}

func TestRelayCoreAllowsUpstream404ByDefault(t *testing.T) {
	paths := setupRelayProxyTest(t, 404)

	exitCode, _ := captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "core", []string{"--method", "GET", "--path", "/api/states/missing"})
	})

	if exitCode != 0 {
		t.Fatalf("runRelayProxy() exit = %d, want 0", exitCode)
	}
}

func TestRelayCoreStrictStatusReturnsNonzeroForUpstream404(t *testing.T) {
	paths := setupRelayProxyTest(t, 404)

	exitCode, _ := captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "core", []string{"--method", "GET", "--path", "/api/states/missing", "--strict-status"})
	})

	if exitCode != 1 {
		t.Fatalf("runRelayProxy() exit = %d, want 1", exitCode)
	}
}

func TestRunSetupFailsLoudlyForCorruptStateFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.StateFile), 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.WriteFile(paths.StateFile, []byte(`{not-json`), 0o600); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{"--non-interactive", "--host", "127.0.0.1", "--relay-token", "test", "claude"})
	})

	if exitCode != 1 {
		t.Fatalf("runSetup() exit = %d, want 1", exitCode)
	}
	for _, want := range []string{"state file is corrupt", paths.StateFile, "rerun setup"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestLoadStateOrDefaultCheckedTreatsMissingStateAsFirstRun(t *testing.T) {
	paths := runtimePaths{StateFile: filepath.Join(t.TempDir(), "state.json")}

	state, err := loadStateOrDefaultChecked(paths)
	if err != nil {
		t.Fatalf("loadStateOrDefaultChecked() error: %v", err)
	}
	if state.SchemaVersion != stateSchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", state.SchemaVersion, stateSchemaVersion)
	}
	if state.ClientInstallModes == nil {
		t.Fatal("ClientInstallModes must be initialized")
	}
}

func setupRelayProxyTest(t *testing.T, upstreamStatus int) runtimePaths {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".test-relay-token"))

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/core" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = fmt.Fprintf(w, `{"ok":true,"data":{"status":%d,"body":{"message":"upstream status"}}}`, upstreamStatus)
	}))
	t.Cleanup(relayServer.Close)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := saveConfig(paths, runtimeConfig{
		HAHost:       "127.0.0.1",
		HAURL:        "http://127.0.0.1:8123",
		RelayBaseURL: relayServer.URL,
	}); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("test-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}
	return paths
}
