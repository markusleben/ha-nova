package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A setup, options, subentry, or repair step that sends a password, key,
// token, or PIN typed into the chat, or a URL with a password in it, is
// refused before it is sent; an empty one, or a field that only looks
// similar (max_tokens, bypass_mode), is not.
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
		{"/api/config/config_entries/flow/0123abcd", map[string]any{"stream_source": "rtsp://admin:hunter2@192.168.1.5/live"}, "stream_source"},
		{"/api/config/config_entries/options/flow/0123abcd", map[string]any{"still_image_url": "http://admin:pw@192.168.1.5/still.jpg"}, "still_image_url"},
		{"/api/config/config_entries/flow/0123abcd", map[string]any{"stream_source": "rtsp://192.168.1.5/live", "bypass_mode": "on", "otp_enabled": "yes"}, ""},
	} {
		body, _ := json.Marshal(map[string]any{"method": "POST", "path": tc.path, "body": tc.body})
		refusal := checkRawRelayRequest("core", body)
		if tc.refused == "" && refusal != nil || tc.refused != "" && (refusal == nil || !strings.Contains(refusal.Error(), tc.refused)) {
			t.Errorf("%s %v: refusal %v", tc.path, tc.body, refusal)
		}
	}
	for _, name := range []string{"pass", "pwd", "passcode", "pincode", "pairing_code", "otp", "otp_code", "mfa_code", "2fa"} {
		for _, value := range []any{"123456", 123456} {
			body, _ := json.Marshal(map[string]any{"method": "POST", "path": "/api/config/config_entries/flow/0123abcd", "body": map[string]any{name: value}})
			if refusal := checkRawRelayRequest("core", body); refusal == nil || !strings.Contains(refusal.Error(), strconv.Quote(name)) {
				t.Errorf("%s %v: refusal %v", name, value, refusal)
			}
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
		map[string]any{"name": "entry", "required": true, "selector": map[string]any{"text": map[string]any{"type": "password"}}}}}
	for _, tc := range []struct {
		fields []any
		want   string
	}{
		{[]any{host, secret}, "never ask the user for entry"},
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
		if !strings.HasPrefix(env.NextStep, "The setup isn't finished") || !strings.Contains(env.NextStep, "complete that step in Home Assistant under Settings > Devices & services") ||
			!strings.Contains(env.NextStep, "If you started this flow, cancel it with a plan (core DELETE /api/config/config_entries/flow/f1)") ||
			!strings.Contains(env.NextStep, "leave one Home Assistant started") {
			t.Fatalf("next_step %q", env.NextStep)
		}
		step := func(body string) string {
			return `[{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/flow/f1","body":` + body + `}}]`
		}
		if exit, env := planRaw(t, paths, step(`{"host":"192.168.1.20","advanced":{"entry":"hunter2"}}`)); exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, `"entry"`) {
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
// read stops the step, in a plan as on the computer, and names where the
// user completes it; an options flow can leave the secret out.
func TestPlanRawFlowFormsFileAndOptionsHandOff(t *testing.T) {
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{
		"POST /api/config/config_entries/options/flow": func(map[string]any) any {
			return map[string]any{"status": 200, "body": map[string]any{"type": "form", "flow_id": "o1", "handler": "e1", "step_id": "init",
				"data_schema": []any{map[string]any{"name": "entry", "selector": map[string]any{"text": map[string]any{"type": "password"}}}}}}
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
	if exit, env := planRaw(t, paths, step(`{"entry":"x"}`)); exit != verbExitConfirmation || len(env.Notices) != 0 {
		t.Fatalf("null file: exit %d %+v %v", exit, env.Error, env.Notices)
	}
	_, env := planRaw(t, paths, `[{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/options/flow","body":{"handler":"e1"}}}]`)
	exit, env := applyRaw(t, paths, env)
	if exit != 0 || !strings.Contains(env.NextStep, "Leave entry out of the step, or tell them to complete that step in Home Assistant under Settings > Devices & services") {
		t.Fatalf("options form: exit %d next_step %q %+v", exit, env.NextStep, env.Error)
	}
	write("{")
	exit, env = planRaw(t, paths, step(`{"password":"hunter2"}`))
	if exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, `"password"`) {
		t.Fatalf("the name check still holds: exit %d %+v", exit, env.Error)
	}
	unread := "cannot read the setup forms HA NOVA keeps"
	exit, env = planRaw(t, paths, step(`{"mode":"auto"}`))
	if exit == verbExitConfirmation || exit == 0 || env.Error == nil || !strings.Contains(env.Error.Message, unread) {
		t.Fatalf("an unreadable file in a plan: exit %d %+v", exit, env.Error)
	}
	_, refusal, err := guardRawRequest(paths, verbOptions{}, "core", []byte(`{"method":"POST","path":"/api/config/config_entries/options/flow/o1","body":{"mode":"auto"}}`))
	if refusal != nil || err == nil || !strings.Contains(err.Error(), unread) ||
		!strings.Contains(err.Error(), "the setup isn't finished: the user completes this step in Home Assistant under Settings > Devices & services") {
		t.Fatalf("an unreadable file on the computer: %v %v", refusal, err)
	}
}

// A flow that ends (create_entry, abort) or that a plan cancels (DELETE)
// loses its files, and a cancelled flow keeps no hand-off.
func TestPlanRawFlowFormsGoWhenTheFlowEnds(t *testing.T) {
	form := func(id string) any {
		return map[string]any{"status": 200, "body": map[string]any{"type": "form", "flow_id": id, "handler": id, "step_id": "user",
			"data_schema": []any{map[string]any{"name": "entry", "selector": map[string]any{"text": map[string]any{"type": "password"}}}}}}
	}
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{
		"POST /api/config/config_entries/flow": func(req map[string]any) any {
			body, _ := req["body"].(map[string]any)
			return form(fmt.Sprint(body["handler"]))
		},
		"POST /api/config/config_entries/flow/c1": func(map[string]any) any {
			return map[string]any{"status": 200, "body": map[string]any{"type": "create_entry", "flow_id": "c1", "handler": "c1"}}
		},
		"POST /api/config/config_entries/flow/a1": func(map[string]any) any {
			return map[string]any{"status": 200, "body": map[string]any{"type": "abort", "flow_id": "a1", "reason": "already_configured"}}
		},
	})
	start := func(id string) string {
		return `{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/flow","body":{"handler":"` + id + `"}}}`
	}
	files := func(id string) int { found, _ := flowFormFiles(paths, id); return len(found) }
	_, env := planRaw(t, paths, "["+start("c1")+","+start("a1")+"]")
	if exit, env := applyRaw(t, paths, env); exit != 0 || files("c1") != 1 || files("a1") != 1 {
		t.Fatalf("two forms: exit %d %+v", exit, env.Error)
	}
	_, env = planRaw(t, paths, `[{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/flow/c1","body":{"host":"x"}}},
		{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/flow/a1","body":{"host":"y"}}},`+start("d1")+`,
		{"endpoint":"core","payload":{"method":"DELETE","path":"/api/config/config_entries/flow/d1"}}]`)
	exit, env := applyRaw(t, paths, env)
	if exit != 0 || files("c1")+files("a1")+files("d1") != 0 || strings.Contains(env.NextStep, "isn't finished") {
		t.Fatalf("ended and cancelled: exit %d next_step %q files %d %d %d", exit, env.NextStep, files("c1"), files("a1"), files("d1"))
	}
}

// ha-nova relay core keeps a flow's forms as a raw plan does: a step that
// fills the password its form asked for is refused, and a cancel removes the
// flow's files.
func TestRelayFlowFormsAreKeptOnTheComputer(t *testing.T) {
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"POST /api/config/config_entries/flow": func(map[string]any) any {
			return map[string]any{"status": 200, "body": map[string]any{"type": "form", "flow_id": "r1", "handler": "demo", "step_id": "user",
				"data_schema": []any{map[string]any{"name": "entry", "selector": map[string]any{"text": map[string]any{"type": "password"}}}}}}
		},
		"DELETE /api/config/config_entries/flow/r1": func(map[string]any) any { return map[string]any{"status": 200, "body": map[string]any{}} },
	})
	if exit, stderr := relayCore(t, paths, "POST", "/api/config/config_entries/flow", `{"handler":"demo"}`); exit != 0 {
		t.Fatalf("start: exit %d %s", exit, stderr)
	}
	if exit, stderr := relayCore(t, paths, "POST", "/api/config/config_entries/flow/r1", `{"entry":"hunter2"}`); exit != rawRefusedExit || !strings.Contains(stderr, `"entry"`) {
		t.Fatalf("the password its form asked for: exit %d %s", exit, stderr)
	}
	if exit, stderr := relayCore(t, paths, "DELETE", "/api/config/config_entries/flow/r1", ""); exit != 0 {
		t.Fatalf("cancel: exit %d %s", exit, stderr)
	}
	if found, err := flowFormFiles(paths, "r1"); len(found) != 0 || err != nil {
		t.Fatalf("after the cancel: files %v %v", found, err)
	}
	if got := strings.Join(sent(), " | "); got != "POST /api/config/config_entries/flow | DELETE /api/config/config_entries/flow/r1" {
		t.Fatalf("sent %s", got)
	}
}

