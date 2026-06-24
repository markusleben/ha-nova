package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTraceLatestUsesRelayAndRunIDFromTraceList(t *testing.T) {
	paths, seenTypes := setupTraceRelayTest(t, func(w http.ResponseWriter, _ *http.Request, payload map[string]string) {
		switch payload["type"] {
		case "config/entity_registry/get":
			if payload["entity_id"] != "automation.nachtladung_prepare" {
				t.Fatalf("entity_id = %q", payload["entity_id"])
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"unique_id":"1782058242195"}}`))
		case "trace/list":
			if payload["item_id"] != "1782058242195" {
				t.Fatalf("trace/list item_id = %q", payload["item_id"])
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":[{"run_id":"old-run","last_step":"action/4","timestamp":{"start":"2026-06-20T21:30:00.874972+00:00","finish":"2026-06-20T21:30:00.899670+00:00"}},{"run_id":"latest-run","last_step":"action/1/choose/0/sequence/10","timestamp":{"start":"2026-06-21T21:42:08.527830+00:00","finish":"2026-06-21T21:42:13.753181+00:00"}}]}`))
		case "trace/get":
			if payload["run_id"] != "latest-run" {
				t.Fatalf("trace/get run_id = %q, want latest-run", payload["run_id"])
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"trace":{"last_step":"action/2","state":"finished"}}}`))
		default:
			t.Fatalf("unexpected ws type %q", payload["type"])
		}
	})

	exitCode, output := captureCommandOutput(t, func() int {
		return runTraceCommand(paths, []string{"latest", "automation.nachtladung_prepare", "--json"})
	})

	if exitCode != 0 {
		t.Fatalf("trace latest exit = %d, output:\n%s", exitCode, output)
	}
	for _, want := range []string{
		`"ok": true`,
		`"entity_id": "automation.nachtladung_prepare"`,
		`"unique_id": "1782058242195"`,
		`"run_id": "latest-run"`,
		`"timestamp": "2026-06-21T21:42:08.527830+00:00"`,
		`"last_step": "action/2"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	gotTypes := strings.Join(*seenTypes, ",")
	if gotTypes != "config/entity_registry/get,trace/list,trace/get" {
		t.Fatalf("ws call order = %q", gotTypes)
	}
}

