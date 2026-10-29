package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// germanHome: Home Assistant in German (docs/work/2026-10-28-confirmation-
// localization-spec.md, PR 2b) with its own names for lock and light states
// and for climate.set_temperature; counter.reset has only an English name
// (the fake answers it, as Home Assistant does). The front lock is “Haustür”.
func germanHome(t *testing.T) (*writeHarness, func(id string) storedPlan) {
	t.Helper()
	f := newFakeHA()
	f.config = map[string]any{"language": "de"}
	f.translations = map[string]map[string]string{
		"de/services": {"component.climate.services.set_temperature.name": "Temperatur einstellen", "component.lock.services.unlock.name": "Aufsperren"},
		"en/services": {"component.climate.services.set_temperature.name": "Set target temperature", "component.lock.services.unlock.name": "Unlock", "component.counter.services.reset.name": "Reset"},
		"de/entity_component": {"component.lock.entity_component._.state.locked": "Verriegelt", "component.lock.entity_component._.state.unlocked": "Entriegelt",
			"component.lock.entity_component._.state.jammed": "Blockiert", "component.light.entity_component._.state.on": "An", "component.light.entity_component._.state.off": "Aus"},
	}
	f.states["lock.front"]["attributes"] = map[string]any{"friendly_name": "Haustür"}
	f.ent("climate.living", "", "heat", map[string]any{"friendly_name": "Wohnzimmer", "temperature": 20.0})
	f.ent("counter.cups", "", "3", map[string]any{"friendly_name": "Tassen"})
	f.ent("switch.porch", "", "off", map[string]any{"friendly_name": "Veranda"})
	f.ent("cover.garage", "zwave_js", "open", map[string]any{"device_class": "garage", "friendly_name": "Garage", "supported_features": 15.0})
	f.ent("lock.shed", "zwave_js", "unavailable", map[string]any{"friendly_name": "Schuppen"})
	f.services = append(f.defaultServices(), map[string]any{"domain": "counter", "services": map[string]any{"reset": map[string]any{"target": map[string]any{}}}})
	h := newWriteHarness(t, f)
	return h, func(id string) storedPlan {
		t.Helper()
		plan, err := readPlanFile(filepath.Join(planDir(h.paths), id+".json"))
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
}

const germanUnlock = "Ich werde gleich „Haustür“ entriegeln (Verriegelt → Entriegelt). Das entriegelt eine Tür, deshalb musst du das bestätigen. Noch wurde nichts ausgeführt."

// A German home gets the confirmation in German, with Home Assistant's own
// names for the states of what it acts on and for an action HA NOVA has no
// verb for; they are stored with the plan. HA NOVA's own verbs, which carry
// the security words, are never replaced by Home Assistant's.
func TestGermanHomeConfirmsInGerman(t *testing.T) {
	h, stored := germanHome(t)
	env := h.plan("lock.unlock", "lock.front")
	plan := stored(env.Plan.ID)
	if !strings.HasPrefix(env.Say, germanUnlock+"\n"+confirmT("de", "say.chat_note")) || !strings.Contains(env.Say, "**"+env.Plan.Code+"**") || plan.Prompt != germanUnlock ||
		plan.Lang != "de" || plan.Words["state:lock.jammed"] != "Blockiert" || plan.Words["state:light.on"] != "" || plan.Words["service:lock.unlock"] != "" || strings.Contains(env.Say, "Aufsperren") {
		t.Fatalf("say %q\nplan %s %v", env.Say, plan.Lang, plan.Words)
	}
	env = h.plan("climate.set_temperature", "climate.living", "--set", "temperature=21")
	if !strings.HasPrefix(env.Say, "Ich werde gleich „Temperatur einstellen“ für „Wohnzimmer“ ausführen (temperature 20 → 21). ") ||
		stored(env.Plan.ID).Words["service:climate.set_temperature"] != "Temperatur einstellen" {
		t.Fatalf("an action named by Home Assistant: %q %v", env.Say, stored(env.Plan.ID).Words)
	}
}

// What Home Assistant has no German name for: an action makes the whole
// confirmation English and says why (never mixed); a state stays as it is;
// HA NOVA's own words for unknown and unavailable are German.
func TestGermanHomeWithoutHomeAssistantNames(t *testing.T) {
	h, stored := germanHome(t)
	env := h.plan("counter.reset", "counter.cups")
	notice := func(service string) verbNotice {
		return verbNotice{Kind: "language", Text: "HA NOVA couldn't get a short name for " + service + " in Home Assistant's language (de), so this confirmation is in English."}
	}
	if !strings.HasPrefix(env.Say, "I'm about to reset “Tassen”.") || len(env.Notices) != 1 || env.Notices[0] != notice("counter.reset") || stored(env.Plan.ID).Lang != "en" || stored(env.Plan.ID).Words != nil {
		t.Fatalf("no German name for the action: %q %+v", env.Say, env.Notices)
	}
	// Its apply says nothing about a language: no ask is shown again.
	if _, applied := h.run(runApplyCommand, "--plan", env.Plan.ID); slices.ContainsFunc(applied.Notices, func(n verbNotice) bool { return n.Kind == "language" }) {
		t.Fatalf("apply: %+v", applied.Notices)
	}
	// A name without a letter once defanged or too long for the title, or an
	// English read that fails (a German read alone can't tell a German name
	// from Home Assistant's English fill-in), makes it English too.
	set := func(name string) {
		h.f.translations["de/services"]["component.climate.services.set_temperature.name"] = name
	}
	for _, name := range []string{"“”​", strings.Repeat("Temperatur ", 4) + "einstellen"} {
		set(name)
		if env := h.plan("climate.set_temperature", "climate.living", "--set", "temperature=21"); !strings.HasPrefix(env.Say, "I'm about to ") || len(env.Notices) != 1 || env.Notices[0] != notice("climate.set_temperature") {
			t.Errorf("name %q: %q %+v", name, env.Say, env.Notices)
		}
	}
	set("Temperatur einstellen")
	h.f.extra = func(endpoint string, p map[string]any) (any, bool) {
		return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "timeout"}, p["type"] == "frontend/get_translations" && p["language"] == "en"
	}
	if env := h.plan("climate.set_temperature", "climate.living", "--set", "temperature=21"); !strings.HasPrefix(env.Say, "I'm about to ") || len(env.Notices) != 1 {
		t.Errorf("English read failed: %q %+v", env.Say, env.Notices)
	}
	h.f.extra = nil
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"switch.turn_on", "switch.porch"}, "Ich werde gleich „Veranda“ einschalten (off → on)."},
		{[]string{"cover.set_cover_position", "cover.garage", "--set", "position=0"}, "Ich werde gleich die Position von „Garage“ einstellen (position unbekannt → 0%)."},
		{[]string{"lock.unlock", "lock.shed"}, "Ich werde gleich „Schuppen“ entriegeln (nicht verfügbar → Entriegelt)."},
	} {
		if env := h.plan(c.args...); !strings.HasPrefix(env.Say, c.want) || len(env.Notices) != 0 {
			t.Errorf("%v: %q %+v", c.args, env.Say, env.Notices)
		}
	}
}

