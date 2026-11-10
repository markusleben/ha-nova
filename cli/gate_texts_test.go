package main

import (
	"net/http"
	"strings"
	"testing"
)

// /texts answers every page text in Home Assistant's language, with the
// approver's name through sayText and the numbers left for the Relay; only
// with the gate secret.
func TestGateWorkerTexts(t *testing.T) {
	f := newFakeHA()
	paths, _ := setupVerbRelayTest(t, f.reply)
	post := gateTestServer(t, paths)
	for _, tc := range []struct{ language, lang, name string }{{"en-GB", "en", "“Anna”"}, {"de-CH", "de", "„Anna“"}} {
		f.config = map[string]any{"language": tc.language}
		status, out := post("/texts", gateTestSecret, map[string]any{"name": "Anna\u202e"})
		texts, _ := out["texts"].(map[string]any)
		if status != http.StatusOK || out["lang"] != tc.lang {
			t.Fatalf("%s: HTTP %d %v", tc.language, status, out)
		}
		want := []string{"window.cancel"}
		for key := range confirmCatalogs["en"] {
			if strings.HasPrefix(key, "page.") {
				want = append(want, key)
			}
		}
		if len(texts) != len(want) {
			t.Errorf("%s: %d texts, want %d", tc.lang, len(texts), len(want))
		}
		for _, key := range want {
			if text, _ := texts[key].(string); text == "" {
				t.Errorf("%s: %q missing", tc.lang, key)
			}
		}
		if texts["page.approve"] != confirmT(tc.lang, "page.approve") || texts["window.cancel"] != confirmT(tc.lang, "window.cancel") {
			t.Errorf("%s: buttons %v / %v", tc.lang, texts["page.approve"], texts["window.cancel"])
		}
		for key, placeholder := range map[string]string{"page.ends_in": "{minutes}", "page.more": "{count}", "page.waiting_many": "{count}"} {
			if text, _ := texts[key].(string); !strings.Contains(text, placeholder) {
				t.Errorf("%s: %q = %q, want %s kept", tc.lang, key, text, placeholder)
			}
		}
		if text, _ := texts["page.signed_in"].(string); !strings.Contains(text, " "+tc.name+".") {
			t.Errorf("%s: signed in %q, want %s", tc.lang, text, tc.name)
		}
	}
	for _, token := range []string{"", strings.Repeat("0", 64)} {
		if status, _ := post("/texts", token, map[string]any{"name": "Anna"}); status != http.StatusUnauthorized {
			t.Errorf("token %q: HTTP %d, want 401", token, status)
		}
	}
	if status, _ := post("/texts", gateTestSecret, map[string]any{"name": "Anna", "ask": "x"}); status != http.StatusBadRequest {
		t.Errorf("unknown field: HTTP %d, want 400", status)
	}
}
