package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestStrictJSONBytesPreservesUnicodeAndAcceptsOneBOM(t *testing.T) {
	valid := []byte("{\"title\":\"Übersicht ☕ \uFFFD \uFEFF\"}")

	for name, input := range map[string][]byte{
		"plain": valid,
		"bom":   append(append([]byte{}, utf8BOM...), valid...),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := strictJSONBytes(input, "test JSON")
			if err != nil {
				t.Fatalf("strictJSONBytes() error: %v", err)
			}
			if !bytes.Equal(got, valid) {
				t.Fatalf("bytes changed:\n got %x\nwant %x", got, valid)
			}
		})
	}
}

func TestStrictJSONBytesRejectsInvalidEncodingsBeforeJSON(t *testing.T) {
	cases := map[string][]byte{
		"active legacy code page byte": append(append([]byte(`{"title":"`), 0xDC), []byte(`bersicht"}`)...),
		"truncated UTF-8":              append([]byte(`{"title":"`), 0xC3),
		"UTF-16LE":                     {0xFF, 0xFE, '{', 0x00, '}', 0x00},
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := strictJSONBytes(input, "test JSON")
			if err == nil || !strings.Contains(err.Error(), "not valid UTF-8") {
				t.Fatalf("error = %v, want UTF-8 rejection", err)
			}
		})
	}

	for name, input := range map[string][]byte{
		"malformed JSON": []byte(`{"title":`),
		"empty":          nil,
		"BOM only":       append([]byte{}, utf8BOM...),
		"double BOM":     append(append(append([]byte{}, utf8BOM...), utf8BOM...), []byte(`{}`)...),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := strictJSONBytes(input, "test JSON")
			if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
				t.Fatalf("error = %v, want JSON rejection", err)
			}
		})
	}
}

func TestLoadRelayPayloadExplainsWindowsEncodingWithoutSending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payload.json")
	invalid := append(append([]byte(`{"title":"`), 0xDC), []byte(`bersicht"}`)...)
	if err := os.WriteFile(path, invalid, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := loadRelayPayload(relayRequestOptions{JSONFile: path})
	if err == nil {
		t.Fatal("expected invalid UTF-8 to fail")
	}
	for _, want := range []string{path, "not valid UTF-8", "nothing was sent", "Set-Content -Encoding UTF8", "BOM is accepted"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q: %v", want, err)
		}
	}
}

func TestLoadRelayPayloadStripsBOMAndPreservesValidBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payload.json")
	want := []byte("{\"title\":\"Übersicht ☕ \uFFFD \uFEFF\"}")
	if err := os.WriteFile(path, append(append([]byte{}, utf8BOM...), want...), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := loadRelayPayload(relayRequestOptions{JSONFile: path})
	if err != nil {
		t.Fatalf("loadRelayPayload() error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("payload changed:\n got %x\nwant %x", got, want)
	}
}

func TestRelayFileInputsRejectInvalidUTF8BeforeEveryRequest(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	path := filepath.Join(t.TempDir(), "payload.json")
	invalid := append(append([]byte(`{"title":"`), 0xDC), []byte(`bersicht"}`)...)
	if err := os.WriteFile(path, invalid, 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		endpoint string
		args     []string
	}{
		{endpoint: "ws", args: []string{"--data-file", path}},
		{endpoint: "files", args: []string{"--data-file", path}},
		{endpoint: "backups", args: []string{"--data-file", path}},
		{endpoint: "core", args: []string{"--method", "POST", "--path", "/api/test", "--body-file", path}},
	}
	for _, tc := range cases {
		t.Run(tc.endpoint, func(t *testing.T) {
			before := capture.requests.Load()
			exitCode, output := captureCommandOutput(t, func() int {
				return runRelayProxy(paths, tc.endpoint, tc.args)
			})
			if exitCode != 1 {
				t.Fatalf("exit = %d, want 1", exitCode)
			}
			if !strings.Contains(output, "not valid UTF-8") || !strings.Contains(output, "nothing was sent") {
				t.Fatalf("unexpected output: %s", output)
			}
			if got := capture.requests.Load(); got != before {
				t.Fatalf("request count = %d, want %d", got, before)
			}
		})
	}
}

func TestRelayRejectsInvalidUTF8BeforeConfigLookup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.json")
	if err := os.WriteFile(path, append([]byte(`{"title":"`), 0xDC), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := runtimePaths{ConfigFile: filepath.Join(dir, "missing-config.json")}

	exitCode, output := captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "ws", []string{"--data-file", path})
	})
	if exitCode != 1 || !strings.Contains(output, "not valid UTF-8") || !strings.Contains(output, "nothing was sent") {
		t.Fatalf("exit/output = %d, %q", exitCode, output)
	}
}