// The phone asks in German, and its result is German too; the chat's
// result line stays English.
func TestGermanHomeAsksOnThePhone(t *testing.T) {
	h, _ := germanHome(t)
	p := usePhones(t, "mobile_app_iphone")
	env := h.plan("lock.unlock", "lock.front")
	if !strings.HasSuffix(env.Say, "Noch wurde nichts ausgeführt. Bestätige auf deinem Handy. Tippe in der Benachrichtigung auf „Entriegeln“. Um die Knöpfe zu sehen, halte die Benachrichtigung auf einem iPhone gedrückt oder zieh sie auf Android nach unten.") {
		t.Fatalf("phone plan: %q", env.Say)
	}
	p.answer = func(ok, _ string) []phoneEvent { return []phoneEvent{tap(ok, "owner-id", "REMOTE")} }
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != 0 || len(p.sent) != 2 || applied.Say != "Done: “Haustür” is unlocked now. I checked it." {
		t.Fatalf("apply: exit %d %+v sent %v", exit, applied, p.sent)
	}
	actions := p.sent[0]["data"].(map[string]any)["actions"].([]any)
	if p.sent[0]["title"] != "„Haustür“ entriegeln?" || p.sent[0]["message"] != "Angefordert von „"+phoneHost("de")+"“. Zum Antworten gedrückt halten (iPhone) oder runterziehen (Android)." ||
		actions[0].(map[string]any)["title"] != "Entriegeln" || actions[1].(map[string]any)["title"] != "Abbrechen" ||
		p.sent[1]["title"] != "Erledigt: „Haustür“ entriegeln" || p.sent[1]["message"] != "Home Assistant bestätigt das." {
		t.Fatalf("phone texts %v", p.sent)
	}
	// Home Assistant's name never becomes a button: no word list can tell
	// that “Abbrechen” or “Stopp” dismisses (Design 8).
	env = h.plan("climate.set_temperature", "climate.living", "--set", "temperature=21")
	if !strings.Contains(env.Say, "Tippe in der Benachrichtigung auf „Bestätigen“.") ||
		(actionGoal{label: "Abbrechen", named: true, names: []string{"“Tee”"}}).button("de") != "Bestätigen" {
		t.Fatalf("a named action's button: %q", env.Say)
	}
}

