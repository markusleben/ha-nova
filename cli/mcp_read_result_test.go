package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func states(n int) []any {
	rows := make([]any, n)
	for i := range rows {
		rows[i] = map[string]any{"entity_id": fmt.Sprintf("light.l%04d", i), "state": map[bool]string{true: "on", false: "off"}[i%2 == 0]}
	}
	return rows
}

// jq runs on the Relay's {ok, data} as relay --jq does: one output as is,
// several or none as a list.
func TestMCPReadJQ(t *testing.T) {
	paths, calls := readRelay(t, func(endpoint string, body map[string]any) any {
		if body["type"] == "get_states" {
			return states(3)
		}
		return nil
	})
	c := startMCP(t, paths)
	mcpInit(c)
	getStates := map[string]any{"type": "get_states"}
	for filter, want := range map[string]string{
		".data | length":    `3`,
		".data[].entity_id": `["light.l0000","light.l0001","light.l0002"]`,
		`[.data[] | select(.state == "on") | .entity_id]`: `["light.l0000","light.l0002"]`,
		`.data[] | select(.state == "gone")`:              `[]`,
		".ok":                                             `true`,
	} {
		env, code := readEnvelope(c.tool("ha_read", map[string]any{"endpoint": "ws", "payload": getStates, "jq": filter}))
		got, _ := json.Marshal(env["result"])
		if code != "" || string(got) != want {
			t.Errorf("jq %s: %s (%s), want %s", filter, got, code, want)
		}
	}
	// The same filter through `ha-nova relay ws --jq` prints the same values.
	exit, out := captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "ws", []string{"--data", `{"type":"get_states"}`, "--jq", ".data[].entity_id"})
	})
	if exit != 0 || strings.Join(strings.Fields(out), ",") != `"light.l0000","light.l0001","light.l0002"` {
		t.Fatalf("relay --jq: %d %q", exit, out)
	}
	before := len(calls())
	for filter, code := range map[string]string{"[": "INVALID_ARGUMENTS", " ": "INVALID_ARGUMENTS", ".data.entity_id": "JQ_FAILED"} {
		if _, got := readEnvelope(c.tool("ha_read", map[string]any{"endpoint": "ws", "payload": getStates, "jq": filter})); got != code {
			t.Errorf("jq %q: code %q, want %q", filter, got, code)
		}
	}
	if sent := len(calls()) - before; sent != 1 { // only the filter that compiles reads
		t.Fatalf("%d reads for bad filters, want 1", sent)
	}
	env, _ := readEnvelope(c.tool("ha_read", map[string]any{"endpoint": "core", "path": "/api/states", "jq": ".data.status"}))
	if env["result"] != 200.0 {
		t.Fatalf("core jq: %v", env)
	}
}

// Over 64 KiB (after jq) nothing comes back but the step to filter.
func TestMCPReadSizeCap(t *testing.T) {
	paths, _ := readRelay(t, func(endpoint string, body map[string]any) any {
		if body["type"] == "get_states" {
			return states(2000) // about 90 KiB
		}
		return nil
	})
	c := startMCP(t, paths)
	mcpInit(c)
	getStates := map[string]any{"type": "get_states"}
	for _, filter := range []string{"", ".data", ".data[]"} {
		args := map[string]any{"endpoint": "ws", "payload": getStates}
		if filter != "" {
			args["jq"] = filter
		}
		res := c.tool("ha_read", args)
		env, code := readEnvelope(res)
		errInfo, _ := env["error"].(map[string]any)
		next, _ := errInfo["next"].(string)
		text := res["content"].([]any)[0].(map[string]any)["text"].(string)
		if code != "RESULT_TOO_LARGE" || env["result"] != nil || !strings.Contains(fmt.Sprint(errInfo["message"]), "over ha_read's 64 KiB") ||
			!strings.Contains(next, "Filter with jq") || strings.Contains(text, "light.l1999") {
			t.Errorf("jq %q: %v", filter, env)
		}
	}
	if env, code := readEnvelope(c.tool("ha_read", map[string]any{"endpoint": "ws", "payload": getStates, "jq": `[.data[] | select(.state == "on")] | length`})); code != "" || env["result"] != 1000.0 {
		t.Fatalf("filtered: %v", env)
	}
}

// A camera frame comes back as image content, never as text.
func TestMCPReadCameraImage(t *testing.T) {
	frame := []byte("ÿØÿà a tiny jpeg")
	big := make([]byte, mcpCameraMax+1)
	paths, calls := readRelay(t, func(endpoint string, body map[string]any) any {
		switch body["path"] {
		case "/api/camera_proxy/camera.door":
			return map[string]any{"status": 200, "body": base64.StdEncoding.EncodeToString(frame), "body_encoding": "base64", "content_type": "image/jpeg; charset=binary"}
		case "/api/camera_proxy/camera.text":
			return map[string]any{"status": 200, "body": map[string]any{"message": "no image"}}
		case "/api/camera_proxy/camera.big":
			return map[string]any{"status": 200, "body": base64.StdEncoding.EncodeToString(big), "body_encoding": "base64", "content_type": "image/png"}
		}
		return map[string]any{"status": 404, "body": map[string]any{"message": "Entity not found."}}
	})
	c := startMCP(t, paths)
	mcpInit(c)
	res := c.tool("ha_read", map[string]any{"endpoint": "camera", "entity_id": "camera.door"})
	env, code := readEnvelope(res)
	content := res["content"].([]any)
	if code != "" || len(content) != 2 {
		t.Fatalf("camera: %v", res)
	}
	image := content[1].(map[string]any)
	text := content[0].(map[string]any)["text"].(string)
	encoded := base64.StdEncoding.EncodeToString(frame)
	result, _ := env["result"].(map[string]any)
	if image["type"] != "image" || image["mimeType"] != "image/jpeg" || image["data"] != encoded || strings.Contains(text, encoded) ||
		env["image"] != nil || result["bytes"] != float64(len(frame)) || result["mime_type"] != "image/jpeg" {
		t.Fatalf("camera: %v", res)
	}
	if last := calls()[len(calls())-1]; last.endpoint != "core" || last.body != `{"method":"GET","path":"/api/camera_proxy/camera.door"}` {
		t.Fatalf("camera request %v", last)
	}
	for id, want := range map[string]string{"camera.text": "no image", "camera.big": "over 8 MiB", "camera.gone": "HTTP 404"} {
		res := c.tool("ha_read", map[string]any{"endpoint": "camera", "entity_id": id})
		env, code := readEnvelope(res)
		if code == "" || len(res["content"].([]any)) != 1 || !strings.Contains(fmt.Sprint(env["error"]), want) {
			t.Errorf("%s: %v", id, env)
		}
	}
}

