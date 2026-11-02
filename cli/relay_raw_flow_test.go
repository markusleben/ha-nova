package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// A setup, options, subentry, or repair step that sends a password, key,
// token, or PIN typed into the chat is refused before it is sent; an empty
// one, or a field that only looks similar (max_tokens), is not.
func TestRawFlowStepWithASecretIsRefused(t *testing.T) {
	for _, tc := range []struct {
		path    string
		body    any
		refused string
	}{
		{"/api/config/config_entries/flow/0123abcd", map[string]any{"host": "192.168.1.20", "password": "hunter2"}, "password"},
		{"/api/config/config_entries/options/flow/0123abcd", map[string]any{"api_key": "k-1"}, "api_key"},
		{"/api/config/config_entries/subentries/flow/0123abcd", map[string]any{"advanced": map[string]any{"access_token": "t"}}, "access_token"},
		{"/api/repairs/issues/fix/0123abcd", map[string]any{"pin": 1234}, "pin"},
		{"/api/config/config_entries/flow/0123abcd", map[string]any{"door_pin": 1234}, "door_pin"},
		{"/api/config/config_entries/flow/0123abcd", map[string]any{"password": 1234}, "password"},
		{"/api/config/config_entries/flow/0123abcd", map[string]any{"access_token": 5, "psk": 12345678}, ""},
		{"/api/config/config_entries/flow/0123abcd", map[string]any{"host": "192.168.1.20", "password": ""}, ""},
		{"/api/config/config_entries/options/flow/0123abcd", map[string]any{"max_tokens": 150, "prompt": "Be brief"}, ""},
		{"/api/config/config_entries/flow", map[string]any{"handler": "fritz"}, ""},
	} {
		body, _ := json.Marshal(map[string]any{"method": "POST", "path": tc.path, "body": tc.body})
		refusal := checkRawRelayRequest("core", body)
		if tc.refused == "" && refusal != nil || tc.refused != "" && (refusal == nil || !strings.Contains(refusal.Error(), tc.refused)) {
			t.Errorf("%s %v: refusal %v", tc.path, tc.body, refusal)
		}
	}
}

// In a plan, that refusal tells the model to cancel the flow and send the
// user to Home Assistant, never to ask them for the secret.
func TestPlanRawFlowStepWithASecretSendsTheUserToHomeAssistant(t *testing.T) {
	paths, sent := rawRelay(t, nil)
	for path, place := range map[string]string{
		"/api/config/config_entries/flow/0123abcd": "Settings > Devices & services",
		"/api/repairs/issues/fix/0123abcd":         "Settings > System > Repairs",
	} {
		exit, env := planRaw(t, paths, `[{"endpoint":"core","payload":{"method":"POST","path":"`+path+`","body":{"username":"admin","password":"hunter2"}}}]`)
		if exit != verbExitRefused || env.Error == nil || env.Error.Code != "REFUSED" || !strings.Contains(env.Error.Next, place) ||
			!strings.Contains(env.Error.Next, "core DELETE "+path) || !strings.Contains(env.Error.Next, "never ask the user for password") {
			t.Errorf("%s: exit %d %+v", path, exit, env.Error)
		}
	}
	if len(sent()) != 0 {
		t.Fatalf("planning sent %v", sent())
	}
}

// A flow whose next form asks for a secret (a password selector here, under
// a name that doesn't say so) tells the model, in the apply result, not to
// ask for it in the chat, and a step that fills that field anyway is
// refused; a form without one keeps the usual next step.
func TestPlanRawFlowFormAskingForASecretPointsToHomeAssistant(t *testing.T) {
	form := func(fields ...any) any {
		return map[string]any{"status": 200, "body": map[string]any{"type": "form", "flow_id": "f1", "handler": "fritz", "step_id": "user", "data_schema": fields}}
	}
	host := map[string]any{"name": "host", "required": true, "type": "string"}
	secret := map[string]any{"type": "expandable", "name": "advanced", "schema": []any{
		map[string]any{"name": "pass", "required": true, "selector": map[string]any{"text": map[string]any{"type": "password"}}}}}
	for _, tc := range []struct {
		fields []any
		want   string
	}{
		{[]any{host, secret}, "never ask the user for pass"},
		{[]any{host}, "Tell the user what each item's response says"},
	} {
		paths, _ := rawRelay(t, map[string]func(map[string]any) any{
			"POST /api/config/config_entries/flow": func(map[string]any) any { return form(tc.fields...) },
		})
		_, env := planRaw(t, paths, `[{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/flow","body":{"handler":"fritz"}}}]`)
		exit, env := applyRaw(t, paths, env)
		if exit != 0 || !strings.Contains(env.NextStep, tc.want) {
			t.Fatalf("exit %d next_step %q", exit, env.NextStep)
		}
		if tc.want == "Tell the user what each item's response says" {
			continue
		}
		if !strings.Contains(env.NextStep, "Settings > Devices & services") || !strings.Contains(env.NextStep, "If you started this flow, cancel it with a plan (core DELETE /api/config/config_entries/flow/f1)") ||
			!strings.Contains(env.NextStep, "leave one Home Assistant started") {
			t.Fatalf("next_step %q", env.NextStep)
		}
		step := func(body string) string {
			return `[{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/flow/f1","body":` + body + `}}]`
		}
		if exit, env := planRaw(t, paths, step(`{"host":"192.168.1.20","advanced":{"pass":"hunter2"}}`)); exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, `"pass"`) {
			t.Fatalf("a step that fills the form's password field: exit %d %+v", exit, env.Error)
		}
		if exit, env := planRaw(t, paths, step(`{"host":"192.168.1.20"}`)); exit != verbExitConfirmation {
			t.Fatalf("a step without it: exit %d %+v", exit, env.Error)
		}
	}
}

