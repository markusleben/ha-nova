package main

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// F2 regression: on paired installs the health command must honor its own
// connect/max timeouts, applied to the SPKI-pinned transport, instead of the
// fixed pairing timeout — while preserving the pin configuration.
func TestApplyHealthTimeoutsRewritesPinnedClient(t *testing.T) {
	client := spkiPinnedClient("test-pin")
	applyHealthTimeouts(client, 3, 9)

	if client.Timeout != 9*time.Second {
		t.Fatalf("want 9s overall timeout, got %v", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.DialContext == nil {
		t.Fatal("want a dial timeout applied to the pinned transport")
	}
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.MinVersion != tls.VersionTLS13 {
		t.Fatal("applyHealthTimeouts must preserve the SPKI pin TLS configuration")
	}
}

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

func TestParseHealthFlagsDefaultsAndOverrides(t *testing.T) {
	opts, err := parseHealthFlags(nil)
	if err != nil {
		t.Fatalf("parseHealthFlags(nil) error: %v", err)
	}
	if opts.ConnectTimeoutSeconds != defaultRelayConnectTimeoutSeconds || opts.MaxTimeSeconds != defaultRelayMaxTimeSeconds {
		t.Fatalf("defaults = %+v, want connect %d / max %d", opts, defaultRelayConnectTimeoutSeconds, defaultRelayMaxTimeSeconds)
	}

	// curl-compatible flags from the session-start hook must take effect.
	opts, err = parseHealthFlags([]string{"--connect-timeout", "1", "--max-time", "2"})
	if err != nil {
		t.Fatalf("parseHealthFlags(overrides) error: %v", err)
	}
	if opts.ConnectTimeoutSeconds != 1 || opts.MaxTimeSeconds != 2 {
		t.Fatalf("overrides = %+v, want connect 1 / max 2", opts)
	}

	if _, err := parseHealthFlags([]string{"--max-time", "0"}); err == nil {
		t.Fatal("parseHealthFlags(--max-time 0) expected error, got nil")
	}
}

func TestReadAllLimitedEnforcesCeiling(t *testing.T) {
	under, err := readAllLimited(strings.NewReader("abc"), 3)
	if err != nil || string(under) != "abc" {
		t.Fatalf("readAllLimited(at limit) = %q, %v; want abc, nil", under, err)
	}

	if _, err := readAllLimited(strings.NewReader("abcd"), 3); err == nil {
		t.Fatal("readAllLimited(over limit) expected error, got nil")
	} else if !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("readAllLimited(over limit) error = %v, want mention of exceeded limit", err)
	}
}

func TestShouldWarnRelayOutdatedThrottlesRepeatWarnings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if !shouldWarnRelayOutdated(paths) {
		t.Fatal("first shouldWarnRelayOutdated() = false, want true")
	}
	if shouldWarnRelayOutdated(paths) {
		t.Fatal("second shouldWarnRelayOutdated() = true, want throttled false")
	}
}

func TestRelayProxyWarnsOnOutdatedRelayVersionHeader(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".test-relay-token"))

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(relayVersionHeader, "0.1.0")
		w.Header().Set("content-type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true,"data":{"status":200,"body":{}}}`)
	}))
	t.Cleanup(relayServer.Close)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.8.0","min_relay_version":"0.2.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
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

	exitCode, output := captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "core", []string{"--method", "GET", "--path", "/api/states"})
	})
	if exitCode != 0 {
		t.Fatalf("runRelayProxy() exit = %d, want 0", exitCode)
	}
	if !strings.Contains(output, "Relay outdated: v0.1.0 is below minimum v0.2.0") {
		t.Fatalf("expected outdated-relay warning in output, got: %q", output)
	}

	// Second call within the throttle window stays quiet.
	exitCode, output = captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "core", []string{"--method", "GET", "--path", "/api/states"})
	})
	if exitCode != 0 {
		t.Fatalf("second runRelayProxy() exit = %d, want 0", exitCode)
	}
	if strings.Contains(output, "Relay outdated") {
		t.Fatalf("expected throttled second call without warning, got: %q", output)
	}
}

// --out-binary decodes the base64 body the relay marks with
// body_encoding: "base64" (camera frames). A missing marker must fail loudly:
// silently writing the JSON envelope would hand the caller a "JPEG" full of
// JSON.
func TestWriteRelayBinaryBodyDecodesMarkedBase64(t *testing.T) {
	jpeg := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10}
	envelope := []byte(`{"ok":true,"data":{"status":200,"body":"` +
		base64.StdEncoding.EncodeToString(jpeg) +
		`","body_encoding":"base64","content_type":"image/jpeg"}}`)

	out := filepath.Join(t.TempDir(), "nested", "frame.jpg")
	if err := writeRelayBinaryBody(envelope, out); err != nil {
		t.Fatalf("writeRelayBinaryBody() error: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read decoded file: %v", err)
	}
	if !bytes.Equal(got, jpeg) {
		t.Fatalf("decoded bytes = %x, want %x", got, jpeg)
	}
}

func TestWriteRelayBinaryBodyRejectsUnmarkedBody(t *testing.T) {
	envelope := []byte(`{"ok":true,"data":{"status":200,"body":{"state":"on"}}}`)
	err := writeRelayBinaryBody(envelope, filepath.Join(t.TempDir(), "x.bin"))
	if err == nil {
		t.Fatal("expected an error for a non-binary body")
	}
	if !strings.Contains(err.Error(), "--out") {
		t.Fatalf("error should point at --out, got: %v", err)
	}
}

func TestParseRelayFlagsRejectsBinaryOutWithFilters(t *testing.T) {
	if _, err := parseRelayFlags("core", []string{
		"--method", "GET", "--path", "/api/camera_proxy/camera.front",
		"--out-binary", "f.jpg", "--jq", ".data",
	}); err == nil {
		t.Fatal("expected --out-binary + --jq to be rejected")
	}
	if _, err := parseRelayFlags("core", []string{
		"--method", "GET", "--path", "/api/camera_proxy/camera.front",
		"--out-binary", "f.jpg", "--out", "f.json",
	}); err == nil {
		t.Fatal("expected --out-binary + --out to be rejected")
	}
}