// relayCore runs ha-nova relay core and returns its exit and stderr.
func relayCore(t *testing.T, paths runtimePaths, method, target, body string) (exit int, stderr string) {
	t.Helper()
	args := []string{"core", "--method", method, "--path", target}
	if body != "" {
		args = append(args, "--body", body)
	}
	stderr = captureStderr(t, func() { exit = runRelayCommand(paths, args) })
	return exit, stderr
}

// The Relay forwards a core path as a URL path (core-proxy.ts): without its
// fragment or query, %-escapes decoded (and a ? or # they decode to cuts the
// path again when fetch parses it), backslashes as slashes, rooted. The flow
// guard reads the same path (relayCorePath), so a form that came back is
// kept however the path that reached it was spelled, and a cancel with a
// fragment removes the flow's files. Doubled and trailing slashes, which the
// Relay keeps, are folded too (broader, never narrower). A dot segment, which
// the Relay resolves before it decodes, is refused: nothing is sent.
func TestRelayFlowFormsKeptWhateverThePathSpelling(t *testing.T) {
	form := func(map[string]any) any {
		return map[string]any{"status": 200, "body": map[string]any{"type": "form", "flow_id": "r1", "handler": "demo", "step_id": "user",
			"data_schema": []any{map[string]any{"name": "entry", "selector": map[string]any{"text": map[string]any{"type": "password"}}}}}}
	}
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/api/config/config_entries/flow#x", `{"handler":"demo"}`},
		{"POST", "/api/config/config_entries/flow/r1#x", `{"host":"x"}`},
		{"GET", "/api/config/config_entries/fl%6fw/r1", ""},
		{"GET", "/api/config/config_entries/flow/%2572%2531", ""},
		{"GET", "/api/config/config_entries/flow/r1%3f", ""},
		{"GET", "/api/config/config_entries/flow/r1%23x", ""},
		{"GET", "/api/config/config_entries/flow/r1%253f", ""},
		{"GET", `/api/config/config_entries/flow\r1`, ""},
		{"GET", "api/config/config_entries/flow/r1", ""},
		{"GET", "/api/config/config_entries//flow/r1", ""},
		{"GET", "/api/config/config_entries/flow/r1/", ""},
	} {
		paths, _ := rawRelay(t, map[string]func(map[string]any) any{c.method + " " + c.path: form})
		if exit, stderr := relayCore(t, paths, c.method, c.path, c.body); exit != 0 {
			t.Errorf("%s %s: exit %d %s", c.method, c.path, exit, stderr)
			continue
		}
		if exit, stderr := relayCore(t, paths, "POST", "/api/config/config_entries/flow/r1", `{"entry":"hunter2"}`); exit != rawRefusedExit {
			t.Errorf("after %s %s, the password its form asked for: exit %d %s", c.method, c.path, exit, stderr)
		}
	}
	counted, hits := rawRelayCounter(t)
	if exit, stderr := relayCore(t, counted, "GET", "/api/config/config_entries/flow/x/../r1", ""); exit != rawRefusedExit || hits.Load() != 0 {
		t.Errorf("a dot segment: exit %d, %d sent %s", exit, hits.Load(), stderr)
	}
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{
		"POST /api/config/config_entries/flow":        form,
		"DELETE /api/config/config_entries/flow/r1#x": func(map[string]any) any { return map[string]any{"status": 200, "body": map[string]any{}} },
	})
	relayCore(t, paths, "POST", "/api/config/config_entries/flow", `{"handler":"demo"}`)
	if exit, stderr := relayCore(t, paths, "DELETE", "/api/config/config_entries/flow/r1#x", ""); exit != 0 {
		t.Fatalf("cancel: exit %d %s", exit, stderr)
	}
	if found, err := flowFormFiles(paths, "r1"); len(found) != 0 || err != nil {
		t.Fatalf("after a cancel with a fragment: files %v %v", found, err)
	}
}