func TestGermanHomeAsksInTheWindow(t *testing.T) {
	h, _ := germanHome(t)
	w := useWindow(t, windowConfirmed)
	env := h.plan("lock.unlock", "lock.front")
	if !strings.HasSuffix(env.Say, "Noch wurde nichts ausgeführt. Bestätige im Fenster auf diesem Computer.") {
		t.Fatalf("window plan: %q", env.Say)
	}
	if exit, _ := h.run(runApplyCommand, "--plan", env.Plan.ID); exit != 0 || len(w.messages) != 1 || w.langs[0] != "de" ||
		w.messages[0] != germanUnlock+"\n\n"+confirmT("de", "window.asked") {
		t.Fatalf("window: exit %d %q %v", exit, w.messages, w.langs)
	}
}

// Home Assistant's words are read once, when the plan is made: the plan a
// drift makes and apply keep them while Home Assistant renames its states.
func TestHomeAssistantWordsStayWithThePlan(t *testing.T) {
	h, stored := germanHome(t)
	w := useWindow(t, windowConfirmed)
	original := h.f.translations["de/entity_component"]
	renamed := map[string]string{"component.lock.entity_component._.state.locked": "Zu", "component.lock.entity_component._.state.unlocked": "Auf", "component.lock.entity_component._.state.jammed": "Klemmt"}
	id := h.plan("lock.unlock", "lock.front").Plan.ID
	words := stored(id).Words
	h.f.translations["de/entity_component"] = renamed
	h.f.states["lock.front"]["state"] = "jammed"
	exit, env := h.run(runApplyCommand, "--plan", id)
	if exit != 2 || env.Status != "drift" || env.Plan == nil || !strings.HasPrefix(env.Say, "Seit der letzten Vorschau hat sich „Haustür“ geändert: Zustand war Verriegelt, jetzt Blockiert. ") ||
		!jsonEqual(stored(env.Plan.ID).Words, words) {
		t.Fatalf("drift: exit %d %q %v", exit, env.Say, stored(env.Plan.ID).Words)
	}
	h.f.states["lock.front"]["state"] = "locked"
	h.f.translations["de/entity_component"] = original
	id = h.plan("lock.unlock", "lock.front").Plan.ID
	h.f.translations["de/entity_component"] = renamed
	if exit, _ := h.run(runApplyCommand, "--plan", id); exit != 0 || len(w.messages) != 1 || !strings.HasPrefix(w.messages[0], germanUnlock) {
		t.Fatalf("apply after a rename: exit %d %q", exit, w.messages)
	}
}

