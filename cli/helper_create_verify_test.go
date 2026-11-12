package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// helperHome answers like Home Assistant after a Statistics helper create:
// one built-in entry e1 with options, its sensor, and an options form.
func helperHome() map[string]any {
	return map[string]any{
		"POST /api/config/config_entries/flow/f1": map[string]any{"status": 200, "body": map[string]any{"type": "create_entry", "flow_id": "f1", "handler": "statistics", "result": map[string]any{"entry_id": "e1", "title": "Change"}}},
		"config_entries/get": []any{
			map[string]any{"entry_id": "e0", "domain": "statistics", "title": "Old", "state": "loaded", "supports_options": true},
			map[string]any{"entry_id": "e1", "domain": "statistics", "title": "Change", "state": "loaded", "supports_options": true},
		},
		"config/entity_registry/list": []any{
			map[string]any{"entity_id": "sensor.old", "config_entry_id": "e0"},
			map[string]any{"entity_id": "sensor.change", "config_entry_id": "e1"},
		},
		"manifest/list": []any{map[string]any{"domain": "statistics", "is_built_in": true}},
		"POST /api/config/config_entries/options/flow": map[string]any{"status": 200, "body": map[string]any{"type": "form", "flow_id": "o1", "handler": "e1", "step_id": "init", "data_schema": []any{
			map[string]any{"name": "state_characteristic", "description": map[string]any{"suggested_value": "change"}},
			map[string]any{"name": "max_age", "description": map[string]any{"suggested_value": map[string]any{"hours": 3}}},
			map[string]any{"name": "keep_last_sample", "description": map[string]any{"suggested_value": true}},
			map[string]any{"name": "sampling_size"},
			map[string]any{"name": "note", "description": map[string]any{"suggested_value": "{{ 1 > 0 }}"}},
		}}},
		"DELETE /api/config/config_entries/options/flow/o1": map[string]any{"status": 200, "body": map[string]any{"message": "Flow aborted"}},
	}
}

// submitHelperStep runs relay core for one flow step against home and
// returns the exit code, the combined output, the --out file's envelope
// (when extra is empty), and the keys of every request the Relay got.
func submitHelperStep(t *testing.T, home map[string]any, path string, extra ...string) (int, string, map[string]any, []string) {
	t.Helper()
	paths, calls := readRelay(t, func(endpoint string, body map[string]any) any {
		if answer, ok := home[rawKey(endpoint, body)]; ok {
			return answer
		}
		return map[string]any{"status": 200, "body": map[string]any{}}
	})
	out := filepath.Join(t.TempDir(), "step.json")
	args := append([]string{"--method", "POST", "--path", path, "--body", "{\"state_characteristic\":\"change\"}"}, extra...)
	if len(extra) == 0 {
		args = append(args, "--out", out)
	}
	exit, output := captureCommandOutput(t, func() int { return runRelayProxy(paths, "core", args) })
	var envelope map[string]any
	if data, err := os.ReadFile(out); err == nil {
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatalf("--out is not JSON: %v\n%s", err, data)
		}
	}
	var keys []string
	for _, c := range calls() {
		var body map[string]any
		_ = json.Unmarshal([]byte(c.body), &body)
		keys = append(keys, rawKey(c.endpoint, body))
	}
	return exit, output, envelope, keys
}

func verificationOf(t *testing.T, envelope map[string]any) map[string]any {
	t.Helper()
	v, ok := envelope["verification"].(map[string]any)
	if !ok {
		t.Fatalf("no verification in %v", envelope)
	}
	return v
}