// A form a plan couldn't keep stops that flow for the rest of the plan:
// its next step can't be checked against the form, so nothing more is sent.
func TestPlanRawFlowFormNotKeptStopsTheFlow(t *testing.T) {
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"POST /api/config/config_entries/flow": func(map[string]any) any {
			return map[string]any{"status": 200, "body": map[string]any{"type": "form", "flow_id": "k1", "handler": "demo", "step_id": "user",
				"data_schema": []any{map[string]any{"name": "entry", "selector": map[string]any{"text": map[string]any{"type": "password"}}}}}}
		},
	})
	dir := flowFormDir(paths)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	exit, env := planRaw(t, paths, `[{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/flow","body":{"handler":"demo"}}},
		{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/flow/k1","body":{"entry":"hunter2"}}}]`)
	if exit != verbExitConfirmation {
		t.Fatalf("plan: exit %d %+v", exit, env.Error)
	}
	exit, env = applyRaw(t, paths, env)
	if got := strings.Join(sent(), " | "); got != "POST /api/config/config_entries/flow" {
		t.Fatalf("apply: exit %d sent %s %+v", exit, got, env.Error)
	}
	if env.Error == nil || !strings.Contains(env.Error.Message, "couldn't keep the setup form") ||
		!slices.ContainsFunc(env.Notices, func(n verbNotice) bool {
			return n.Kind == "flow_forms" && strings.Contains(n.Text, "sent that flow nothing more")
		}) {
		t.Fatalf("apply: exit %d %+v %+v", exit, env.Error, env.Notices)
	}
}

