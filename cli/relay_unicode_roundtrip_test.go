package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestRelayRejectsUTF16WithAndWithoutBOMBeforeRequest(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	dir := t.TempDir()
	jsonText := `{"type":"ping","title":"Ж"}`

	cases := map[string]struct {
		input []byte
		want  string
	}{
		"UTF-16LE BOM": {
			input: encodeUTF16(jsonText, binary.LittleEndian, true),
			want:  "detected UTF-16LE",
		},
		"UTF-16BE BOM": {
			input: encodeUTF16(jsonText, binary.BigEndian, true),
			want:  "detected UTF-16BE",
		},
		"UTF-16LE no BOM": {
			input: encodeUTF16(jsonText, binary.LittleEndian, false),
			want:  "detected BOM-less UTF-16LE",
		},
		"UTF-16BE no BOM": {
			input: encodeUTF16(jsonText, binary.BigEndian, false),
			want:  "detected BOM-less UTF-16BE",
		},
		"UTF-32LE no BOM": {
			input: encodeUTF32(jsonText, binary.LittleEndian, false),
			want:  "detected BOM-less UTF-32LE",
		},
		"UTF-32BE no BOM": {
			input: encodeUTF32(jsonText, binary.BigEndian, false),
			want:  "detected BOM-less UTF-32BE",
		},
		"UTF-32LE BOM": {
			input: encodeUTF32(jsonText, binary.LittleEndian, true),
			want:  "detected UTF-32LE",
		},
		"UTF-32BE BOM": {
			input: encodeUTF32(jsonText, binary.BigEndian, true),
			want:  "detected UTF-32BE",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".json")
			if err := os.WriteFile(path, tc.input, 0o600); err != nil {
				t.Fatal(err)
			}
			exitCode, output := captureCommandOutput(t, func() int {
				return runRelayProxy(paths, "ws", []string{"--data-file", path})
			})
			if exitCode != 1 || !strings.Contains(output, tc.want) || !strings.Contains(output, "nothing was sent") {
				t.Fatalf("exit/output = %d, %q", exitCode, output)
			}
			if got := capture.requests.Load(); got != 0 {
				t.Fatalf("request count = %d, want 0", got)
			}
		})
	}
}

