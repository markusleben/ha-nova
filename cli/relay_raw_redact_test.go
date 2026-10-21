package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const seededCode = "4711"

// lockEntry is a registry get/get_entries row as Home Assistant returns it:
// the lock's default code sits in options in plain text.
func lockEntry(code string) map[string]any {
	return map[string]any{"entity_id": "lock.front", "platform": "zwave_js", "name": nil,
		"options": map[string]any{"lock": map[string]any{"default_code": code}, "conversation": map[string]any{"should_expose": false}}}
}

func TestRedactSecretsGolden(t *testing.T) {
	// Each case's want is its input with the secrets swapped for the marker:
	// key order and every other byte stay.
	cases := []struct {
		name, in string
		secrets  []string
	}{
		{"lock and alarm codes", `{"ok":true,"data":{"lock.front":{"options":{"lock":{"default_code":"4711"}}},"alarm_control_panel.home":{"options":{"alarm_control_panel":{"default_code":"9999"}}}}}`, []string{`"4711"`, `"9999"`}},
		{"an empty code stays empty: not set", `{"ok":true,"data":{"options":{"lock":{"default_code":""}}}}`, nil},
		{"old values in a registry event", `{"event_type":"entity_registry_updated","data":{"changes":{"options":{"lock":{"default_code":"4711"}}}}}`, []string{`"4711"`}},
		{"an escaped key", `{"options":{"lock":{"default_\u0063ode":"4711"}}}`, nil}, // checked below: re-encoded
		{"backup encryption password", `{"config":{"create_backup":{"agent_ids":["backup.local"],"password":"s3cret"}}}`, []string{`"s3cret"`}},
		{"no backup password set", `{"config":{"create_backup":{"password":null}}}`, nil},
		{"thread dataset", `{"otbr":{"active_dataset_tlvs":"0e080000","channel":15}}`, []string{`"0e080000"`}},
		{"secret flow fields only", `{"type":"form","data_schema":[{"name":"host","type":"string","description":{"suggested_value":"10.0.0.2"}},{"name":"pw","selector":{"text":{"type":"password"}},"description":{"suggested_value":"s3cret"}},{"name":"api_key","type":"string","default":"k-1"},{"name":"country_code","type":"string","default":"DE"}]}`, []string{`"s3cret"`, `"k-1"`}},
		{"a blueprint input named password is config, not a flow form", `{"input":{"pw":{"name":"password","default":"x"}}}`, nil},
		{"flow fields in a section, a Secret() flag, key names; numbers and ids stay",
			`{"data_schema":[{"type":"expandable","name":"advanced","schema":[{"name":"client_key_password","description":{"suggested_value":"p1"}}]},{"name":"user","secret":true,"default":"p2"},{"name":"s2_access_control_key","description":{"suggested_value":"p3"}},{"name":"installer_code","default":"p4"},{"name":"max_tokens","default":4096},{"name":"use_psk","default":true},{"name":"token_url","default":"https://x"},{"name":"code_format","default":"number"}]}`,
			[]string{`"p1"`, `"p2"`, `"p3"`, `"p4"`}},
		{"an MQTT alarm panel's discovery code", `{"entities":[{"discovery_data":{"payload":{"name":"Alarm","code":"1234"}}}]}`, []string{`"1234"`}},
		{"a retained MQTT discovery message", `{"topic":"homeassistant/alarm_control_panel/a/config","payload":"{\"name\":\"Alarm\",\"code\":\"1234\"}"}`, []string{`\"1234\"`}},
		{"a retained MQTT device discovery message", `{"topic":"homeassistant/device/d/config","payload":"{\"cmps\":{\"a\":{\"p\":\"alarm_control_panel\",\"code\":\"1234\"}}}"}`, []string{`\"1234\"`}},
		{"big numbers stay exact", `{"n":1e1000,"m":12345678901234567890,"default_code":"4711"}`, []string{`"4711"`}},
		{"a repeated key keeps no hidden copy", `{"default_code":"1234","default_code":null}`, nil}, // checked below
		{"trailing data is never rewritten", `{"default_code":"4711"} {}`, nil},
	}
	for _, c := range cases {
		want := c.in
		for _, secret := range c.secrets {
			marker := `"(hidden: a stored code, password, or key)"`
			if strings.HasPrefix(secret, `\"`) { // inside a JSON string payload
				marker = `\"(hidden: a stored code, password, or key)\"`
			}
			want = strings.Replace(want, secret, marker, 1)
		}
		if c.name == "a repeated key keeps no hidden copy" {
			want = `{"default_code":null}` // the last value, as Home Assistant and the Relay read it
		}
		if c.name == "an escaped key" {
			want = `{"options":{"lock":{"default_code":"(hidden: a stored code, password, or key)"}}}`
		}
		if got := string(redactSecrets([]byte(c.in), false)); got != want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, want)
		}
	}
}