func TestRelayRejectsMalformedJSONBeforeRequest(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	path := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(path, []byte(`{"title":`), 0o600); err != nil {
		t.Fatal(err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "ws", []string{"--data-file", path})
	})
	if exitCode != 1 || !strings.Contains(output, "not valid JSON") || !strings.Contains(output, "nothing was sent") {
		t.Fatalf("exit/output = %d, %q", exitCode, output)
	}
	if got := capture.requests.Load(); got != 0 {
		t.Fatalf("request count = %d, want 0", got)
	}
}

func TestRelayValidFilePayloadReachesNetworkWithoutTranscoding(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	payload := []byte("{\"title\":\"Übersicht ☕ \uFFFD \uFEFF\"}")
	if err := os.WriteFile(payloadPath, append(append([]byte{}, utf8BOM...), payload...), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		endpoint string
		args     []string
		want     []byte
	}{
		{name: "ws", endpoint: "ws", args: []string{"--data-file", payloadPath}, want: payload},
		{
			name:     "core",
			endpoint: "core",
			args:     []string{"--method", "POST", "--path", "/api/test", "--body-file", payloadPath},
			want:     append(append([]byte(`{"method":"POST","path":"/api/test","body":`), payload...), '}'),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exitCode, output := captureCommandOutput(t, func() int {
				return runRelayProxy(paths, tc.endpoint, tc.args)
			})
			if exitCode != 0 {
				t.Fatalf("exit/output = %d, %q", exitCode, output)
			}
			select {
			case got := <-capture.bodies:
				if !bytes.Equal(got, tc.want) {
					t.Fatalf("request body changed:\n got %x\nwant %x", got, tc.want)
				}
			default:
				t.Fatal("request body was not captured")
			}
		})
	}
}