func TestRelayRejectsBOMLessUTF16JQFilterBeforeRequest(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "payload.json")
	filterPath := filepath.Join(dir, "filter.jq")
	if err := os.WriteFile(payloadPath, []byte(`{"type":"ping"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filterPath, encodeASCIIUnits([]byte("."), 2, 0), 0o600); err != nil {
		t.Fatal(err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "ws", []string{"--data-file", payloadPath, "--jq-file", filterPath})
	})
	for _, want := range []string{"detected BOM-less UTF-16LE", "nothing was sent", "System.IO.File", "System.Text.UTF8Encoding"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: exit/output = %d, %q", want, exitCode, output)
		}
	}
	if exitCode != 1 {
		t.Fatalf("exit/output = %d, %q", exitCode, output)
	}
	if got := capture.requests.Load(); got != 0 {
		t.Fatalf("request count = %d, want 0", got)
	}
}

func TestRelayUnicodeRoundTripAndOutAreDeterministicUTF8(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	payload := []byte(`{"title":"Grüße – Öl & Überschuss · café · 東京 · 😀"}`)

	cases := []struct {
		name        string
		inputBOM    bool
		responseBOM bool
	}{
		{name: "plain UTF-8"},
		{name: "UTF-8 input BOM", inputBOM: true},
		{name: "UTF-8 response BOM", responseBOM: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture.response = func(requestBody []byte) []byte {
				response := append([]byte(`{"ok":true,"data":`), requestBody...)
				response = append(response, '}')
				if tc.responseBOM {
					response = append(append([]byte{}, utf8BOM...), response...)
				}
				return response
			}
			input := append([]byte{}, payload...)
			if tc.inputBOM {
				input = append(append([]byte{}, utf8BOM...), payload...)
			}
			dir := t.TempDir()
			payloadPath := filepath.Join(dir, "payload.json")
			outputPath := filepath.Join(dir, "response.json")
			if err := os.WriteFile(payloadPath, input, 0o600); err != nil {
				t.Fatal(err)
			}

			exitCode, output := captureCommandOutput(t, func() int {
				return runRelayProxy(paths, "ws", []string{"--data-file", payloadPath, "--out", outputPath})
			})
			if exitCode != 0 {
				t.Fatalf("exit/output = %d, %q", exitCode, output)
			}
			select {
			case requestBody := <-capture.bodies:
				if !bytes.Equal(requestBody, payload) {
					t.Fatalf("request bytes changed:\n got %x\nwant %x", requestBody, payload)
				}
			default:
				t.Fatal("request body was not captured")
			}

			got, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			want := append([]byte(`{"ok":true,"data":`), payload...)
			want = append(want, '}')
			if !bytes.Equal(got, want) {
				t.Fatalf("--out bytes changed:\n got %x\nwant %x", got, want)
			}
			if bytes.HasPrefix(got, utf8BOM) {
				t.Fatal("--out must be BOM-less UTF-8")
			}
		})
	}
}

func TestRelayRejectsTwoResponseBOMsAcrossTextSinks(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	payload := []byte(`{"type":"ping"}`)
	capture.response = func([]byte) []byte {
		response := append(append([]byte{}, utf8BOM...), utf8BOM...)
		return append(response, []byte(`{"ok":true,"data":{}}`)...)
	}

	for name, args := range map[string][]string{
		"stdout": {"--data", string(payload)},
		"out": {
			"--data", string(payload),
			"--out", filepath.Join(t.TempDir(), "response.json"),
		},
		"jq and out": {
			"--data", string(payload),
			"--jq", ".",
			"--out", filepath.Join(t.TempDir(), "filtered.json"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			exitCode, output := captureCommandOutput(t, func() int {
				return runRelayProxy(paths, "ws", args)
			})
			if exitCode != 1 || !strings.Contains(output, "more than one leading UTF-8 BOM") {
				t.Fatalf("exit/output = %d, %q", exitCode, output)
			}
			if strings.ContainsRune(output, '\uFEFF') {
				t.Fatalf("double BOM leaked to %s output: %q", name, output)
			}
			for index, arg := range args {
				if arg == "--out" {
					if _, err := os.Stat(args[index+1]); !os.IsNotExist(err) {
						t.Fatalf("double-BOM response must not create --out file: %v", err)
					}
				}
			}
		})
	}
}

func TestRelayOneResponseBOMWritesCleanStdout(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	response := []byte(`{"ok":true,"data":{"title":"Grüße 東京 😀"}}`)
	capture.response = func([]byte) []byte {
		return append(append([]byte{}, utf8BOM...), response...)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "ws", []string{"--data", `{"type":"ping"}`})
	})
	if exitCode != 0 || output != string(response)+"\n" {
		t.Fatalf("exit/output = %d, %q", exitCode, output)
	}
}

func TestRelayHealthResponseUsesStrictUTF8Boundary(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	valid := []byte(`{"ok":true,"data":{"status":"ok","version":"0.7.1"}}`)

	t.Run("one BOM is removed", func(t *testing.T) {
		capture.response = func([]byte) []byte {
			return append(append([]byte{}, utf8BOM...), valid...)
		}
		exitCode, output := captureCommandOutput(t, func() int {
			return runHealth(paths, nil)
		})
		if exitCode != 0 || output != string(valid)+"\n" {
			t.Fatalf("exit/output = %d, %q", exitCode, output)
		}
	})

	t.Run("invalid UTF-8 is rejected", func(t *testing.T) {
		capture.response = func([]byte) []byte {
			return append([]byte(`{"ok":true,"data":"`), 0xDC)
		}
		exitCode, output := captureCommandOutput(t, func() int {
			return runHealth(paths, nil)
		})
		if exitCode != 1 || !strings.Contains(output, "Relay health response is not valid UTF-8") || !strings.Contains(output, "already sent") {
			t.Fatalf("exit/output = %d, %q", exitCode, output)
		}
	})

	t.Run("two BOMs are rejected", func(t *testing.T) {
		capture.response = func([]byte) []byte {
			response := append(append([]byte{}, utf8BOM...), utf8BOM...)
			return append(response, valid...)
		}
		exitCode, output := captureCommandOutput(t, func() int {
			return runHealth(paths, nil)
		})
		if exitCode != 1 || !strings.Contains(output, "more than one leading UTF-8 BOM") || strings.ContainsRune(output, '\uFEFF') {
			t.Fatalf("exit/output = %d, %q", exitCode, output)
		}
	})

	t.Run("BOM only is rejected as empty", func(t *testing.T) {
		capture.response = func([]byte) []byte {
			return append([]byte{}, utf8BOM...)
		}
		exitCode, output := captureCommandOutput(t, func() int {
			return runHealth(paths, nil)
		})
		if exitCode != 1 || !strings.Contains(output, "relay health check failed") {
			t.Fatalf("exit/output = %d, %q", exitCode, output)
		}
	})
}

func TestRelayRejectsInvalidUTF8ResponseBeforeTextOutput(t *testing.T) {
	paths, capture := setupStrictInputRelay(t)
	capture.response = func([]byte) []byte {
		return append([]byte(`{"ok":true,"data":"`), 0xDC)
	}
	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "payload.json")
	outputPath := filepath.Join(dir, "response.json")
	if err := os.WriteFile(payloadPath, []byte(`{"type":"ping"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "ws", []string{"--data-file", payloadPath, "--out", outputPath})
	})
	if exitCode != 1 || !strings.Contains(output, "already sent") || !strings.Contains(output, "not valid UTF-8") {
		t.Fatalf("exit/output = %d, %q", exitCode, output)
	}
	if got := capture.requests.Load(); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("invalid UTF-8 response must not be written: %v", err)
	}
}

func encodeASCIIUnits(input []byte, width, asciiOffset int) []byte {
	output := make([]byte, len(input)*width)
	for index, value := range input {
		output[index*width+asciiOffset] = value
	}
	return output
}

func encodeUTF16(input string, byteOrder binary.ByteOrder, withBOM bool) []byte {
	units := utf16.Encode([]rune(input))
	output := make([]byte, 0, len(units)*2+2)
	if withBOM {
		if byteOrder == binary.LittleEndian {
			output = append(output, 0xFF, 0xFE)
		} else {
			output = append(output, 0xFE, 0xFF)
		}
	}
	for _, unit := range units {
		encoded := make([]byte, 2)
		byteOrder.PutUint16(encoded, unit)
		output = append(output, encoded...)
	}
	return output
}

func encodeUTF32(input string, byteOrder binary.ByteOrder, withBOM bool) []byte {
	output := make([]byte, 0, len([]rune(input))*4+4)
	if withBOM {
		if byteOrder == binary.LittleEndian {
			output = append(output, 0xFF, 0xFE, 0x00, 0x00)
		} else {
			output = append(output, 0x00, 0x00, 0xFE, 0xFF)
		}
	}
	for _, value := range []rune(input) {
		encoded := make([]byte, 4)
		byteOrder.PutUint32(encoded, uint32(value))
		output = append(output, encoded...)
	}
	return output
}
