package main

import (
	"strings"
	"testing"
)

func TestRedactStreamTokensGolden(t *testing.T) {
	const h = `"(hidden: a stored code, password, or key)"`
	cases := []struct{ name, in, want string }{
		{"get_states", `{"ok":true,"data":[{"entity_id":"camera.door","state":"idle","attributes":{"access_token":"t1","entity_picture":"/api/camera_proxy/camera.door?token=t1","friendly_name":"Door"}}]}`,
			`{"ok":true,"data":[{"entity_id":"camera.door","state":"idle","attributes":{"access_token":` + h + `,"entity_picture":"/api/camera_proxy/camera.door","friendly_name":"Door"}}]}`},
		{"an image entity", `{"entity_id":"image.doorbell","attributes":{"access_token":"t1","entity_picture":"/api/image_proxy/image.doorbell?token=t1"}}`,
			`{"entity_id":"image.doorbell","attributes":{"access_token":` + h + `,"entity_picture":"/api/image_proxy/image.doorbell"}}`},
		{"a state_changed event", `{"event_type":"state_changed","data":{"entity_id":"camera.door","old_state":{"entity_id":"camera.door","attributes":{"access_token":"t1"}},"new_state":{"entity_id":"camera.door","attributes":{"access_token":"t2"}}}}`,
			`{"event_type":"state_changed","data":{"entity_id":"camera.door","old_state":{"entity_id":"camera.door","attributes":{"access_token":` + h + `}},"new_state":{"entity_id":"camera.door","attributes":{"access_token":` + h + `}}}}`},
		{"subscribe_entities add and change", `{"a":{"camera.door":{"s":"idle","a":{"access_token":"t1","entity_picture":"/api/camera_proxy/camera.door?token=t1"}}},"c":{"camera.door":{"+":{"a":{"access_token":"t2"},"lc":1},"-":{"a":["brand"]}}}}`,
			`{"a":{"camera.door":{"s":"idle","a":{"access_token":` + h + `,"entity_picture":"/api/camera_proxy/camera.door"}}},"c":{"camera.door":{"+":{"a":{"access_token":` + h + `},"lc":1},"-":{"a":["brand"]}}}}`},
		{"history_during_period", `{"camera.door":[{"s":"idle","a":{"access_token":"t1"},"lu":1},{"s":"idle","lu":2}]}`,
			`{"camera.door":[{"s":"idle","a":{"access_token":` + h + `},"lu":1},{"s":"idle","lu":2}]}`},
		{"other parameters stay; every token goes, an encoded or odd one too", `{"entity_id":"camera.door","attributes":{"entity_picture":"/api/camera_proxy/camera.door?width=640&token=t1;TOKEN=t2&%74oken=t3&%zz=t4&h=1"}}`,
			`{"entity_id":"camera.door","attributes":{"entity_picture":"/api/camera_proxy/camera.door?width=640&h=1"}}`},
		{"odd shapes are hidden whole", `{"x":[{"entity_id":"camera.a","attributes":{"access_token":7,"entity_picture":{"url":"/p?token=t1"}}},{"entity_id":"camera.b","attributes":"token=t1"}]}`,
			`{"x":[{"entity_id":"camera.a","attributes":{"access_token":` + h + `,"entity_picture":` + h + `}},{"entity_id":"camera.b","attributes":` + h + `}]}`},
		{"an escaped entity id", `{"entity_id":"camera\u002edoor","attributes":{"access_token":"t1"}}`,
			`{"entity_id":"camera.door","attributes":{"access_token":` + h + `}}`},
		{"a camera's own picture without a query stays", `{"entity_id":"camera.door","attributes":{"entity_picture":"https://example.com/door.png","access_token":""}}`, ""},
		{"other entities keep their picture and attributes", `{"entity_id":"media_player.tv","attributes":{"access_token":"m1","entity_picture":"/api/media_player_proxy/media_player.tv?token=m1&cache=9"}}`, ""},
		{"camera words that are no camera entity", `{"entity_id":"sensor.camera.count","attributes":{"access_token":"s1"}}`, ""},
	}
	for _, c := range cases {
		want := c.want
		if want == "" {
			want = c.in
		}
		if got := string(redactSecrets([]byte(c.in), false)); got != want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, want)
		}
	}
}

