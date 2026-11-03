package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// A URL a raw WS write stores (Home Assistant's own URLs, a blueprint's
// source, also in its YAML, a person's or area's picture) with a password
// in it is refused before anything is sent, also one Go can't parse, in
// ha-nova relay ws, a raw plan, and a chat app's plan alike; the same URLs
// without a password go through.
func TestRawURLSettingsWithAPasswordAreRefused(t *testing.T) {
	paths, sent := rawRelay(t, nil)
	c := startMCP(t, paths)
	mcpInit(c)
	blueprint := func(yaml string) string {
		raw, _ := json.Marshal(map[string]any{"type": "blueprint/save", "domain": "automation", "path": "a.yaml", "yaml": yaml})
		return string(raw)
	}
	for _, tc := range []struct{ body, field string }{
		{`{"type":"config/core/update","external_url":"https://me:hunter2@ha.example.com"}`, "external_url"},
		{`{"type":"config/core/update","location_name":"Home","internal_url":"http://me:pa ss@homeassistant.local:8123"}`, "internal_url"},
		{`{"type":"config/core/update","external_url":"https://me:p\tw@ha.example.com"}`, "external_url"},
		{`{"message":{"type":"config/core/update","external_url":"https://me:p#ss@ha.example.com"},"collect_events":{"max_events":1}}`, "external_url"},
		{`{"type":"config/core/update","external_url":"https://admin:2024#Secret@ha.example.com"}`, "external_url"},
		{`{"type":"config/core/update","internal_url":"http://admin:8123?pw@homeassistant.local"}`, "internal_url"},
		{`{"type":"config/core/update","external_url":"\u0001https://me:pw@ha.example.com"}`, "external_url"},
		{`{"type":"config/core/update","external_url":"ht\ttps://me:pw@ha.example.com"}`, "external_url"},
		{`{"type":"blueprint/save","domain":"automation","path":"a.yaml","yaml":"x","source_url":"https://me:ghp_x@github.com/me/bp/a.yaml"}`, "source_url"},
		{blueprint("blueprint:\n  name: A\n  source_url: https://me:p#ss@example.com/a.yaml # mine\n  domain: automation\n"), "source_url"},
		{blueprint("blueprint: {name: A, source_url: 'https://me:pa ss@example.com/a.yaml', domain: automation}"), "source_url"},
		{blueprint(`{"blueprint": {"name": "A", "source_url": "https://me:x@example.com/a.yaml"}}`), "source_url"},
		{blueprint("blueprint:\n  source_url:\n    https://me:pw@example.com/a.yaml\n"), "source_url"},
		{blueprint("blueprint:\n  source_url: !!str https://me:pw@example.com/a.yaml\n"), "source_url"},
		{blueprint("blueprint:\n  source_url: >-\n    https://me:pw@example.com/a.yaml\n"), "source_url"},
		{blueprint("blueprint:\n  source_url: >- # upstream location\n    https://me:pw@example.com/a.yaml\n"), "source_url"},
		{blueprint("blueprint:\n  source_url: # note\n    https://me:pw@example.com/a.yaml\n"), "source_url"},
		{blueprint("blueprint:\n  source_url:\n    # mirror\n    https://me:pw@example.com/a.yaml\n"), "source_url"},
		{blueprint("blueprint:\n  source_url: !!str # pinned\n    https://me:pw@example.com/a.yaml\n"), "source_url"},
		{blueprint("blueprint:\n  source_url: |-\n    https://me:pa\n    ss@example.com/a.yaml\n  domain: automation\n"), "source_url"},
		{blueprint("blueprint:\n  source_url: |2-\n      https://me:pa\n    ss@example.com/a.yaml\n"), "source_url"},
		{blueprint("blueprint:\n  source_url: >-\n    https://me:pa\n    \tss@example.com/a.yaml\n"), "source_url"},
		{blueprint("blueprint:\n  source_url: \"https://me:pa\n    ss@example.com/a.yaml\"\n"), "source_url"},
		{blueprint("blueprint:\n  source_url: https://me:pa\n    ss@example.com/a.yaml\n  domain: automation\n"), "source_url"},
		{blueprint("blueprint:\n  source_url: 'https://me:p''w@example.com/a.yaml'\n"), "source_url"},
		{blueprint("blueprint:\n  source_url: \"https://me:p\\\"w@example.com/a.yaml\"\n"), "source_url"},
		{blueprint("blueprint:\n  source_url: \"https:/\t/me:pw@example.com/a.yaml\"\n"), "source_url"},
		{blueprint("blueprint:\n  source_url: ht\ttps://me:pw@example.com/a.yaml\n"), "source_url"},
		{blueprint("blueprint:\n  # source_url: # optional\n  source_url: https://me:pw@example.com/a.yaml\n"), "source_url"},
		{blueprint("blueprint:\n  description: >\n    Keep a copy at the source_url:\n  source_url: https://me:pw@example.com/a.yaml\n"), "source_url"},
		{blueprint("blueprint: {source_url: https://github.com/a.yaml, source_url: 'https://me:pw@example.com/a.yaml'}"), "source_url"},
		{blueprint("blueprint:\n  source_url: \"https://me\\x3apw@example.com/a.yaml\"\n"), "source_url"},
		{blueprint("blueprint:\n  source_url: \"https://me:pw\\u0040example.com/a.yaml\"\n"), "source_url"},
		{blueprint("blueprint:\n  source_url: \"https://me:pw\\U00110000@example.com/a.yaml\"\n"), "source_url"},
		{blueprint("blueprint:\n  source_url: \"https:/\\\n    /me:pw@example.com/a.yaml\"\n"), "source_url"},
		{blueprint("blueprint:\n  source_url: |-\n    https:/\n    /me:pw@example.com/a.yaml\n"), "source_url"},
		{blueprint("blueprint:\n  source_url: https:/\n\n    /me:pw@example.com/a.yaml\n"), "source_url"},
		{blueprint("blueprint:\n  source_url: https://me:pa, ss@example.com/a.yaml\n"), "source_url"},
		{blueprint("blueprint:\r  source_url: # c\r    https://me:pw@example.com/a.yaml\r  domain: automation\r"), "source_url"},
		{blueprint("blueprint:\n  source_url: |-\u2028    https://me:pw@example.com/a.yaml\n"), "source_url"},
		{blueprint("blueprint:\n  !!str source_url: https://me:pa\n    ss@example.com/a.yaml\n"), "source_url"},
		{`{"type":"person/update","person_id":"p1","picture":"https://me:hunter2@cdn.example.com/me.png"}`, "picture"},
		{`{"type":"config/area_registry/create","name":"Garden","picture":"https://me:hunter2@cdn.example.com/g.png"}`, "picture"},
		{`{"type":"person/create","name":"Me","picture":"http://admin:#Cam2024@192.168.1.20/snap.jpg"}`, "picture"},
	} {
		want := `that sets "` + tc.field + `" (a password or other secret)`
		if err := checkRawRelayRequest("ws", []byte(tc.body)); err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "secrets don't belong in a chat; do this in the Home Assistant UI") {
			t.Errorf("%s: guard %v", tc.body, err)
		}
		var exit int
		stderr := captureStderr(t, func() { exit = runRelayCommand(paths, []string{"ws", "--data", tc.body}) })
		if exit != rawRefusedExit || !strings.Contains(stderr, want) {
			t.Errorf("%s: relay ws exit %d %s", tc.body, exit, stderr)
		}
		if strings.Contains(tc.body, "collect_events") { // a plan sends no envelope
			continue
		}
		items := []any{map[string]any{"endpoint": "ws", "payload": json.RawMessage(tc.body)}}
		raw, _ := json.Marshal(items)
		if exit, env := planRaw(t, paths, string(raw)); exit != verbExitRefused || env.Plan != nil || env.Error == nil || !strings.Contains(env.Error.Message, want) {
			t.Errorf("%s: plan raw exit %d %+v", tc.body, exit, env.Error)
		}
		if chat, code := readEnvelope(c.tool("ha_plan", map[string]any{"kind": "raw", "requests": items})); code != "REFUSED" || chat["plan"] != nil {
			t.Errorf("%s: ha_plan %v", tc.body, chat)
		}
	}
	if got := sent(); len(got) != 0 {
		t.Errorf("refused URLs sent %v", got)
	}
	for _, body := range []string{
		`{"type":"config/core/update","external_url":"https://ha.example.com","internal_url":"http://homeassistant.local:8123"}`,
		`{"type":"config/core/update","external_url":null,"internal_url":""}`,
		`{"type":"config/core/update","external_url":"https://ha.example.com:8443","internal_url":"http://[fd00::1]:8123"}`,
		blueprint("blueprint: {name: A, source_url: https://github.com, author: me@example.com}"),
		blueprint("blueprint: {source_url: https://ha.example.com:8443,author: me@example.com}"),
		`{"type":"config/area_registry/update","area_id":"garden","picture":"https://cdn.example.com:8443/garden@2x.png"}`,
		`{"type":"blueprint/save","domain":"automation","path":"a.yaml","yaml":"x","source_url":"https://github.com/me/bp/blob/main/a.yaml"}`,
		blueprint("blueprint:\n  name: Camera\n  description: Point it at rtsp://user:pass@camera\n  source_url: https://example.com/a.yaml\n"),
		blueprint("blueprint:\n  name: A\n  source_url: >- # upstream, by me@example.com: thanks\n    https://github.com/me/bp/a.yaml\n  domain: automation\n  description: rtsp://user:pass@camera\n"),
		blueprint("blueprints:\n  - source_url: https://github.com/me/a.yaml\n    author: me@example.com\n  - source_url: 'https://github.com/me/b.yaml' # by me@example.com\n"),
		`{"type":"person/update","person_id":"p1","picture":"/api/image/serve/abc/512x512"}`,
		`{"type":"config/area_registry/update","area_id":"garden","picture":null}`,
	} {
		rawAllowed(t, "ws", body)
	}
	// The same rule reads these URLs in a setup step and an App's settings
	// too: a space or tab in the password, one Go reads as a host and port.
	for _, value := range []string{"http://me:pa ss@broker", "http://me:p\tw@broker", "mqtt://me@home:pa ss@broker", "mqtt://admin:2024#pw@broker"} {
		flow, _ := json.Marshal(map[string]any{"method": "POST", "path": "/api/config/config_entries/flow/0123abcd", "body": map[string]any{"broker_url": value}})
		app := supervisorBody("POST", "/addons/core_mosquitto/options", map[string]any{"data": map[string]any{"options": map[string]any{"server": value}}})
		_, refusal, err := guardRaw("supervisor", []byte(app), nil)
		if checkRawRelayRequest("core", flow) == nil || refusal == nil || err != nil {
			t.Errorf("%q: flow step or App option not refused (%v, %v)", value, refusal, err)
		}
	}
}