// The form's own marks decide what is kept for its flow: a field marked
// secret or a password box, whatever its name, or a text field whose name
// says secret; a number, toggle, or choice is never one by its name.
func TestPlanRawFlowFormKeepsOnlyItsSecretFields(t *testing.T) {
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{
		"POST /api/config/config_entries/flow": func(map[string]any) any {
			return map[string]any{"status": 200, "body": map[string]any{"type": "form", "flow_id": "f1", "handler": "acme", "step_id": "user", "data_schema": []any{
				map[string]any{"name": "x", "type": "integer", "secret": true},
				map[string]any{"name": "token_lifetime", "type": "integer"},
				map[string]any{"name": "use_token", "type": "boolean"},
				map[string]any{"name": "pin_mode", "selector": map[string]any{"select": map[string]any{"options": []any{"auto", "off"}}}},
				map[string]any{"name": "api_secret", "type": "string"},
			}}}
		},
	})
	_, env := planRaw(t, paths, `[{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/flow","body":{"handler":"acme"}}}]`)
	if exit, env := applyRaw(t, paths, env); exit != 0 || !strings.Contains(env.NextStep, "never ask the user for x") {
		t.Fatalf("apply: exit %d next_step %q", exit, env.NextStep)
	}
	step := func(body string) string {
		return `[{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/flow/f1","body":` + body + `}}]`
	}
	if exit, env := planRaw(t, paths, step(`{"x":1234}`)); exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, `"x"`) {
		t.Fatalf("the field the form marked secret: exit %d %+v", exit, env.Error)
	}
	if exit, env := planRaw(t, paths, step(`{"token_lifetime":3600,"use_token":true,"pin_mode":"auto"}`)); exit != verbExitConfirmation {
		t.Fatalf("a number, a toggle, and a choice: exit %d %+v", exit, env.Error)
	}
}

// Once a flow's form asked for a secret, its hand-off stays in the result
// of an apply a later step stopped.
func TestPlanRawFlowSecretHandOffSurvivesAFailedStep(t *testing.T) {
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{
		"POST /api/config/config_entries/flow": func(req map[string]any) any {
			if body, _ := req["body"].(map[string]any); body["handler"] == "hue" {
				return map[string]any{"status": 400, "body": map[string]any{"message": "Invalid handler specified"}}
			}
			return map[string]any{"status": 200, "body": map[string]any{"type": "form", "flow_id": "f1", "handler": "fritz", "step_id": "user",
				"data_schema": []any{map[string]any{"name": "password", "type": "string"}}}}
		},
	})
	_, env := planRaw(t, paths, `[{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/flow","body":{"handler":"fritz"}}},
		{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/flow","body":{"handler":"hue"}}}]`)
	exit, env := applyRaw(t, paths, env)
	if exit == 0 || env.Error == nil || !strings.Contains(env.Error.Next, "never ask the user for password") || !strings.Contains(env.Error.Next, "Tell the user which steps went through") {
		t.Fatalf("exit %d %+v", exit, env.Error)
	}
}