// A helper create's terminal submit carries the CLI's own read-back: the
// entry by the result's id, its entities, and its options from an options
// flow that is aborted, never submitted.
func TestRelayCoreVerifiesAHelperCreate(t *testing.T) {
	exit, output, envelope, keys := submitHelperStep(t, helperHome(), "/api/config/config_entries/flow/f1")
	if exit != 0 || strings.Contains(output, "could not check") {
		t.Fatalf("exit %d\n%s", exit, output)
	}
	v := verificationOf(t, envelope)
	options, _ := v["options"].(map[string]any)
	entry, _ := v["entry"].(map[string]any)
	maxAge, _ := options["max_age"].(map[string]any)
	if v["passed"] != true || entry["entry_id"] != "e1" || entry["title"] != "Change" || entry["state"] != "loaded" ||
		!slices.Equal(anyStrings(v["linked_entities"]), []string{"sensor.change"}) ||
		options["state_characteristic"] != "change" || options["keep_last_sample"] != true || options["sampling_size"] != nil || maxAge["hours"] != float64(3) {
		t.Fatalf("verification = %v", v)
	}
	if body := envelope["data"].(map[string]any)["body"].(map[string]any); body["type"] != "create_entry" {
		t.Fatalf("the create's own answer changed: %v", body)
	}
	want := []string{"POST /api/config/config_entries/flow/f1", "config_entries/get", "config/entity_registry/list", "manifest/list",
		"POST /api/config/config_entries/options/flow", "DELETE /api/config/config_entries/options/flow/o1"}
	if !slices.Equal(keys, want) {
		t.Fatalf("requests = %v", keys)
	}
}

// --jq and stdout see the key too, and a template's > is not escaped.
func TestRelayCoreHelperVerificationReachesJQAndStdout(t *testing.T) {
	_, output, _, _ := submitHelperStep(t, helperHome(), "/api/config/config_entries/flow/f1", "--jq", ".verification.passed")
	if strings.TrimSpace(output) != "true" {
		t.Fatalf("--jq output = %q", output)
	}
	_, output, _, _ = submitHelperStep(t, helperHome(), "/api/config/config_entries/flow/f1", "--jq", ".verification.options.note")
	if !strings.Contains(output, "{{ 1 > 0 }}") {
		t.Fatalf("stdout = %s", output)
	}
}

// An entry without options support gets no options flow.
func TestRelayCoreHelperWithoutOptionsStartsNoOptionsFlow(t *testing.T) {
	home := helperHome()
	home["config_entries/get"] = []any{map[string]any{"entry_id": "e1", "domain": "statistics", "title": "Change", "state": "loaded", "supports_options": false}}
	exit, _, envelope, keys := submitHelperStep(t, home, "/api/config/config_entries/flow/f1")
	v := verificationOf(t, envelope)
	if exit != 0 || v["passed"] != true || v["options"] != nil || slices.ContainsFunc(keys, func(k string) bool { return strings.Contains(k, "options") || k == "manifest/list" }) {
		t.Fatalf("exit %d, verification %v, requests %v", exit, v, keys)
	}
}

// Every failed check says so without failing the command, which already
// created the helper: passed false, a problem that forbids running the flow
// again, the same on stderr, and an options flow it started still aborted.
func TestRelayCoreHelperVerificationFailsLoudButNotTheCreate(t *testing.T) {
	for name, tc := range map[string]struct {
		change  func(map[string]any)
		problem string
		aborted bool
	}{
		"no entry id": {func(h map[string]any) {
			h["POST /api/config/config_entries/flow/f1"] = map[string]any{"status": 200, "body": map[string]any{"type": "create_entry", "handler": "statistics", "result": map[string]any{}}}
		}, "named no entry", false},
		"entry missing": {func(h map[string]any) { h["config_entries/get"] = []any{} }, "not in the config entries", false},
		"custom integration": {func(h map[string]any) {
			h["manifest/list"] = []any{map[string]any{"domain": "statistics", "is_built_in": false}}
		}, "not Home Assistant's built-in integration", false},
		"options form unexpected": {func(h map[string]any) {
			h["POST /api/config/config_entries/options/flow"] = map[string]any{"status": 200, "body": map[string]any{"type": "menu", "flow_id": "o1", "handler": "e1", "step_id": "init"}}
		}, "\"menu\" step", true},
		"abort failed": {func(h map[string]any) {
			h["DELETE /api/config/config_entries/options/flow/o1"] = map[string]any{"status": 404, "body": map[string]any{}}
		}, "was not aborted", true},
		"registry read failed": {func(h map[string]any) {
			h["config/entity_registry/list"] = fakeRelayFailure{code: "UPSTREAM_WS_ERROR", message: "boom"}
		}, "boom", false},
	} {
		t.Run(name, func(t *testing.T) {
			home := helperHome()
			tc.change(home)
			exit, output, envelope, keys := submitHelperStep(t, home, "/api/config/config_entries/flow/f1")
			v := verificationOf(t, envelope)
			problem, _ := v["problem"].(string)
			if exit != 0 || v["passed"] != false || !strings.Contains(problem, tc.problem) || !strings.Contains(problem, "Don't run the flow again") ||
				!strings.Contains(output, "Don't run the flow again") || strings.Contains(problem, "nothing was") {
				t.Fatalf("exit %d, verification %v\n%s", exit, v, output)
			}
			if slices.Contains(keys, "DELETE /api/config/config_entries/options/flow/o1") != tc.aborted ||
				slices.ContainsFunc(keys, func(k string) bool { return strings.HasPrefix(k, "POST /api/config/config_entries/options/flow/") }) {
				t.Fatalf("requests = %v", keys)
			}
		})
	}
}

