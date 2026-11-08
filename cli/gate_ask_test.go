package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// The phone lookup's reads are reads at the gate, so the Relay forwards them
// for the worker, whose own requests pass only as reads (gate.ts).
func TestGatePhoneReadsAreReads(t *testing.T) {
	none := func() (*relaySession, error) { return nil, errors.New("no session") }
	bodies := map[string]string{}
	for _, wsType := range []string{"auth/current_user", "config/auth/list", "config/device_registry/list", "person/list"} {
		bodies[wsType] = fmt.Sprintf(`{"type":%q}`, wsType)
	}
	template, _ := json.Marshal(map[string]any{"method": "POST", "path": "/api/template", "body": map[string]any{"template": phoneTemplate,
		"variables": map[string]any{"device": "", "names": []any{"Pixel"}, "trackers": []any{[]any{"device_tracker.pixel", "u1", "Anna"}}}}})
	for name, body := range bodies {
		if c, err := gateVerdict("ws", []byte(body), none); err != nil || c.Verdict != gatePass || c.Kind != "read" {
			t.Errorf("%s: %+v %v, want pass/read", name, c, err)
		}
	}
	if c, err := gateVerdict("core", template, none); err != nil || c.Verdict != gatePass || c.Kind != "read" {
		t.Errorf("phone template: %+v %v, want pass/read", c, err)
	}
	other, _ := json.Marshal(map[string]any{"method": "POST", "path": "/api/template", "body": map[string]any{"template": "{{ 1 }}"}})
	if c, _ := gateVerdict("core", other, none); c.Kind == "read" {
		t.Errorf("another template is a read: %+v", c)
	}
}

// /ask rates every request and words a typed ask as its plan would: the
// action, its entity ids, and the device that asked. Nothing is worded
// unless something is typed and nothing is refused.
func TestGateWorkerAsk(t *testing.T) {
	f := newFakeHA()
	paths, _ := setupVerbRelayTest(t, f.reply)
	post := gateTestServer(t, paths)
	ask := func(bodies ...string) map[string]any {
		t.Helper()
		requests := []any{}
		for _, body := range bodies {
			requests = append(requests, map[string]any{"endpoint": "core", "body_b64": b64(body)})
		}
		status, out := post("/ask", gateTestSecret, map[string]any{"device": "Markus’ MacBook", "requests": requests})
		if status != http.StatusOK {
			t.Fatalf("ask: HTTP %d %v", status, out)
		}
		return out
	}
	unlock := `{"method":"POST","path":"/api/services/lock/unlock","body":{"entity_id":["lock.front"]}}`
	light := `{"method":"POST","path":"/api/services/light/turn_on","body":{"entity_id":["light.kitchen"]}}`

	if out := ask(light); out["verdict"] != "pass" || out["title"] != nil {
		t.Errorf("a passing ask: %v", out)
	}
	if out := ask(light, `{"method":"POST","path":"/api/events/doorbell"}`, unlock); out["verdict"] != "refuse" || out["title"] != nil {
		t.Errorf("a refused item: %v", out)
	}
	out := ask(unlock)
	title, _ := out["title"].(string)
	body, _ := out["body"].(string)
	items, _ := out["items"].([]any)
	if out["verdict"] != "typed" || out["lang"] != "en" || !strings.HasPrefix(title, "Unlock") || out["button"] == "" || len(items) != 1 {
		t.Fatalf("a typed ask: %v", out)
	}
	if !strings.Contains(body, "(lock.front)") || !strings.Contains(body, "MacBook") {
		t.Errorf("body names neither the entity id nor the device: %q", body)
	}
	if item := items[0].(map[string]any); !strings.HasSuffix(item["text"].(string), "(lock.front)") || item["verdict"] != "typed" {
		t.Errorf("item: %v", item)
	}

	out = ask(light, unlock)
	items, _ = out["items"].([]any)
	body, _ = out["body"].(string)
	if out["verdict"] != "typed" || out["title"] != confirmT("en", "raw.steps", "count", 2)+"?" && !strings.Contains(out["title"].(string), confirmT("en", "raw.steps", "count", 2)) || len(items) != 2 {
		t.Fatalf("a two-step ask: %v", out)
	}
	if !strings.Contains(body, "Step 2: Unlock") || strings.Contains(body, "Step 1") {
		t.Errorf("the body names only the typed step: %q", body)
	}
	if first := items[0].(map[string]any); first["verdict"] != "pass" || !strings.HasSuffix(first["text"].(string), "(light.kitchen)") {
		t.Errorf("the passing step is listed too, for the page: %v", first)
	}

	for name, in := range map[string]map[string]any{
		"no device":     {"requests": []any{map[string]any{"endpoint": "core", "body_b64": b64(light)}}},
		"no requests":   {"device": "Mac", "requests": []any{}},
		"21 requests":   {"device": "Mac", "requests": slices.Repeat([]any{map[string]any{"endpoint": "core", "body_b64": b64(light)}}, 21)},
		"no endpoint":   {"device": "Mac", "requests": []any{map[string]any{"body_b64": b64(light)}}},
		"unknown field": {"device": "Mac", "requests": []any{map[string]any{"endpoint": "core", "body_b64": b64(light)}}, "lang": "de"},
	} {
		if status, _ := post("/ask", gateTestSecret, in); status != http.StatusBadRequest {
			t.Errorf("%s: HTTP %d, want 400", name, status)
		}
	}
	if status, _ := post("/ask", "", map[string]any{"device": "Mac", "requests": []any{}}); status != http.StatusUnauthorized {
		t.Errorf("no bearer: HTTP %d, want 401", status)
	}
}