// A camera's and an image entity's live token never reaches a raw read,
// ha_read, ha-nova get, or ha_get; frames still come through the Relay, and
// another entity's picture stays as Home Assistant sent it.
func TestStreamTokenNeverLeaves(t *testing.T) {
	const camToken, imgToken = "cam-live-4f2a", "img-live-9c1e"
	f := newFakeHA()
	f.ent("camera.door", "onvif", "idle", map[string]any{"access_token": camToken, "entity_picture": "/api/camera_proxy/camera.door?token=" + camToken, "friendly_name": "Door"})
	f.ent("image.doorbell", "ring", "2026-10-25T10:00:00+00:00", map[string]any{"access_token": imgToken, "entity_picture": "/api/image_proxy/image.doorbell?token=" + imgToken})
	f.ent("media_player.tv", "cast", "playing", map[string]any{"entity_picture": "/api/media_player_proxy/media_player.tv?token=art-1&cache=9"})
	check := func(where, out string, wantPictures ...string) {
		t.Helper()
		if strings.Contains(out, camToken) || strings.Contains(out, imgToken) {
			t.Fatalf("%s shows a live token: %s", where, out)
		}
		for _, want := range wantPictures {
			if !strings.Contains(out, want) {
				t.Fatalf("%s lacks %q: %s", where, want, out)
			}
		}
	}
	raw, _ := readRelay(t, func(endpoint string, body map[string]any) any {
		var all []any
		for _, st := range f.states {
			all = append(all, st)
		}
		return all
	})
	exit, out := captureCommandOutput(t, func() int { return runRelayProxy(raw, "ws", []string{"--data", `{"type":"get_states"}`}) })
	if exit != 0 {
		t.Fatalf("get_states: exit %d %s", exit, out)
	}
	check("get_states", out, `"/api/camera_proxy/camera.door"`, `"/api/image_proxy/image.doorbell"`, "media_player.tv?token=art-1&cache=9", secretHidden)
	h := newWriteHarness(t, f)
	for _, id := range []string{"camera.door", "image.doorbell"} {
		exit, out = captureCommandOutput(t, func() int {
			return runRelayProxy(h.paths, "core", []string{"--method", "GET", "--path", "/api/states/" + id})
		})
		if exit != 0 {
			t.Fatalf("/api/states/%s: exit %d %s", id, exit, out)
		}
		check("/api/states/"+id, out, secretHidden)
		exit, out = captureCommandOutput(t, func() int {
			return runRelayProxy(h.paths, "core", []string{"--method", "GET", "--path", "/api/states/" + id, "--jq", ".data.body.attributes.access_token"})
		})
		check("--jq on "+id, out, secretHidden)
	}
	exit, out = captureCommandOutput(t, func() int {
		return runMCPRead(h.paths, []string{"core", `--body={"method":"GET","path":"/api/states/camera.door"}`})
	})
	if exit != 0 {
		t.Fatalf("ha_read: exit %d %s", exit, out)
	}
	check("ha_read", out, "/api/camera_proxy/camera.door", secretHidden)
	exit, out = captureCommandOutput(t, func() int { return runGetCommand(h.paths, []string{"camera.door"}) })
	if exit != 0 {
		t.Fatalf("ha-nova get: exit %d %s", exit, out)
	}
	check("ha-nova get", out, "/api/camera_proxy/camera.door", secretHidden)

	s := newServe(t, f, `{"allowed_users":["`+userA+`"]}`)
	env := s.tool(userA, "ha_get", map[string]any{"entity_id": "image.doorbell"})
	if env["status"] != "ok" {
		t.Fatalf("ha_get: %v", env)
	}
	exit, out = captureCommandOutput(t, func() int { return runGetCommand(h.paths, []string{"media_player.tv"}) })
	check("ha-nova get media_player.tv", out, "media_player.tv?token=art-1")
	got, _ := marshalPlain(env)
	check("ha_get", string(got), "/api/image_proxy/image.doorbell", secretHidden)
}

// Only a camera or image entity id opens the parse: a picture path that says
// image. keeps the fast path (a parse would drop the repeated key).
func TestRedactStreamTokensFastPath(t *testing.T) {
	in := `{"entity_id":"light.desk","attributes":{"entity_picture":"/local/image.png?token=x"},"n":1,"n":2}`
	if got := string(redactSecrets([]byte(in), false)); got != in {
		t.Fatalf("got %s, want the original bytes", got)
	}
}

// A template's output is plain text redaction cannot read: a render naming
// the token or the picture is refused before anything is sent, on the CLI
// and in ha_read; render_template is not a raw WS type at all.
func TestTemplateCannotPrintStreamToken(t *testing.T) {
	for _, body := range []string{
		`{"method":"POST","path":"/api/template","body":{"template":"{{ state_attr('camera.door','access_token') }}"}}`,
		`{"method":"post","path":"/api/template?x=1","body":{"template":"{{ states.image.doorbell.attributes.Entity_Picture }}"}}`,
		`{"method":"POST","path":"/api/template","body":{"template":"{{ state_attr('camera.door','\u0061ccess_token') }}"}}`,
		`{"method":"POST","path":"/api/template","body":"{\"template\":\"{{ state_attr('camera.door','ACCESS_TOKEN') }}\"}"}`,
	} {
		rawRefused(t, "core", body)
	}
	for _, body := range []string{`{"type":"render_template","template":"{{ 1 }}"}`, `{"message":{"type":"render_template","template":"{{ 1 }}"},"collect_events":{"max_events":1}}`} {
		rawRefused(t, "ws", body)
	}
	rawAllowed(t, "core", `{"method":"POST","path":"/api/template","body":{"template":"{{ states('camera.door') }}"}}`)
	rawAllowed(t, "core", `{"method":"POST","path":"/api/config/config_entries/flow/0123abcd","body":{"access_token":"x"}}`)

	paths, hits := rawRelayCounter(t)
	exit, out := captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "core", []string{"--method", "POST", "--path", "/api/template", "--body", `{"template":"{{ state_attr('camera.door','access_token') }}"}`})
	})
	if exit != rawRefusedExit || hits.Load() != 0 || !strings.Contains(out, "read the entity instead") {
		t.Fatalf("relay core: exit %d hits %d out %s", exit, hits.Load(), out)
	}

	read, calls := readRelay(t, nil)
	c := startMCP(t, read)
	mcpInit(c)
	env, code := readEnvelope(c.tool("ha_read", map[string]any{"endpoint": "core", "method": "POST", "path": "/api/template", "payload": map[string]any{"template": "{{ state_attr('image.doorbell','entity_picture') }}"}}))
	if message, _ := env["error"].(map[string]any)["message"].(string); code != "REFUSED" || !strings.Contains(message, "read the entity instead") || len(calls()) != 0 {
		t.Fatalf("ha_read: %s %v calls %d", code, env["error"], len(calls()))
	}
}
