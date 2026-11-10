package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// The confirmation Markus got on 2026-11-10 (HANO-33): 8 long-term
// statistics imports into the energy totals of 8 air conditioners. Each
// step said its raw entity id, two ISO timestamps, and the overwrite; the
// reason said it again; the macOS window cut the phone's line and the
// instruction. It now reads as one line with names and months, the
// overwrite once, and the instruction first.
var klimaImports = []struct {
	room        string
	rows        int
	first, last string
}{
	{"arbeitszimmer_eg", 1161, "2024-11-06T12:00:00+00:00", "2026-01-09T23:00:00+00:00"},
	{"wintergarten_eg", 1328, "2024-11-06T10:00:00+00:00", "2026-01-10T13:00:00+00:00"},
	{"arbeitszimmer_og", 300, "2025-11-19T11:00:00+00:00", "2026-01-10T15:00:00+00:00"},
	{"kinderzimmer_og", 439, "2025-11-20T04:00:00+00:00", "2026-01-10T15:00:00+00:00"},
	{"fernsehzimmer_eg", 92, "2025-11-19T15:00:00+00:00", "2026-01-10T12:00:00+00:00"},
	{"spielzimmer_eg", 93, "2025-11-19T12:00:00+00:00", "2026-01-10T07:00:00+00:00"},
	{"schlafzimmer_og", 2500, "2025-02-10T09:00:00+00:00", "2025-11-12T06:00:00+00:00"},
	{"schlafzimmer_og", 758, "2025-11-12T08:00:00+00:00", "2026-01-10T10:00:00+00:00"},
}

// klimaPlan plans the 8 imports in a home that speaks lang; friendly names
// as Home Assistant has them ("Klimagerät Arbeitszimmer EG Energie").
func klimaPlan(t *testing.T, lang string) (*tierEngine, verbEnvelope, storedPlan) {
	t.Helper()
	handlers := map[string]func(map[string]any) any{
		"GET /api/config": func(map[string]any) any {
			return map[string]any{"status": 200, "body": map[string]any{"language": lang}}
		},
	}
	var items []rawRequest
	for _, imp := range klimaImports {
		id := "sensor.klimagerat_" + imp.room + "_energy_total"
		words := strings.Split(imp.room, "_")
		name := "Klimagerät " + strings.ToUpper(words[0][:1]) + words[0][1:] + " " + strings.ToUpper(words[1]) + " Energie"
		handlers["GET /api/states/"+id] = func(map[string]any) any {
			return map[string]any{"status": 200, "body": map[string]any{"entity_id": id, "state": "12.5", "attributes": map[string]any{"friendly_name": name}}}
		}
		var rows []any
		for i := range imp.rows {
			start := imp.first
			if i == imp.rows-1 {
				start = imp.last
			}
			rows = append(rows, map[string]any{"start": start, "sum": i})
		}
		payload, _ := json.Marshal(map[string]any{"type": "recorder/import_statistics", "stats": rows,
			"metadata": map[string]any{"statistic_id": id, "source": "recorder", "has_sum": true, "unit_of_measurement": "kWh"}})
		items = append(items, rawRequest{Endpoint: "ws", Payload: payload})
	}
	paths, _ := rawRelay(t, handlers)
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.close)
	env := verbEnvelope{}
	e, plan, err := computeRawPlan(s, &env, items)
	if err != nil {
		t.Fatal(err)
	}
	return e, env, plan
}