// A lock's default code, seeded in Home Assistant, never reaches any CLI
// output or snapshot file: raw reads (with --jq and --out), the MCP read,
// an old Relay snapshot that still holds it, and both snapshot stores.
func TestSeededDefaultCodeNeverLeaves(t *testing.T) {
	f := newFakeHA()
	f.extra = func(endpoint string, p map[string]any) (any, bool) {
		switch p["type"] {
		case "config/entity_registry/get":
			return lockEntry(seededCode), true
		case "config/entity_registry/get_entries":
			return map[string]any{"lock.front": lockEntry(seededCode)}, true
		}
		return nil, false
	}
	h := newWriteHarness(t, f)
	f.snapshotFiles = map[string]json.RawMessage{"automations/auto-old-20261016T120000001Z.json.gz": json.RawMessage(`{"entity_id":"lock.front","options":{"lock":{"default_code":"4711"}}}`)}
	outFile := filepath.Join(t.TempDir(), "out.json")
	leaks := func(where, out string) {
		t.Helper()
		if strings.Contains(out, seededCode) {
			t.Fatalf("%s shows the code: %s", where, out)
		}
	}
	for _, args := range [][]string{
		{"--data", `{"type":"config/entity_registry/get","entity_id":"lock.front"}`},
		{"--data", `{"type":"config/entity_registry/get_entries","entity_ids":["lock.front"]}`},
		{"--data", `{"type":"config/entity_registry/get","entity_id":"lock.front"}`, "--jq", `.data.options.lock.default_code`},
		{"--data", `{"type":"config/entity_registry/get","entity_id":"lock.front"}`, "--out", outFile},
	} {
		exit, out := captureCommandOutput(t, func() int { return runRelayProxy(h.paths, "ws", args) })
		if exit != 0 {
			t.Fatalf("args %v: exit %d out %q", args, exit, out)
		}
		leaks(strings.Join(args, " "), out)
	}
	written, err := os.ReadFile(outFile)
	if err != nil || !strings.Contains(string(written), secretHidden) {
		t.Fatalf("--out file: %v %s", err, written)
	}
	leaks("--out file", string(written))

	exit, out := captureCommandOutput(t, func() int {
		return runMCPRead(h.paths, []string{"ws", `{"type":"config/entity_registry/get","entity_id":"lock.front"}`})
	})
	if exit != 0 || !strings.Contains(out, secretHidden) {
		t.Fatalf("ha_read: exit %d %s", exit, out)
	}
	leaks("ha_read", out)

	exit, out = captureCommandOutput(t, func() int {
		return runRelayProxy(h.paths, "backups", []string{"--data", `{"action":"load","file":"automations/auto-old-20261016T120000001Z.json.gz"}`})
	})
	if exit != 0 || !strings.Contains(out, secretHidden) {
		t.Fatalf("old snapshot load: exit %d %s", exit, out)
	}
	leaks("old snapshot load", out)

	// A metadata snapshot an agent saves with the code in it (got elsewhere)
	// is stored without it.
	exit, out = captureCommandOutput(t, func() int {
		return runRelayProxy(h.paths, "backups", []string{"--data", `{"action":"save","category":"automations","name":"auto-lock-front","data":{"entity_id":"lock.front","options":{"lock":{"default_code":"4711"}}}}`})
	})
	if exit != 0 {
		t.Fatalf("snapshot save: exit %d %s", exit, out)
	}
	for _, c := range h.log.all() {
		leaks("request to "+c.endpoint, string(c.raw))
	}

	record := `{"op":"update","domain":"metadata","target_id":"lock.front","before_config":{"options":{"lock":{"default_code":"4711"}}},"expected_after":{"options":{"lock":{"default_code":"4711"}}}}`
	if _, err := saveUndoSnapshotForServer(h.paths, []byte(record), ""); err != nil {
		t.Fatal(err)
	}
	// A record saved before codes were hidden is hidden on the next save.
	stackFile := undoSnapshotStackPath(h.paths)
	stored, _ := os.ReadFile(stackFile)
	if err := os.WriteFile(stackFile, []byte(strings.ReplaceAll(string(stored), secretHidden, seededCode)), 0o600); err != nil {
		t.Fatal(err)
	}
	bom := strings.Replace(record, `"target_id":"lock.front"`, `"target_id":"lock.back"`, 1)
	if _, err := saveUndoSnapshotForServer(h.paths, []byte("\ufeff"+bom), ""); err != nil { // a BOM the strict parse strips
		t.Fatal(err)
	}
	_ = filepath.WalkDir(h.paths.ConfigDir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			data, _ := os.ReadFile(path)
			leaks("file "+path, string(data))
		}
		return nil
	})
	exit, out = captureCommandOutput(t, func() int { return runSnapshotShow(h.paths, nil) })
	if exit != 0 || !strings.Contains(out, secretHidden) {
		t.Fatalf("snapshot show: exit %d %s", exit, out)
	}
	leaks("snapshot show", out)
}