func TestTraceLatestExplainsMissingTraces(t *testing.T) {
	paths, _ := setupTraceRelayTest(t, func(w http.ResponseWriter, _ *http.Request, payload map[string]string) {
		switch payload["type"] {
		case "config/entity_registry/get":
			_, _ = w.Write([]byte(`{"ok":true,"data":{"unique_id":"script_id"}}`))
		case "trace/list":
			_, _ = w.Write([]byte(`{"ok":true,"data":[]}`))
		default:
			t.Fatalf("unexpected ws type %q", payload["type"])
		}
	})

	exitCode, output := captureCommandOutput(t, func() int {
		return runTraceCommand(paths, []string{"latest", "script.test"})
	})

	if exitCode != 1 {
		t.Fatalf("trace latest exit = %d, want 1", exitCode)
	}
	for _, want := range []string{"no traces found", "keeps only recent traces", "need an id"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestTraceListNormalizesRecentRunsWithoutTraceGet(t *testing.T) {
	paths, seenTypes := setupTraceRelayTest(t, func(w http.ResponseWriter, _ *http.Request, payload map[string]string) {
		switch payload["type"] {
		case "config/entity_registry/get":
			_, _ = w.Write([]byte(`{"ok":true,"data":{"unique_id":"nachtladung_prepare"}}`))
		case "trace/list":
			_, _ = w.Write([]byte(`{"ok":true,"data":[{"run_id":"old-run","last_step":"action/4","state":"stopped","script_execution":"finished","timestamp":{"start":"2026-06-20T21:30:00.874972+00:00"},"trigger":"time"},{"run_id":"error-run","last_step":"action/1/choose/0/sequence/0","state":"stopped","script_execution":"error","error":"UndefinedError: 'breakeven_hour' is undefined","timestamp":{"start":"2026-06-21T21:40:23.736161+00:00"},"trigger":null},{"run_id":"latest-run","last_step":"action/1/choose/0/sequence/10","state":"stopped","script_execution":"finished","timestamp":{"start":"2026-06-21T21:42:08.527830+00:00"},"trigger":{"platform":"event"}}]}`))
		default:
			t.Fatalf("unexpected ws type %q", payload["type"])
		}
	})

	exitCode, output := captureCommandOutput(t, func() int {
		return runTraceCommand(paths, []string{"list", "automation.nachtladung_prepare", "--json"})
	})

	if exitCode != 0 {
		t.Fatalf("trace list exit = %d, output:\n%s", exitCode, output)
	}
	for _, want := range []string{
		`"count": 3`,
		`"unique_id": "nachtladung_prepare"`,
		`"run_id": "latest-run"`,
		`"timestamp": "2026-06-21T21:42:08.527830+00:00"`,
		`"trigger": "event"`,
		`"run_id": "error-run"`,
		`"error": "UndefinedError: 'breakeven_hour' is undefined"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	gotTypes := strings.Join(*seenTypes, ",")
	if gotTypes != "config/entity_registry/get,trace/list" {
		t.Fatalf("ws call order = %q", gotTypes)
	}
	if strings.Index(output, `"run_id": "latest-run"`) > strings.Index(output, `"run_id": "error-run"`) {
		t.Fatalf("latest run should sort before older error run:\n%s", output)
	}
}

func TestTraceGetNormalizesDetailWithoutTraceList(t *testing.T) {
	paths, seenTypes := setupTraceRelayTest(t, func(w http.ResponseWriter, _ *http.Request, payload map[string]string) {
		switch payload["type"] {
		case "config/entity_registry/get":
			_, _ = w.Write([]byte(`{"ok":true,"data":{"unique_id":"nachtladung_prepare"}}`))
		case "trace/get":
			if payload["item_id"] != "nachtladung_prepare" {
				t.Fatalf("trace/get item_id = %q", payload["item_id"])
			}
			if payload["run_id"] != "error-run" {
				t.Fatalf("trace/get run_id = %q", payload["run_id"])
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"item_id":"nachtladung_prepare","timestamp":{"start":"2026-06-21T21:40:23.736161+00:00"},"last_step":"action/1/choose/0/sequence/0","state":"stopped","script_execution":"error","error":"UndefinedError: 'breakeven_hour' is undefined","trace":null}}`))
		default:
			t.Fatalf("unexpected ws type %q", payload["type"])
		}
	})

	exitCode, output := captureCommandOutput(t, func() int {
		return runTraceCommand(paths, []string{"get", "automation.nachtladung_prepare", "error-run", "--json"})
	})

	if exitCode != 0 {
		t.Fatalf("trace get exit = %d, output:\n%s", exitCode, output)
	}
	for _, want := range []string{
		`"entity_id": "automation.nachtladung_prepare"`,
		`"unique_id": "nachtladung_prepare"`,
		`"run_id": "error-run"`,
		`"item_id": "nachtladung_prepare"`,
		`"timestamp": "2026-06-21T21:40:23.736161+00:00"`,
		`"last_step": "action/1/choose/0/sequence/0"`,
		`"script_execution": "error"`,
		`"error": "UndefinedError: 'breakeven_hour' is undefined"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	gotTypes := strings.Join(*seenTypes, ",")
	if gotTypes != "config/entity_registry/get,trace/get" {
		t.Fatalf("ws call order = %q", gotTypes)
	}
}

func TestTraceLatestRejectsUnsupportedEntityDomain(t *testing.T) {
	exitCode, output := captureCommandOutput(t, func() int {
		return runTraceCommand(runtimePaths{}, []string{"latest", "light.kitchen"})
	})

	if exitCode != 1 {
		t.Fatalf("trace latest exit = %d, want 1", exitCode)
	}
	if !strings.Contains(output, "supports only automation.<id> or script.<id>") {
		t.Fatalf("unexpected output:\n%s", output)
	}
}

func setupTraceRelayTest(t *testing.T, handler func(http.ResponseWriter, *http.Request, map[string]string)) (runtimePaths, *[]string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".test-relay-token"))

	seenTypes := []string{}
	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want Bearer test-token", got)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		seenTypes = append(seenTypes, payload["type"])
		w.Header().Set("content-type", "application/json")
		handler(w, r, payload)
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
	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := writeRelayAuthToken("test-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}
	return paths, &seenTypes
}