func TestRelayPreflightsJQFileAndSyntaxBeforeRequest(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"type":"ping"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	invalidUTF8Path := filepath.Join(dir, "invalid.jq")
	if err := os.WriteFile(invalidUTF8Path, []byte{'.', 0xDC}, 0o600); err != nil {
		t.Fatal(err)
	}
	invalidSyntaxPath := filepath.Join(dir, "syntax.jq")
	if err := os.WriteFile(invalidSyntaxPath, []byte(`.[`), 0o600); err != nil {
		t.Fatal(err)
	}
	emptyPath := filepath.Join(dir, "empty.jq")
	if err := os.WriteFile(emptyPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	whitespacePath := filepath.Join(dir, "whitespace.jq")
	if err := os.WriteFile(whitespacePath, []byte(" \r\n\t"), 0o600); err != nil {
		t.Fatal(err)
	}
	bomOnlyPath := filepath.Join(dir, "bom-only.jq")
	if err := os.WriteFile(bomOnlyPath, append([]byte{}, utf8BOM...), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, filterPath := range map[string]string{
		"invalid UTF-8":  invalidUTF8Path,
		"invalid syntax": invalidSyntaxPath,
		"missing file":   filepath.Join(dir, "missing.jq"),
		"empty":          emptyPath,
		"whitespace":     whitespacePath,
		"BOM only":       bomOnlyPath,
	} {
		t.Run(name, func(t *testing.T) {
			before := capture.requests.Load()
			exitCode, output := captureCommandOutput(t, func() int {
				return runRelayProxy(paths, "ws", []string{"--data-file", payloadPath, "--jq-file", filterPath})
			})
			if exitCode != 1 || !strings.Contains(output, "nothing was sent") {
				t.Fatalf("exit/output = %d, %q", exitCode, output)
			}
			if got := capture.requests.Load(); got != before {
				t.Fatalf("request count = %d, want %d", got, before)
			}
		})
	}
}

func TestRunJQHandlesStrictUTF8OnCommandPaths(t *testing.T) {
	dir := t.TempDir()
	validInputPath := filepath.Join(dir, "valid.json")
	validFilterPath := filepath.Join(dir, "valid.jq")
	if err := os.WriteFile(validInputPath, append(append([]byte{}, utf8BOM...), []byte(`{"title":"Übersicht ☕"}`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(validFilterPath, append(append([]byte{}, utf8BOM...), []byte(`.title`)...), 0o600); err != nil {
		t.Fatal(err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runJQ([]string{"-r", "--file", validInputPath, "--jq-file", validFilterPath})
	})
	if exitCode != 0 || output != "Übersicht ☕\n" {
		t.Fatalf("exit/output = %d, %q", exitCode, output)
	}

	invalidInputPath := filepath.Join(dir, "invalid.json")
	invalidFilterPath := filepath.Join(dir, "invalid.jq")
	if err := os.WriteFile(invalidInputPath, append([]byte(`{"title":"`), 0xDC), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invalidFilterPath, []byte{'.', 0xDC}, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"input file":  {"--file", invalidInputPath, "."},
		"filter file": {"--file", validInputPath, "--jq-file", invalidFilterPath},
	} {
		t.Run(name, func(t *testing.T) {
			exitCode, output := captureCommandOutput(t, func() int { return runJQ(args) })
			if exitCode != 1 || !strings.Contains(output, "not valid UTF-8") {
				t.Fatalf("exit/output = %d, %q", exitCode, output)
			}
		})
	}
}

func TestRunJQRejectsInvalidUTF8FromStdin(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	invalid := append(append([]byte(`{"title":"`), 0xDC), []byte(`bersicht"}`)...)
	if _, err := writer.Write(invalid); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	originalStdin := os.Stdin
	os.Stdin = reader
	t.Cleanup(func() {
		os.Stdin = originalStdin
		_ = reader.Close()
	})

	exitCode, output := captureCommandOutput(t, func() int { return runJQ([]string{"."}) })
	if exitCode != 1 || !strings.Contains(output, "jq stdin is not valid UTF-8") {
		t.Fatalf("exit/output = %d, %q", exitCode, output)
	}
}

func TestSnapshotDiffAndJQRejectInvalidUTF8(t *testing.T) {
	invalidObject := append(append([]byte(`{"title":"`), 0xDC), []byte(`bersicht"}`)...)
	if _, err := configObjectFromBytes(invalidObject); err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("config error = %v, want UTF-8 rejection", err)
	}
	if _, err := applyJQFilter(".", invalidObject, false); err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("jq error = %v, want UTF-8 rejection", err)
	}

	paths := testSnapshotPaths(t)
	invalidSnapshot := append(append([]byte(`{"op":"update","domain":"automation","target_id":"1","before_config":{"alias":"`), 0xDC), []byte(`"},"expected_after":{"alias":"ok"}}`)...)
	if err := saveUndoSnapshotBytes(paths, invalidSnapshot); err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("snapshot error = %v, want UTF-8 rejection", err)
	}
	if _, err := os.Stat(undoSnapshotStackPath(paths)); !os.IsNotExist(err) {
		t.Fatalf("invalid snapshot must not be written: %v", err)
	}
}

func TestStoredUndoSnapshotRejectsInvalidUTF8(t *testing.T) {
	paths := testSnapshotPaths(t)
	invalid := append(append([]byte(`{"schema_version":2,"snapshots":[{"op":"update","domain":"automation","target_id":"1","before_config":{"alias":"`), 0xDC), []byte(`"},"expected_after":{"alias":"ok"}}]}`)...)
	if err := os.WriteFile(undoSnapshotStackPath(paths), invalid, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadUndoSnapshotStack(paths)
	if err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("error = %v, want corrupt UTF-8 rejection", err)
	}
}

type strictInputRelayCapture struct {
	requests atomic.Int32
	bodies   chan []byte
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