// The forms HA NOVA keeps: a file holding null holds none; one it can't
// read leaves the name check and says so; an options flow can leave the
// secret out.
func TestPlanRawFlowFormsFileAndOptionsHandOff(t *testing.T) {
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{
		"POST /api/config/config_entries/options/flow": func(map[string]any) any {
			return map[string]any{"status": 200, "body": map[string]any{"type": "form", "flow_id": "o1", "handler": "e1", "step_id": "init",
				"data_schema": []any{map[string]any{"name": "pass", "selector": map[string]any{"text": map[string]any{"type": "password"}}}}}}
		},
	})
	step := func(body string) string {
		return `[{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/options/flow/o1","body":` + body + `}}]`
	}
	write := func(content string) {
		file := flowFormFile(paths, "o1", time.Now())
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("null")
	if exit, env := planRaw(t, paths, step(`{"pass":"x"}`)); exit != verbExitConfirmation || len(env.Notices) != 0 {
		t.Fatalf("null file: exit %d %+v %v", exit, env.Error, env.Notices)
	}
	_, env := planRaw(t, paths, `[{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/options/flow","body":{"handler":"e1"}}}]`)
	exit, env := applyRaw(t, paths, env)
	if exit != 0 || !strings.Contains(env.NextStep, "Leave pass out of the step, or tell them to finish this in Home Assistant under Settings > Devices & services") {
		t.Fatalf("options form: exit %d next_step %q %+v", exit, env.NextStep, env.Error)
	}
	write("{")
	exit, env = planRaw(t, paths, step(`{"password":"hunter2"}`))
	if exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, `"password"`) {
		t.Fatalf("the name check still holds: exit %d %+v", exit, env.Error)
	}
	exit, env = planRaw(t, paths, step(`{"mode":"auto"}`))
	if exit != verbExitConfirmation || !slices.ContainsFunc(env.Notices, func(n verbNotice) bool { return n.Kind == "flow_forms" && strings.Contains(n.Text, "field names only") }) {
		t.Fatalf("an unreadable file: exit %d %+v %v", exit, env.Error, env.Notices)
	}
}

// A recorded secret is matched in its own section: a root field of the same
// name stays writable. Each flow keeps its own hand-off and files, and a
// file written 30 days ago goes, never a newer one of the same flow. The
// cache folder's name is taken literally.
func TestPlanRawFlowFormsPerFlow(t *testing.T) {
	form := func(id string, fields ...any) any {
		return map[string]any{"status": 200, "body": map[string]any{"type": "form", "flow_id": id, "handler": "acme", "step_id": "init", "data_schema": fields}}
	}
	password := map[string]any{"name": "value", "selector": map[string]any{"text": map[string]any{"type": "password"}}}
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{
		"POST /api/repairs/issues/fix": func(map[string]any) any { return form("r1", password) },
		"POST /api/config/config_entries/options/flow": func(map[string]any) any {
			return form("o1", map[string]any{"name": "value", "type": "string"}, map[string]any{"type": "expandable", "name": "advanced", "schema": []any{password}})
		},
	})
	paths.CacheDir = filepath.Join(paths.CacheDir, "ha[prod]*")
	for id, days := range map[string]int{"stale": 31, "open": 29, "o1": 31} {
		file := flowFormFile(paths, id, time.Now().Add(-time.Duration(days)*24*time.Hour))
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(`[["x"]]`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, env := planRaw(t, paths, `[{"endpoint":"core","payload":{"method":"POST","path":"/api/repairs/issues/fix","body":{"handler":"acme","issue_id":"i1"}}},
		{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/options/flow","body":{"handler":"e1"}}}]`)
	exit, env := applyRaw(t, paths, env)
	if exit != 0 || !strings.Contains(env.NextStep, "Settings > System > Repairs") || !strings.Contains(env.NextStep, "core DELETE /api/repairs/issues/fix/r1") ||
		!strings.Contains(env.NextStep, "core DELETE /api/config/config_entries/options/flow/o1") {
		t.Fatalf("two flows: exit %d next_step %q", exit, env.NextStep)
	}
	step := func(body string) string {
		return `[{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/options/flow/o1","body":` + body + `}}]`
	}
	if exit, env := planRaw(t, paths, step(`{"value":"friendly label"}`)); exit != verbExitConfirmation {
		t.Fatalf("the root field of the same name: exit %d %+v", exit, env.Error)
	}
	if exit, env := planRaw(t, paths, step(`{"advanced":{"value":"hunter2"}}`)); exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, `"value"`) {
		t.Fatalf("the section's password: exit %d %+v", exit, env.Error)
	}
	entries, _ := os.ReadDir(filepath.Dir(flowFormFile(paths, "r1", time.Now())))
	var flows []string
	for _, entry := range entries {
		flow, _, _ := strings.Cut(entry.Name(), ".")
		flows = append(flows, flow)
	}
	if !slices.Equal(flows, []string{"o1", "open", "r1"}) {
		t.Fatalf("kept %v", flows)
	}
}
