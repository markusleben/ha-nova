package main

import (
	"path/filepath"
	"testing"
)

// languageHarness: a fake Home Assistant whose language the test sets, with
// a German catalog that is a copy of the English one (until PR 2 adds it).
func languageHarness(t *testing.T, answers ...windowAnswer) (*writeHarness, *fakeWindow, func(id string) storedPlan) {
	t.Helper()
	saved := confirmCatalogs
	t.Cleanup(func() { confirmCatalogs = saved })
	confirmCatalogs = map[string]map[string]setupEntry{"en": saved["en"], "de": saved["en"]}
	f := newFakeHA()
	f.config = map[string]any{"language": "de-CH"}
	h := newWriteHarness(t, f)
	stored := func(id string) storedPlan {
		t.Helper()
		plan, err := readPlanFile(filepath.Join(planDir(h.paths), id+".json"))
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
	return h, useWindow(t, answers...), stored
}

// A typed plan stores Home Assistant's language and keeps it to apply: HA
// changing its language meanwhile changes neither the window, nor the chat
// plan that replaces a failed window, nor the plan a drift makes
// (docs/work/2026-10-28-confirmation-localization-spec.md, Design 1-2).
func TestPlanKeepsTheLanguageItWasMadeIn(t *testing.T) {
	h, w, stored := languageHarness(t, windowConfirmed, windowFailed)
	id := h.plan("lock.unlock", "lock.front").Plan.ID
	if lang := stored(id).Lang; lang != "de" {
		t.Fatalf("stored language %q, want de", lang)
	}
	h.f.config["language"] = "en"
	if exit, env := h.run(runApplyCommand, "--plan", id); exit != 0 || len(h.posts()) != 1 || len(w.langs) != 1 || w.langs[0] != "de" {
		t.Fatalf("window after HA switched to English: exit %d %+v posts %d window languages %v", exit, env.Error, len(h.posts()), w.langs)
	}
	h.f.config["language"] = "de"
	id = h.plan("lock.unlock", "lock.front").Plan.ID
	h.f.config["language"] = "en"
	if _, env := h.run(runApplyCommand, "--plan", id); env.Plan == nil || env.Plan.ID == id || stored(env.Plan.ID).Lang != "de" {
		t.Fatalf("chat plan after a failed window: %+v", env)
	}
	h.f.config["language"] = "de"
	id = h.plan("lock.unlock", "lock.front").Plan.ID
	h.f.config["language"] = "en"
	h.f.states["lock.front"]["state"] = "jammed"
	if exit, env := h.run(runApplyCommand, "--plan", id); exit != 2 || env.Status != "drift" || env.Plan == nil || env.Plan.ID == id || stored(env.Plan.ID).Lang != "de" {
		t.Fatalf("drift plan: exit %d %+v", exit, env)
	}
	if lang := stored(h.plan("light.turn_off", "light.kitchen").Plan.ID).Lang; lang != "" {
		t.Fatalf("an ordinary plan read the language: %q", lang)
	}
}

// The language is sealed: an edited one, one moved into the prompt, or one
// this binary has no catalog for is refused before any window opens.
func TestEditedPlanLanguageIsRefused(t *testing.T) {
	h, w, _ := languageHarness(t)
	for name, edit := range map[string]func(map[string]any){
		"another language": func(p map[string]any) { p["lang"] = "en" },
		"language moved into the prompt": func(p map[string]any) {
			p["prompt"], p["lang"] = p["lang"].(string)+"\x00"+p["prompt"].(string), ""
		},
		"a language this binary lacks": func(p map[string]any) { p["lang"] = "fr" },
	} {
		id := h.plan("lock.unlock", "lock.front").Plan.ID
		h.tamper(id, edit)
		if exit, env := h.run(runApplyCommand, "--plan", id); exit != 4 || env.Error.Code != "PLAN_INVALID" || len(w.messages) != 0 || len(h.posts()) != 0 {
			t.Fatalf("%s: exit %d %+v windows %d posts %d", name, exit, env.Error, len(w.messages), len(h.posts()))
		}
	}
}