// Home Assistant's words are sealed with the plan: an edited, added, or
// removed word is refused before any window opens.
func TestEditedHomeAssistantWordsAreRefused(t *testing.T) {
	h, _ := germanHome(t)
	w := useWindow(t)
	for name, edit := range map[string]func(map[string]any){
		"a word changed":    func(p map[string]any) { p["words"].(map[string]any)["state:lock.unlocked"] = "Verriegelt" },
		"a word added":      func(p map[string]any) { p["words"].(map[string]any)["service:lock.unlock"] = "Öffnen" },
		"the words removed": func(p map[string]any) { delete(p, "words") },
	} {
		id := h.plan("lock.unlock", "lock.front").Plan.ID
		h.tamper(id, edit)
		if exit, env := h.run(runApplyCommand, "--plan", id); exit != 4 || env.Error.Code != "PLAN_INVALID" || len(w.messages) != 0 || len(h.posts()) != 0 {
			t.Fatalf("%s: exit %d %+v windows %d posts %d", name, exit, env.Error, len(w.messages), len(h.posts()))
		}
	}
}

// A raw plan speaks English in a German home until its lines have German
// texts (Decision 1).
func TestRawPlanStaysEnglishInAGermanHome(t *testing.T) {
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{
		"GET /api/config": func(map[string]any) any {
			return map[string]any{"status": 200, "body": map[string]any{"language": "de"}}
		},
	})
	exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"input_boolean/create","name":"Guest mode"}},
		{"endpoint":"ws","payload":{"type":"input_boolean/create","name":"Night mode"}}]`)
	if exit != verbExitConfirmation || env.Plan == nil || !strings.Contains(env.Say, "1. Create the toggle helper “Guest mode”.") || !strings.Contains(env.Say, sayYesAsk("en")) {
		t.Fatalf("raw plan: exit %d %q", exit, env.Say)
	}
	if plan, err := readPlanFile(filepath.Join(planDir(paths), env.Plan.ID+".json")); err != nil || plan.Lang != "en" || plan.Words != nil {
		t.Fatalf("raw plan file: %+v %v", plan, err)
	}
}

// A settings ask speaks Home Assistant's language now, never a plan's: the
// window that turns windows off, the link notification, and the link hint,
// also for a plan made while Home Assistant was English (Decision 2).
func TestSettingsAsksSpeakTheHomesLanguageNow(t *testing.T) {
	h, _ := germanHome(t)
	w := useWindow(t, windowConfirmed)
	if exit := runConfirmWindowSetting(h.paths, []string{"off"}); exit != 0 || len(w.langs) != 1 || w.langs[0] != "de" || w.messages[0] != confirmT("de", "settings.window_off") {
		t.Fatalf("window off: exit %d %q %v", exit, w.messages, w.langs)
	}
	h, p, _ := familyLink(t)
	h.f.config = map[string]any{"language": "de"}
	p.tapBy = tapFrom("mobile_app_annas_pixel", "anna-id", true)
	if exit, out := runLink(t, h, "1\n", phoneLinkRun{interactive: true, offer: true}); exit != 0 || len(p.sent) != 2 || p.sent[0]["title"] != "HA NOVA hier bestätigen?" ||
		p.sent[0]["message"] != "„Erlauben“ tippen. Sonst runterziehen. Angefordert von „"+phoneHost("de")+"“." || p.sent[1]["title"] != "Mit HA NOVA verbunden" ||
		!strings.Contains(out, "Only “Anna’s Pixel” gets HA NOVA confirmations") {
		t.Fatalf("link: exit %d %v %q", exit, p.sent, out)
	}
	if exit, _ := runUnlink(t, h); exit != 0 || len(p.sent) < 3 || p.sent[2]["title"] != "HA NOVA nicht mehr bestätigen?" {
		t.Fatalf("unlink: exit %d %v", exit, p.sent)
	}
	pin := func(h *writeHarness) { // #719's pin: no phone chosen yet
		t.Helper()
		doc, err := loadConfigDocument(h.paths.ConfigFile)
		if err != nil {
			t.Fatal(err)
		}
		doc.top[phoneSettingKey] = json.RawMessage(`{"owner-id":"on"}`)
		if err := writeJSONFileIfUnchanged(h.paths.ConfigFile, doc.top, 0o600, doc.source); err != nil {
			t.Fatal(err)
		}
	}
	h, _, _ = familyLink(t)
	h.f.config = map[string]any{"language": "de"}
	pin(h)
	if exit, env := h.run(runPlanCommand, "call", "lock.unlock", "lock.front"); exit != 4 || env.Error.Code != "CONFIRM_UNAVAILABLE" || env.Error.Next != phoneUnlinkedNext("de") ||
		!strings.Contains(env.Error.Next, "“HA NOVA hier bestätigen?” and tap “Erlauben”") {
		t.Fatalf("link hint of a plan: exit %d %+v", exit, env.Error)
	}
	h, _, _ = familyLink(t)
	h.f.config = map[string]any{"language": "en"}
	useWindow(t, windowFailed)
	id := h.plan("lock.unlock", "lock.front").Plan.ID
	pin(h)
	h.f.config["language"] = "de"
	if exit, env := h.run(runApplyCommand, "--plan", id); exit != 4 || env.Error.Code != "CONFIRM_UNAVAILABLE" || env.Error.Next != phoneUnlinkedNext("de") {
		t.Fatalf("link hint after an English plan's window failed: exit %d %+v", exit, env.Error)
	}
}

// Android shows two lines of a notification's body and about 30 runes of
// a pop-up's title: every catalog's hint that names no button and every
// settings ask's title fit them.
func TestPhoneTextsFitEveryCatalog(t *testing.T) {
	for lang := range confirmCatalogs {
		for _, key := range []string{"phone.hint.android", "phone.hint.android_more"} {
			if n := utf8.RuneCountInString(confirmT(lang, key)); n > 38 {
				t.Errorf("%s %s: %d runes", lang, key, n)
			}
		}
		for _, ask := range []phoneAsk{phoneLinkAsk(lang), phoneChangeAsk(lang, ""), phoneChangeAsk(lang, "A very long phone name, Pixel 9 Pro XL")} {
			if n := utf8.RuneCountInString(ask.title); n > 30 {
				t.Errorf("%s title %q: %d runes", lang, ask.title, n)
			}
		}
		// Every action's question and result title (an undo's included) keep
		// a usual name whole, in the language's quotes, and cut the longest
		// name a confirmation shows, never dropping it.
		goals := []actionGoal{{label: "Temperatur einstellen", named: true}}
		for key := range confirmCatalogs[lang] {
			if verb, ok := strings.CutPrefix(key, "goal."); ok && verb != "label" && !strings.Contains(verb, ".") {
				frame := map[bool]string{true: "automation"}[slices.Contains([]string{"create", "change", "delete", "restore", "revert"}, verb)] // a config plan's frame
				goals = append(goals, actionGoal{verb: verb, frame: frame})
			}
		}
		usual, longest := sayText("Wohnzimmer Decke", true), sayText(strings.Repeat("Flurlicht ", 7), true)
		opening := string([]rune(confirmQuote(lang, usual))[0])
		for _, g := range goals {
			for _, prefix := range []string{"", confirmT(lang, "outcome.failed"), confirmT(lang, "outcome.not_confirmed"), confirmT(lang, "outcome.already_done")} {
				g.names = []string{usual}
				if title := g.title(lang, prefix); !strings.Contains(title, confirmQuote(lang, usual)) {
					t.Errorf("%s %s: %q", lang, cmp.Or(g.verb, g.label), title)
				}
				g.names = []string{longest}
				if title := g.title(lang, prefix); utf8.RuneCountInString(title) > phoneTitleLimit || !strings.Contains(title, opening+"Flurlicht") {
					t.Errorf("%s %s, a long name: %q", lang, cmp.Or(g.verb, g.label), title)
				}
			}
		}
	}
}

// A batch in German: the longest list that may show still fits the window,
// with the longer no-copy note; every phone title keeps its count, also
// after a result's prefix; one changed name reads singular.
func TestGermanBatchFitsTheWindowAndThePhone(t *testing.T) {
	f := newFakeHA()
	f.config = map[string]any{"language": "de"}
	name := func(i int) string { return fmt.Sprintf("Long automation name %02d %s", i, strings.Repeat("y", 36)) }
	for i := 1; i <= batchMaxMembers; i++ {
		f.storeConfig(fmt.Sprintf("/api/config/automation/config/l%d", i), map[string]any{"alias": name(i), "actions": []any{step("light.turn_on", "light.hall")}})
	}
	h := newWriteHarness(t, f)
	var deletes []string
	for i := 1; i <= batchMaxMembers; i++ {
		deletes = append(deletes, h.member("delete", "automation."+strings.ReplaceAll(strings.ToLower(name(i)), " ", "_")))
	}
	exit, env := h.batch(deletes...)
	if exit != 3 || env.Plan == nil {
		t.Fatalf("20 deletes: exit %d %+v", exit, env.Error)
	}
	stored, err := readPlanFile(filepath.Join(planDir(h.paths), env.Plan.ID+".json"))
	text := batchNoCopies("de", confirmText(stored, false)) + "\n\n" + windowBoth("de") + "\n" + windowAsked("de")
	lines, chars := strings.Count(text, "\n")+1, utf8.RuneCountInString(text)
	t.Logf("the longest German window: %d lines, %d characters", lines, chars)
	if err != nil || stored.Lang != "de" || lines > batchWindowLines || chars > batchWindowChars ||
		!strings.Contains(text, "\n20. Automatisierung „"+name(20)+"“ löschen.\n") || !strings.Contains(text, confirmT("de", "batch.no_copies")) {
		t.Fatalf("%d lines, %d characters (%v): %s", lines, chars, err, text)
	}
	member := func(op, domain string) batchMember {
		return batchMember{stored: storedPlan{ConfigAction: &configAction{Op: op, Domain: domain}, ConfigRequest: &configRequest{Op: op, Domain: domain}}}
	}
	for _, ops := range [][2]string{{"delete", "delete"}, {"write", "write"}, {"write", "delete"}} {
		for _, kinds := range [][2]string{{"automation", "automation"}, {"script", "script"}, {"automation", "script"}} {
			rp := batchReplan{lang: "de"}
			for i := range batchMaxMembers {
				rp.members = append(rp.members, member(ops[i%2], kinds[i%2]))
			}
			for _, prefix := range []string{"", confirmT("de", "batch.phone.partly_title"), confirmT("de", "outcome.failed")} {
				if title := (actionGoal{whole: batchPhoneTitle(rp)}).title("de", prefix); utf8.RuneCountInString(title) >= phoneTitleLimit || !strings.Contains(title, "20 ") {
					t.Errorf("%v %v %q: %q", ops, kinds, prefix, title)
				}
			}
		}
	}
	if one, two := confirmT("de", "batch.drift", "count", 1, "names", "“A”"), confirmT("de", "batch.drift", "count", 2, "names", "“A” und “B”"); !strings.Contains(one, "„A“ wurde geändert") || !strings.Contains(two, "„B“ wurden geändert") {
		t.Errorf("drift: %q %q", one, two)
	}
}

// The setup page asks in Home Assistant's language and quotes that ask's
// buttons; the page's own words stay English (setup_page_phone.go).
func TestSetupPageQuotesTheAsksButtonsInGerman(t *testing.T) {
	h, phones, _ := familyLink(t)
	h.f.config = map[string]any{"language": "de"}
	p := newPageStepsTest(t, h.paths, "codex")
	p.flow.pairedThisRun = true
	done := p.runControl()
	ctl := p.waitFor("step 3", accepting("phone")).Control
	if state, _ := json.Marshal(ctl); ctl.Phone != "pick" || !strings.Contains(string(state), `"phone_buttons":{"allow":"Erlauben","move":"Wechseln"}`) {
		t.Fatalf("control = %s", state)
	}
	p.click("phone", "dev-anna")
	ctl = p.waitFor("the timeout", func(s setupPageState) bool { return s.Control.PhoneProblem != nil }).Control
	if ctl.PhoneProblem.Key != "setup.page.control.phone_timeout" || ctl.PhoneProblem.Args["allow"] != "Erlauben" || len(phones.sent) == 0 || phones.sent[0]["title"] != phoneLinkTitle("de") {
		t.Fatalf("problem %+v sent %v", ctl.PhoneProblem, phones.sent)
	}
	p.waitFor("finish", accepting("finish"))
	p.click("finish", "")
	<-done
}