// Stored codes and live phone confirmation ids are hidden before jq sees them.
func TestMCPReadRedacts(t *testing.T) {
	paths, _ := readRelay(t, func(endpoint string, body map[string]any) any {
		if body["type"] == "config/entity_registry/get" {
			return map[string]any{"entity_id": "lock.front", "options": map[string]any{"lock": map[string]any{"default_code": "4711"}}}
		}
		return map[string]any{"status": 200, "body": map[string]any{"entity_id": "event.phone", "attributes": map[string]any{"action": "HANOVA_OK_0123456789abcdef"}}}
	})
	c := startMCP(t, paths)
	mcpInit(c)
	for _, args := range []map[string]any{
		{"endpoint": "ws", "payload": map[string]any{"type": "config/entity_registry/get", "entity_id": "lock.front"}},
		{"endpoint": "ws", "payload": map[string]any{"type": "config/entity_registry/get", "entity_id": "lock.front"}, "jq": ".data.options.lock.default_code"},
		{"endpoint": "core", "path": "/api/states/event.phone"},
		{"endpoint": "core", "path": "/api/states/event.phone", "jq": ".data.body.attributes.action"},
	} {
		res := c.tool("ha_read", args)
		out, _ := json.Marshal(res)
		if _, code := readEnvelope(res); code != "" || strings.Contains(string(out), "4711") || strings.Contains(string(out), "HANOVA_OK_") || !strings.Contains(string(out), "(hidden") {
			t.Errorf("%v: %s", args, out)
		}
	}
}

// snapshot and diff run the CLI verbs offline: same output, no Relay.
func TestMCPReadSnapshotAndDiff(t *testing.T) {
	paths, calls := readRelay(t, nil)
	seedTestSnapshot(t, paths, `{"alias":"NOVA","mode":"single"}`)
	c := startMCP(t, paths)
	mcpInit(c)
	result := func(args map[string]any) (any, string) {
		env, code := readEnvelope(c.tool("ha_read", args))
		if code != "" {
			return env["error"], code
		}
		return env["result"], code
	}
	list, code := result(map[string]any{"endpoint": "snapshot", "action": "show", "list": true})
	if rows, _ := list.([]any); code != "" || len(rows) != 1 || rows[0].(map[string]any)["target_id"] != "1" {
		t.Fatalf("show list: %v %s", list, code)
	}
	if target, code := result(map[string]any{"endpoint": "snapshot", "action": "show", "target": "1", "jq": ".expected_after.mode"}); code != "" || target != "single" {
		t.Fatalf("show target: %v %s", target, code)
	}
	if missing, code := result(map[string]any{"endpoint": "snapshot", "action": "show", "target": "nope"}); code != "READ_FAILED" || !strings.Contains(fmt.Sprint(missing), "nope") {
		t.Fatalf("show missing: %v %s", missing, code)
	}
	for mode, want := range map[string]string{"single": "match", "restart": "drift"} {
		if verdict, code := result(map[string]any{"endpoint": "snapshot", "action": "verify", "against": map[string]any{"id": "1", "alias": "NOVA", "mode": mode}}); code != "" || verdict != want {
			t.Errorf("verify %s: %v %s", mode, verdict, code)
		}
	}
	before, after := `{"alias":"Morning","mode":"single","triggers":[{"trigger":"sun","event":"sunrise"}]}`, `{"alias":"Morning lights","mode":"restart","triggers":[{"trigger":"sun","event":"sunrise"}]}`
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "b.json"), []byte(before), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "a.json"), []byte(after), 0o600)
	exit, cli := captureCommandOutput(t, func() int {
		return runDiffCommand(paths, []string{"--before", filepath.Join(dir, "b.json"), "--after", filepath.Join(dir, "a.json")})
	})
	var beforeObj, afterObj map[string]any
	_ = json.Unmarshal([]byte(before), &beforeObj)
	_ = json.Unmarshal([]byte(after), &afterObj)
	rows, code := result(map[string]any{"endpoint": "diff", "before": beforeObj, "after": afterObj})
	var got []string
	for _, row := range rows.([]any) {
		got = append(got, row.(string))
	}
	if exit != 0 || code != "" || len(got) < 2 || strings.Join(got, "\n")+"\n" != cli {
		t.Fatalf("diff: %q, cli %q", got, cli)
	}
	if bad, code := result(map[string]any{"endpoint": "diff", "before": map[string]any{"a": 1}, "after": map[string]any{"a": 1}, "jq": "length"}); code != "" || bad != 0.0 {
		t.Fatalf("no change: %v %s", bad, code)
	}
	if n := len(calls()); n != 0 {
		t.Fatalf("snapshot and diff sent %d Relay requests", n)
	}
}