// A flow's new file outranks its newest one even after the clock was set
// back, and a temporary file a write left behind goes after an hour.
func TestPlanRawFlowFormsOutrankAClockSetBack(t *testing.T) {
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{
		"POST /api/config/config_entries/flow": func(map[string]any) any {
			return map[string]any{"status": 200, "body": map[string]any{"type": "form", "flow_id": "s1", "handler": "acme", "step_id": "user",
				"data_schema": []any{map[string]any{"name": "entry", "selector": map[string]any{"text": map[string]any{"type": "password"}}}}}}
		},
	})
	for file, content := range map[string]string{
		flowFormFile(paths, "s1", time.Now().Add(time.Hour)):                 `[["host"]]`,
		flowFormFile(paths, "s1", time.Now().Add(-2*time.Hour)) + ".tmp-123": "",
	} {
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, env := planRaw(t, paths, `[{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/flow","body":{"handler":"acme"}}}]`)
	if exit, env := applyRaw(t, paths, env); exit != 0 {
		t.Fatalf("apply: exit %d %+v", exit, env.Error)
	}
	step := func(body string) string {
		return `[{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/flow/s1","body":` + body + `}}]`
	}
	if exit, env := planRaw(t, paths, step(`{"host":"192.168.1.20"}`)); exit != verbExitConfirmation {
		t.Fatalf("the older form still counts: exit %d %+v", exit, env.Error)
	}
	if exit, env := planRaw(t, paths, step(`{"entry":"hunter2"}`)); exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, `"entry"`) {
		t.Fatalf("the newest form: exit %d %+v", exit, env.Error)
	}
	entries, _ := os.ReadDir(flowFormDir(paths))
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("left behind: %s", entry.Name())
		}
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