// The window, the chat, and the phone say the 8 imports at a glance, in
// both languages: one line, every statistic by name, months instead of
// timestamps, the overwrite once, and the instruction first. The text
// Markus saw is over the window budget (it showed cut).
func TestStatisticsImportsReadAtAGlance(t *testing.T) {
	names := "„Klimagerät Arbeitszimmer EG Energie“, „Klimagerät Wintergarten EG Energie“, „Klimagerät Arbeitszimmer OG Energie“, „Klimagerät Kinderzimmer OG Energie“, " +
		"„Klimagerät Fernsehzimmer EG Energie“, „Klimagerät Spielzimmer EG Energie“ und „Klimagerät Schlafzimmer OG Energie“"
	enNames := strings.NewReplacer("„", "“", "“", "”", " und ", " and ").Replace(names)
	for _, c := range []struct {
		lang, list, overwrite, title, phone string
	}{
		{"de", "Ich führe gleich diese 8 Schritte in Home Assistant aus, in dieser Reihenfolge:\n" +
			"Schritte 1–8: In 7 Langzeitstatistiken insgesamt 6.671 Stundenwerte importieren (Nov. 2024 – Jan. 2026): " + names + ".\n" +
			"Das schreibt Langzeitstatistiken; schon gespeicherte Werte für dieselben Stunden werden überschrieben, deshalb musst du das bestätigen. Noch wurde nichts ausgeführt.",
			"überschrieben", "8 Schritte in Home Assistant bestätigen?", "Schritte 1–8: In 7 Langzeitstatistiken insgesamt 6.671 Stundenwerte importieren (Nov. 2024 – Jan. 2026). " +
				"Das schreibt Langzeitstatistiken; schon gespeicherte Werte für dieselben Stunden werden überschrieben. "},
		{"en", "I'm about to do these 8 steps in Home Assistant, in this order:\n" +
			"Steps 1–8: Import 6,671 hourly values into 7 long-term statistics (Nov 2024 – Jan 2026): " + enNames + ".\n" +
			"This writes long-term statistics; values already stored for the same hours are overwritten, so I need you to confirm it. Nothing has run yet.",
			"overwritten", "Confirm 8 steps in Home Assistant?", "Steps 1–8: Import 6,671 hourly values into 7 long-term statistics (Nov 2024 – Jan 2026). " +
				"This writes long-term statistics; values already stored for the same hours are overwritten. "},
	} {
		e, env, plan := klimaPlan(t, c.lang)
		window := windowMessage(c.lang, confirmText(plan, false), true)
		ask := phonePlanAsk(c.lang, sayAsk(e, &env, plan, ""), false)
		if window != windowAsked(c.lang)+"\n"+windowBoth(c.lang)+"\n\n"+c.list {
			t.Errorf("%s window:\n%s", c.lang, window)
		}
		if lines, fits := windowFits(c.lang, confirmText(plan, false)); !fits || lines > 20 {
			t.Errorf("%s window: %d lines", c.lang, lines)
		}
		if !strings.HasPrefix(env.Say, c.list) || ask.title != c.title || ask.body != c.phone+phoneRequested(c.lang) {
			t.Errorf("%s say %q\nphone %q %q", c.lang, env.Say, ask.title, ask.body)
		}
		for surface, text := range map[string]string{"window": window, "say": env.Say, "phone": ask.body} {
			if strings.Count(text, c.overwrite) != 1 || strings.Contains(text, "sensor.") || strings.Contains(text, "+00:00") || strings.Contains(text, "T12:00") {
				t.Errorf("%s %s: the overwrite once, no ids or timestamps: %q", c.lang, surface, text)
			}
		}
	}
	before := "Ich führe gleich diese 8 Schritte in Home Assistant aus, in dieser Reihenfolge:"
	for i, imp := range klimaImports {
		before += fmt.Sprintf("\n%d. %d Zeilen in die Langzeitstatistik „sensor.klimagerat_%s_energy_total“ von „%s“ bis „%s“ (gespeicherte Zeilen für dieselben Stunden werden überschrieben) importieren.",
			i+1, imp.rows, imp.room, imp.first, imp.last)
	}
	before += " Das schreibt Langzeitstatistiken; schon gespeicherte Zeilen für dieselben Stunden werden überschrieben, deshalb musst du das bestätigen. Noch wurde nichts ausgeführt."
	if lines, fits := windowFits("de", before); fits {
		t.Errorf("the text that showed cut fits the budget (%d lines): the budget is too loose", lines)
	}
}