// /phone asks the Relay's link for the device and counts only the linked
// administrator's own tap; anything else is no approval.
func TestGateWorkerPhone(t *testing.T) {
	h, p, _ := familyLink(t)
	post := gateTestServer(t, h.paths)
	anna := map[string]any{"service": "mobile_app_annas_pixel", "device_id": "dev-anna", "user_id": "anna-id"}
	phone := func(link map[string]any, wait int) (int, map[string]any) {
		return post("/phone", gateTestSecret, map[string]any{"lang": "en", "title": "Unlock “Front”?", "body": "Requested by “Mac” through HA NOVA.",
			"button": "Unlock", "link": link, "wait_ms": wait})
	}
	for _, tc := range []struct {
		name  string
		tap   func(string, string, string) []phoneEvent
		link  map[string]any
		want  string
		user  string
		asked bool
	}{
		{"the linked user confirms", tapFrom("mobile_app_annas_pixel", "anna-id", true), anna, "approved", "anna-id", true},
		{"the linked user cancels", tapFrom("mobile_app_annas_pixel", "anna-id", false), anna, "declined", "", true},
		{"someone else taps", tapFrom("mobile_app_annas_pixel", "kid-id", true), anna, "rejected", "", true},
		{"no answer", nil, anna, "none", "", true},
		{"not an administrator", tapFrom("mobile_app_kids_phone", "kid-id", true), map[string]any{"service": "mobile_app_kids_phone", "device_id": "dev-kid", "user_id": "kid-id"}, "unavailable", "", false},
		{"the Relay's own user", tapFrom("mobile_app_annas_pixel", "supervisor-id", true), map[string]any{"service": "mobile_app_annas_pixel", "device_id": "dev-anna", "user_id": "supervisor-id"}, "unavailable", "", false},
		{"a removed phone", tapFrom("mobile_app_annas_pixel", "anna-id", true), map[string]any{"service": "mobile_app_annas_pixel", "device_id": "dev-gone", "user_id": "anna-id"}, "unavailable", "", false},
	} {
		p.tapBy, p.sent = tc.tap, nil
		status, out := phone(tc.link, 3000)
		if status != http.StatusOK || out["answer"] != tc.want || tc.user != "" && out["user_id"] != tc.user || tc.user == "" && out["user_id"] != nil {
			t.Errorf("%s: HTTP %d %v, want %s %s", tc.name, status, out, tc.want, tc.user)
		}
		if asked := len(p.sent) > 0; asked != tc.asked {
			t.Errorf("%s: asked %v, want %v", tc.name, asked, tc.asked)
		}
		if tc.asked && p.sent[0]["title"] != "Unlock “Front”?" {
			t.Errorf("%s: title %v", tc.name, p.sent[0]["title"])
		}
	}
	for name, wait := range map[string]int{"no wait": 0, "past the ask's lifetime": 180001} {
		if status, _ := phone(anna, wait); status != http.StatusBadRequest {
			t.Errorf("%s: HTTP %d, want 400", name, status)
		}
	}
	if status, _ := phone(map[string]any{"service": "persistent_notification", "device_id": "dev-anna", "user_id": "anna-id"}, 3000); status != http.StatusBadRequest {
		t.Errorf("a notify service that is no phone: HTTP %d, want 400", status)
	}
}