// A restore or edit built from a hidden read would overwrite the real code
// with the marker: such writes are refused before anything is sent.
func TestWriteWithHiddenValueIsRefused(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	update := `{"type":"config/entity_registry/update","entity_id":"sensor.power","options_domain":"sensor","options":{"display_precision":"(hidden: a stored code, password, or key)"}}`
	escaped := strings.Replace(update, "(hidden", `\u0028hidden`, 1)
	for _, body := range []string{update, escaped} {
		exit, out := captureCommandOutput(t, func() int { return runRelayProxy(h.paths, "ws", []string{"--data", body}) })
		if exit != rawRefusedExit || !strings.Contains(out, "leave out options_domain and options entirely") {
			t.Fatalf("exit %d out %s", exit, out)
		}
	}
	if calls := h.log.all(); len(calls) != 0 {
		t.Fatalf("sent %d requests", len(calls))
	}
	// A plain read naming the marker is still a read.
	exit, out := captureCommandOutput(t, func() int {
		return runRelayProxy(h.paths, "ws", []string{"--data", `{"type":"config/entity_registry/get","entity_id":"(hidden: a stored code, password, or key)"}`})
	})
	if exit == rawRefusedExit {
		t.Fatalf("a read was refused: %s", out)
	}
	if err := checkWriteRequest("core", map[string]any{"method": "POST", "path": "/api/services/lock/unlock", "body": map[string]any{"code": secretHidden}}); err == nil {
		t.Fatal("a typed write with the marker was not refused")
	}
	if err := checkWriteRequest("core", map[string]any{"method": "POST", "path": "/api/services/lock/unlock", "body": map[string]any{"entity_id": "lock.front"}}); err != nil {
		t.Fatalf("a plain typed write was refused: %v", err)
	}
}