// The window budget is a ratchet: never loosen it to fit a text (shorten
// the text or split the plan). A typed plan names everything it confirms,
// never "N more"; one too long for the window is refused when planned,
// never shown cut; deletes alike in a row share one line naming each one.
func TestWindowBudgetRatchet(t *testing.T) {
	if windowWrap < 46 || windowMaxLines > 44 || phoneWhyLimit > 300 {
		t.Fatalf("a loosened budget: wrap %d, lines %d, phone %d", windowWrap, windowMaxLines, phoneWhyLimit)
	}
	f := newFakeHA()
	var locks []string
	for i := 1; i <= 30; i++ {
		id := fmt.Sprintf("lock.door_%02d", i)
		f.ent(id, "zwave_js", "locked", map[string]any{"friendly_name": fmt.Sprintf("Haustür Nebeneingang Garage Nord %02d %s", i, strings.Repeat("y", 20))})
		locks = append(locks, id)
	}
	h := newWriteHarness(t, f)
	useWindow(t, windowConfirmed)
	env := h.plan(append([]string{"lock.unlock"}, locks[:12]...)...)
	stored, err := readPlanFile(filepath.Join(planDir(h.paths), env.Plan.ID+".json"))
	if err != nil || env.Plan.Channel != channelWindow || !strings.Contains(stored.Prompt, " and “Haustür Nebeneingang Garage Nord 12 "+strings.Repeat("y", 20)+"”.") || strings.Contains(stored.Prompt, "more") {
		t.Fatalf("12 locks: %v %q", err, stored.Prompt)
	}
	before := h.planFiles()
	if exit, env := h.run(runPlanCommand, append([]string{"call", "lock.unlock"}, locks...)...); exit != verbExitRefused || env.Error.Code != "PLAN_TOO_LONG" ||
		strings.Join(h.planFiles(), ",") != strings.Join(before, ",") {
		t.Fatalf("30 locks: exit %d %+v", exit, env.Error)
	}
	paths, _ := rawRelay(t, nil)
	kinds := []string{"input_boolean", "counter"}
	for _, mixed := range []bool{false, true} {
		var items []string
		pad := strings.Repeat("z", map[bool]int{false: 4, true: 28}[mixed])
		for i := 1; i <= 20; i++ {
			kind := kinds[0]
			if mixed {
				kind = kinds[i%2]
			}
			items = append(items, fmt.Sprintf(`{"endpoint":"ws","payload":{"type":"%s/delete","%s_id":"helper_%02d_%s"}}`, kind, kind, i, pad))
		}
		exit, env := planRaw(t, paths, "["+strings.Join(items, ",")+"]")
		switch {
		case mixed && (exit != verbExitRefused || env.Error.Code != "PLAN_TOO_LONG"):
			t.Errorf("20 deletes of two kinds: exit %d %+v", exit, env.Error)
		case !mixed && (exit != verbExitConfirmation || env.Plan.Channel != channelWindow || !strings.Contains(env.Say, "\nSteps 1–20: Delete the toggle helper “helper_01_"+pad+"”, ") ||
			!strings.Contains(env.Say, " and the toggle helper “helper_20_"+pad+"”.\n")):
			t.Errorf("20 deletes alike: exit %d %+v %q", exit, env.Error, env.Say)
		}
	}
}

// A phone ask too long for the notification cuts the steps it names, never
// why they need the user nor how many other steps come along.
func TestPhoneCutKeepsTheReasons(t *testing.T) {
	paths, _ := rawRelay(t, nil)
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	var items []rawRequest
	for i := 1; i <= 12; i++ {
		kind := []string{"input_boolean", "counter"}[i%2]
		items = append(items, rawRequest{Endpoint: "ws", Payload: json.RawMessage(fmt.Sprintf(`{"type":"%s/delete","%s_id":"helper_%02d_%s"}`, kind, kind, i, strings.Repeat("z", 28)))})
	}
	items = append(items, rawRequest{Endpoint: "ws", Payload: json.RawMessage(`{"type":"input_boolean/create","name":"Guest"}`)})
	env := verbEnvelope{}
	e, plan, err := computeRawPlan(s, &env, items)
	if err != nil {
		t.Fatal(err)
	}
	ask := phonePlanAsk("en", sayAsk(e, &env, plan, ""), false)
	why := strings.TrimSuffix(ask.body, phoneRequested("en"))
	if !strings.HasPrefix(why, "Step 1: Delete the counter “helper_01_") || !strings.Contains(why, confirmT("en", "phone.more_on_computer")+". This deletes something in Home Assistant.") ||
		!strings.HasSuffix(why, "And 1 other step. ") || len([]rune(why)) > phoneWhyLimit+2 {
		t.Fatalf("phone body %q", ask.body)
	}
}
