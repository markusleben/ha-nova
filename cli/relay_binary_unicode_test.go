package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRelayBinaryOutputValidatesEnvelopeBeforeWriting(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	outputPath := filepath.Join(t.TempDir(), "image.bin")
	capture.response = func([]byte) []byte {
		return append([]byte(`{"ok":true,"data":"`), 0xDC)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "core", []string{
			"--method", "GET",
			"--path", "/api/camera_proxy/camera.test",
			"--out-binary", outputPath,
		})
	})
	if exitCode != 1 || !strings.Contains(output, "not valid UTF-8") {
		t.Fatalf("exit/output = %d, %q", exitCode, output)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("invalid response must not create binary output: %v", err)
	}
}

func TestRelayBinaryOutputPreservesDecodedArbitraryBytes(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	want := []byte{0x00, 0xFF, 0xDC, 0x80, 0x7F}
	capture.response = func([]byte) []byte {
		return []byte(fmt.Sprintf(
			`{"ok":true,"data":{"status":200,"body":%q,"body_encoding":"base64","content_type":"application/octet-stream"}}`,
			base64.StdEncoding.EncodeToString(want),
		))
	}
	outputPath := filepath.Join(t.TempDir(), "payload.bin")

	exitCode, output := captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "core", []string{
			"--method", "GET",
			"--path", "/api/camera_proxy/camera.test",
			"--out-binary", outputPath,
		})
	})
	if exitCode != 0 {
		t.Fatalf("exit/output = %d, %q", exitCode, output)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("binary bytes changed:\n got %x\nwant %x", got, want)
	}
}