// Only a helper's config-flow step that created an entry is verified: not
// another integration, an options or subentry flow, a flow's start, a form,
// or an error. Those send nothing more and print the Relay's answer as is.
func TestRelayCoreVerifiesNothingElse(t *testing.T) {
	created := func(handler string) map[string]any {
		return map[string]any{"status": 200, "body": map[string]any{"type": "create_entry", "handler": handler, "result": map[string]any{"entry_id": "e1"}}}
	}
	for name, tc := range map[string]struct {
		path   string
		answer map[string]any
	}{
		"integration":  {"/api/config/config_entries/flow/f1", created("fritz")},
		"options flow": {"/api/config/config_entries/options/flow/f1", created("statistics")},
		"subentry":     {"/api/config/config_entries/subentries/flow/f1", created("statistics")},
		"flow start":   {"/api/config/config_entries/flow", created("statistics")},
		"form":         {"/api/config/config_entries/flow/f1", map[string]any{"status": 200, "body": map[string]any{"type": "form", "flow_id": "f1", "handler": "statistics", "step_id": "options", "data_schema": []any{}}}},
		"error":        {"/api/config/config_entries/flow/f1", map[string]any{"status": 400, "body": map[string]any{"message": "bad"}}},
	} {
		t.Run(name, func(t *testing.T) {
			home := helperHome()
			home["POST "+tc.path] = tc.answer
			_, _, envelope, keys := submitHelperStep(t, home, tc.path)
			if _, has := envelope["verification"]; has || len(keys) != 1 {
				t.Fatalf("envelope %v, requests %v", envelope, keys)
			}
		})
	}
}

// A secret the options form shows is redacted before it becomes a value.
func TestRelayCoreHelperVerificationRedactsSecretOptions(t *testing.T) {
	home := helperHome()
	home["POST /api/config/config_entries/options/flow"] = map[string]any{"status": 200, "body": map[string]any{"type": "form", "flow_id": "o1", "handler": "e1", "step_id": "init", "data_schema": []any{
		map[string]any{"name": "api_key", "selector": map[string]any{"text": map[string]any{"type": "password"}}, "description": map[string]any{"suggested_value": "hunter2"}},
	}}}
	_, output, envelope, _ := submitHelperStep(t, home, "/api/config/config_entries/flow/f1")
	data, _ := json.Marshal(envelope)
	if strings.Contains(string(data), "hunter2") || strings.Contains(output, "hunter2") || verificationOf(t, envelope)["passed"] != true {
		t.Fatalf("secret in output: %s", data)
	}
}

// The Go list is the helper skill's: a domain added there is verified too.
func TestHelperFlowDomainsMatchTheSkill(t *testing.T) {
	skill, err := os.ReadFile(filepath.Join("..", "skills", "helper", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	line := regexp.MustCompile("(?m)^- \\*\\*Config-entry family\\*\\*.*\\n  - (.+)$").FindSubmatch(skill)
	if line == nil {
		t.Fatal("no config-entry domain list in the helper skill's Scope")
	}
	var listed []string
	for _, m := range regexp.MustCompile("\x60([a-z_]+)\x60").FindAllSubmatch(line[1], -1) {
		listed = append(listed, string(m[1]))
	}
	if !slices.Equal(listed, helperFlowDomains) {
		t.Fatalf("skill lists %v, the CLI verifies %v", listed, helperFlowDomains)
	}
}

func anyStrings(v any) []string {
	var out []string
	list, _ := v.([]any)
	for _, x := range list {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}
