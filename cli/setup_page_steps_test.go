package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Steps 3 and 4 of the setup page against the fake Home Assistants: what
// the page shows, that nothing changes without a click, and that a save is
// exactly the plan the page showed.

type pageStepsTest struct {
	*setupPageTest
	flow *setupFlow
}

func newPageStepsTest(t *testing.T, paths runtimePaths, clients ...string) *pageStepsTest {
	t.Helper()
	page := startSetupPageTest(t)
	page.launch()
	return &pageStepsTest{page, &setupFlow{paths: paths, ui: page.ui, page: page.ui, selectedClients: clients}}
}

func (p *pageStepsTest) runControl() chan int {
	done := make(chan int, 1)
	go func() { done <- p.flow.pageControl() }()
	return done
}

func (p *pageStepsTest) runHome() chan struct{} {
	done := make(chan struct{})
	go func() { p.flow.pageHome(); close(done) }()
	return done
}

func (p *pageStepsTest) preview(slot, state string) *setupPagePreview {
	p.t.Helper()
	got := p.waitFor(slot+" "+state, func(s setupPageState) bool {
		pv := s.Home.previewIn(slot)
		return pv != nil && pv.State == state && slices.Contains(s.Accepts, "done")
	})
	return got.Home.Previews[slot]
}

func (h *setupPageHome) previewIn(slot string) *setupPagePreview {
	if h == nil {
		return nil
	}
	return h.Previews[slot]
}

// pageVerbs runs the page's verbs in this process against the fake Relay;
// override answers some of them itself.
type pageVerbs struct {
	mu    sync.Mutex
	calls []string
}

func (v *pageVerbs) count(prefix string) int {
	v.mu.Lock()
	defer v.mu.Unlock()
	n := 0
	for _, c := range v.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func usePageVerbs(t *testing.T, paths runtimePaths, override func(args []string) (string, int, bool)) *pageVerbs {
	t.Helper()
	v := &pageVerbs{}
	previous := setupPageVerb
	t.Cleanup(func() { setupPageVerb = previous })
	setupPageVerb = func(args ...string) (setupVerbEnvelope, int, error) {
		// Every verb names the server setup connected.
		if n := len(args); n < 3 || args[n-2] != "--server" || args[n-1] != activeServerProfile() {
			t.Errorf("verb without the setup's server: %q", args)
		} else {
			args = args[:n-2]
		}
		v.mu.Lock()
		v.calls = append(v.calls, strings.Join(args, " "))
		v.mu.Unlock()
		out, exit, done := "", 0, false
		if override != nil {
			out, exit, done = override(args)
		}
		if !done {
			commands := map[string]func(runtimePaths, []string) int{"map": runMapCommand, "plan": runPlanCommand, "apply": runApplyCommand}
			out = captureStdout(t, func() { exit = commands[args[0]](paths, append(args[1:], "--server", activeServerProfile())) })
		}
		var env setupVerbEnvelope
		if err := json.Unmarshal([]byte(out), &env); err != nil {
			return env, 0, err
		}
		return env, exit, nil
	}
	return v
}

// Rooms: preview, then save by the plan id shown; a forged or replayed id
// saves nothing; undo is a preview of its own, then a click.
func TestSetupPageRoomsPreviewSaveAndUndo(t *testing.T) {
	h := newRoomsHarness(t, newRoomsFake())
	verbs := usePageVerbs(t, h.paths, nil)
	p := newPageStepsTest(t, h.paths, "codex")
	done := p.runHome()

	home := p.waitFor("step 4", accepting("preview")).Home
	var kinds []string
	for _, in := range home.Insights {
		kinds = append(kinds, in.Kind)
		if in.Suggestions != nil {
			t.Errorf("room suggestions reach the page: %+v", in)
		}
	}
	if strings.Join(kinds, ",") != "battery_low,no_room,automation_unavailable" || home.Stats["rooms"] != 4 || home.Insights[1].Action != "rooms" || home.Claude {
		t.Fatalf("home = %+v", home)
	}
	if verbs.count("plan") != 0 || len(h.r.areaWrites()) != 0 {
		t.Fatalf("step 4 planned or wrote before a click: %v %v", verbs.calls, h.r.areaWrites())
	}

	p.click("preview", "rooms")
	pv := p.preview("rooms", "preview")
	var got []string
	for _, c := range pv.Changes {
		got = append(got, c.Name+"→"+c.Area)
	}
	if pv.Kind != "rooms" || pv.PlanID == "" || strings.Join(got, ",") != "Guest Bedroom Lamp→Guest Bedroom,Kitchen Lights→Kitchen,Küchenradio→Kitchen" {
		t.Fatalf("preview = %+v", pv)
	}
	if len(h.r.areaWrites()) != 0 {
		t.Fatalf("the preview wrote %v", h.r.areaWrites())
	}

	// Only a plan id this run shows: anything else saves nothing.
	for _, forged := range []string{"p_forged", "", strings.ToUpper(pv.PlanID)} {
		p.click("save", forged)
		p.waitFor("the page again", accepting("save"))
	}
	if verbs.count("apply") != 0 || len(h.r.areaWrites()) != 0 {
		t.Fatalf("a forged id was applied: %v", verbs.calls)
	}

	p.click("save", pv.PlanID)
	saved := p.preview("rooms", "saved")
	h.assertAreas(map[string]string{"d_guest": "guest", "d_kitchen": "kitchen", "d_radio": "kitchen"})
	if !saved.Undo || saved.Problem != nil || len(saved.Changes) != 3 || saved.PlanID != "" {
		t.Fatalf("saved = %+v", saved)
	}
	p.click("save", pv.PlanID) // the same id again
	p.waitFor("the page again", accepting("save"))
	if verbs.count("apply") != 1 {
		t.Fatalf("a used plan id was applied again: %v", verbs.calls)
	}

	p.click("undo", "rooms")
	undo := p.preview("rooms", "preview")
	if undo.Kind != "undo" || undo.PlanID == "" || undo.PlanID == pv.PlanID || len(undo.Changes) != 3 {
		t.Fatalf("undo preview = %+v", undo)
	}
	h.assertAreas(map[string]string{"d_guest": "guest", "d_kitchen": "kitchen", "d_radio": "kitchen"}) // the undo preview changed nothing
	p.click("save", undo.PlanID)
	if back := p.preview("rooms", "saved"); back.Kind != "undo" || back.Undo {
		t.Fatalf("undone = %+v", back)
	}
	h.assertAreas(roomsOriginal)
	p.click("done", "")
	<-done
}

// "Made for your home": the preview shows the exact draft, and the save
// stores exactly that draft, read back.
func TestSetupPageTemplatePreviewEqualsSave(t *testing.T) {
	f := newFakeHA()
	f.ent("person.someone", "", "home", nil)
	f.full = []map[string]any{{"entity_id": "lock.front", "platform": "zwave_js", "area_id": "hall"}, {"entity_id": "light.hall", "platform": "hue", "area_id": "hall"}}
	h := newWriteHarness(t, f)
	usePageVerbs(t, h.paths, func(args []string) (string, int, bool) {
		if args[0] == "map" { // this fake answers the plan reads, not every map read
			return `{"schema_version":1,"server":"default","status":"ok","result":{"stats":{"rooms":1,"devices":2,"lights":1,"locks":1,"automations":0},"insights":[],
				"automation":{"template":"door_light_after_dark","uses":[{"role":"trigger","entity_id":"lock.front","name":"Front‮Door"},{"role":"light","entity_id":"light.hall","name":"Hall"}]}}}`, 0, true
		}
		return "", 0, false
	})
	p := newPageStepsTest(t, h.paths, "claude", "codex")
	done := p.runHome()
	home := p.waitFor("step 4", accepting("preview")).Home
	if home.Automation == nil || home.Automation.Uses[0].Name != "FrontDoor" || !home.Claude || len(home.Insights) != 0 {
		t.Fatalf("home = %+v", home)
	}
	p.click("preview", "automation")
	pv := p.preview("automation", "preview")
	if pv.Kind != "template" || pv.Template != templateDoorLight || !strings.Contains(pv.Draft, `"to": "unlocked"`) || len(h.writes()) != 0 {
		t.Fatalf("preview = %+v writes %v", pv, h.writes())
	}
	p.click("save", pv.PlanID)
	if saved := p.preview("automation", "saved"); saved.Problem != nil {
		t.Fatalf("saved = %+v", saved)
	}
	if len(h.writes()) != 1 {
		t.Fatalf("writes %v", h.writes())
	}
	var shown, stored map[string]any
	for path, body := range f.configs {
		if err := json.Unmarshal([]byte(fmt.Sprintf("%s", body)), &stored); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	if err := json.Unmarshal([]byte(pv.Draft), &shown); err != nil || !reflect.DeepEqual(shown, stored) {
		t.Fatalf("shown %v\nstored %v (%v)", shown, stored, err)
	}
	p.click("done", "")
	<-done
}

// Refusals show one plain line; a drift shows the new preview and never
// saves the old plan; a partial save offers undo; a typed plan is never
// offered.
func TestSetupPageRefusalsAndDrift(t *testing.T) {
	plan := func(id, tier string) string {
		return `{"schema_version":1,"server":"default","status":"ok","tier":"` + tier + `","plan":{"id":"` + id + `","confirm":"natural"},
			"result":{"changes":[{"device_id":"d1","name":"Lamp","area_id":"kitchen","area":"Kitchen"}]}}`
	}
	refusal := func(code string) string {
		return `{"schema_version":1,"server":"default","status":"error","error":{"code":"` + code + `","message":"x"}}`
	}
	answers := map[string][]struct {
		out  string
		exit int
	}{
		"map --insights": {{`{"schema_version":1,"server":"default","status":"ok","result":{"stats":{"rooms":1},"insights":[
			{"kind":"battery_low","name":"Door battery","value":"12%","count":1,"action":"battery_reminder"},
			{"kind":"no_room","count":3,"action":"rooms"}],"automation":{"template":"lights_off_when_away","uses":[]}}}`, 0}},
		"plan template battery_reminder":     {{refusal("TEMPLATE_NO_MATCH"), 4}},
		"plan template lights_off_when_away": {{plan("p_typed", "confirm"), 3}},
		"plan rooms":                         {{plan("p_1", "preview"), 3}, {plan("p_2", "preview"), 3}, {plan("p_3", "preview"), 3}},
		"apply --plan p_2":                   {{`{"schema_version":1,"server":"default","status":"drift"}`, 2}},
		"apply --plan p_3":                   {{`{"schema_version":1,"server":"default","status":"unverified","undo":{"target_id":"rooms-0123456789ab"},"error":{"code":"ROOMS_PARTIAL","message":"x"}}`, 6}},
	}
	var mu sync.Mutex
	verbs := usePageVerbs(t, runtimePaths{}, func(args []string) (string, int, bool) {
		mu.Lock()
		defer mu.Unlock()
		key := strings.Join(args, " ")
		queue := answers[key]
		if len(queue) == 0 {
			t.Errorf("unexpected verb %q", key)
			return refusal("NOPE"), 1, true
		}
		answers[key] = queue[1:]
		return queue[0].out, queue[0].exit, true
	})
	p := newPageStepsTest(t, runtimePaths{})
	done := p.runHome()
	p.waitFor("step 4", accepting("preview"))

	p.click("preview", "reminder")
	if pv := p.preview("reminder", "problem"); pv.Problem.Key != "setup.page.home.problem.not_offered" || pv.PlanID != "" {
		t.Fatalf("reminder = %+v", pv)
	}
	p.click("preview", "automation")
	typed := p.preview("automation", "problem")
	if typed.Problem.Key != "setup.page.home.problem.generic" || typed.PlanID != "" {
		t.Fatalf("a typed plan was offered: %+v", typed)
	}
	p.click("save", "p_typed")
	p.click("preview", "reminder") // a refusal that can't change: never planned again
	p.waitFor("the page again", accepting("preview"))
	if n := verbs.count("plan template battery_reminder"); n != 1 {
		t.Fatalf("planned the refused reminder %d times", n)
	}
	if msg, retry := pageRefusal("NO_ROOM_SUGGESTIONS"); msg.Key != "setup.page.home.problem.no_rooms" || retry {
		t.Fatalf("no rooms = %+v %v", msg, retry)
	}
	p.click("preview", "rooms")
	first := p.preview("rooms", "preview")
	p.click("preview", "rooms") // a new preview replaces p_1
	second := p.preview("rooms", "preview")
	if first.PlanID != "p_1" || second.PlanID != "p_2" {
		t.Fatalf("previews %s %s", first.PlanID, second.PlanID)
	}
	p.click("save", "p_1") // no longer on screen
	p.waitFor("the page again", accepting("save"))
	p.click("save", "p_2") // drift: the new preview, p_3
	drifted := p.preview("rooms", "preview")
	if drifted.PlanID != "p_3" || drifted.Notice == nil || drifted.Notice.Key != "setup.page.home.drift" {
		t.Fatalf("after drift = %+v", drifted)
	}
	p.click("save", "p_3")
	partial := p.preview("rooms", "problem")
	if partial.Problem.Key != "setup.page.home.problem.rooms_partial" || !partial.Undo {
		t.Fatalf("partial = %+v", partial)
	}
	if verbs.count("apply --plan p_1") != 0 || verbs.count("apply --plan p_typed") != 0 || verbs.count("apply") != 2 {
		t.Fatalf("applied %v", verbs.calls)
	}
	p.click("done", "")
	<-done
}

// No insights: no rows, just the stats; an unreadable home says so once.
func TestSetupPageHomeWithoutInsights(t *testing.T) {
	for _, test := range []struct {
		out    string
		failed bool
	}{
		{`{"schema_version":1,"server":"default","status":"ok","result":{"stats":{"rooms":0,"devices":3,"lights":0,"locks":0,"automations":0},"insights":[]}}`, false},
		{`{"schema_version":1,"server":"default","status":"error","error":{"code":"RELAY_ERROR","message":"down"}}`, true},
		{`{"schema_version":1,"server":"cabin","status":"ok","result":{"stats":{"rooms":2},"insights":[]}}`, true}, // another home answered
	} {
		usePageVerbs(t, runtimePaths{}, func([]string) (string, int, bool) { return test.out, 0, true })
		p := newPageStepsTest(t, runtimePaths{})
		done := p.runHome()
		home := p.waitFor("step 4", accepting("done")).Home
		if home.Failed != test.failed || len(home.Insights) != 0 || home.Automation != nil || (home.Stats == nil) != test.failed {
			t.Fatalf("home = %+v", home)
		}
		p.click("preview", "rooms") // nothing behind it: no plan
		p.waitFor("the page again", accepting("done"))
		if pv := p.state().Home.previewIn("rooms"); pv != nil {
			t.Fatalf("previewed %+v", pv)
		}
		p.click("done", "")
		<-done
	}
}

// "Open Claude Code" opens a terminal, or says how where none opens; the
// page stays on step 4.
func TestSetupPageOpensClaudeCode(t *testing.T) {
	usePageVerbs(t, runtimePaths{}, func([]string) (string, int, bool) {
		return `{"schema_version":1,"server":"default","status":"ok","result":{"stats":{}}}`, 0, true
	})
	previous := setupPageOpenClaude
	t.Cleanup(func() { setupPageOpenClaude = previous })
	for _, opens := range []bool{true, false} {
		setupPageOpenClaude = func() error {
			if opens {
				return nil
			}
			return errors.New("no terminal")
		}
		p := newPageStepsTest(t, runtimePaths{}, "claude")
		done := p.runHome()
		p.waitFor("step 4", accepting("claude"))
		p.click("claude", "")
		want := map[bool]string{true: "terminal", false: "manual"}[opens]
		if got := p.waitFor("opened", func(s setupPageState) bool { return s.Home.Opened != "" }).Home.Opened; got != want {
			t.Fatalf("opened %q, want %q", got, want)
		}
		p.click("done", "")
		<-done
	}
	// Without Claude Code there is no such button.
	p := newPageStepsTest(t, runtimePaths{}, "codex")
	done := p.runHome()
	if code := p.post(`{"action":"claude"}`, nil); code != http.StatusConflict {
		t.Fatalf("claude without Claude Code = %d", code)
	}
	p.waitFor("step 4", accepting("done"))
	p.click("done", "")
	<-done
}

// Step 3's phone row: the signed-in user's phone first; one notification to
// that phone alone; the tap there links it.
func TestSetupPageLinksThePhoneWithOneTap(t *testing.T) {
	h, phones, _ := familyLink(t)
	phones.tapBy = tapFrom("mobile_app_annas_pixel", "anna-id", true)
	previous := setupSignedInUser
	setupSignedInUser = "anna-id"
	t.Cleanup(func() { setupSignedInUser = previous })
	p := newPageStepsTest(t, h.paths, "codex")
	p.flow.pairedThisRun = true
	done := p.runControl()
	ctl := p.waitFor("step 3", accepting("phone")).Control
	if ctl.Phone != "pick" || !ctl.Proposed || ctl.Phones[0].ID != "dev-anna" || len(ctl.Phones) != 3 || ctl.Rule != "" || ctl.Link != "" {
		t.Fatalf("control = %+v", ctl)
	}
	if len(phones.sent) != 0 {
		t.Fatalf("step 3 notified before a click: %v", phones.services)
	}
	// A phone the page didn't list (not an administrator's): nothing sent.
	p.click("phone", "dev-kid")
	p.waitFor("the page again", accepting("phone"))
	if len(phones.sent) != 0 || linked(t, h) != nil {
		t.Fatalf("an unlisted phone was asked: %v", phones.services)
	}
	p.click("phone", "dev-anna")
	ctl = p.waitFor("linked", func(s setupPageState) bool { return s.Control.Phone == "linked" }).Control
	if ctl.PhoneName != "Anna’s Pixel" || ctl.PhoneProblem != nil {
		t.Fatalf("control = %+v", ctl)
	}
	if strings.Join(phones.services, ",") != "mobile_app_annas_pixel,mobile_app_annas_pixel" || phones.sent[0]["title"] != "Link this phone to HA NOVA?" {
		t.Fatalf("notified %v %v", phones.services, phones.sent)
	}
	if l := linked(t, h); l == nil || l.DeviceID != "dev-anna" || l.UserID != "anna-id" {
		t.Fatalf("link %+v", l)
	}
	p.waitFor("finish", accepting("finish"))
	p.click("finish", "")
	if code := <-done; code != 0 {
		t.Fatalf("exit %d", code)
	}
}

// No tap in time: one line, nothing linked, and the row can try again.
func TestSetupPagePhoneLinkTimesOut(t *testing.T) {
	h, phones, _ := familyLink(t)
	p := newPageStepsTest(t, h.paths, "codex")
	p.flow.pairedThisRun = true
	done := p.runControl()
	ctl := p.waitFor("step 3", accepting("phone")).Control
	if ctl.Proposed || ctl.Phone != "pick" {
		t.Fatalf("control = %+v", ctl)
	}
	p.click("phone", "dev-anna")
	ctl = p.waitFor("the timeout", func(s setupPageState) bool { return s.Control.PhoneProblem != nil }).Control
	if ctl.Phone != "pick" || ctl.PhoneProblem.Key != "setup.page.control.phone_timeout" || ctl.PhoneProblem.Args["phone"] != "Anna’s Pixel" || linked(t, h) != nil {
		t.Fatalf("control = %+v", ctl)
	}
	if len(phones.sent) != 2 || phones.services[0] != "mobile_app_annas_pixel" { // the ask, then "Not linked"
		t.Fatalf("notified %v", phones.services)
	}
	p.waitFor("finish", accepting("finish"))
	p.click("finish", "")
	<-done
}

// The Claude Code rule: preselected on the page, written only by "Finish
// setup" with the switch on, and recorded for uninstall as in #743.
func TestSetupPageClaudeRuleOnlyOnFinish(t *testing.T) {
	for _, on := range []bool{true, false} {
		paths, settings := claudeRuleTestPaths(t)
		p := newPageStepsTest(t, paths, "claude")
		done := p.runControl()
		ctl := p.waitFor("step 3", accepting("finish")).Control
		if ctl.Rule != "offer" || ctl.Phone != "unavailable" {
			t.Fatalf("control = %+v", ctl)
		}
		if _, err := os.Stat(settings); !os.IsNotExist(err) {
			t.Fatalf("step 3 wrote the settings before Finish: %v", err)
		}
		p.click("finish", "rule,rm") // never sent by the page: ignored
		p.waitFor("finish again", accepting("finish"))
		if _, err := os.Stat(settings); !os.IsNotExist(err) {
			t.Fatalf("a malformed finish wrote: %v", err)
		}
		value := ""
		if on {
			value = "rule"
		}
		p.click("finish", value)
		<-done
		_, err := os.Stat(settings)
		if !on {
			if !os.IsNotExist(err) || len(p.flow.pageNotes) != 0 {
				t.Fatalf("switch off: settings %v notes %v", err, p.flow.pageNotes)
			}
			continue
		}
		state := loadStateOrDefault(paths)
		if !strings.Contains(readClaudeSettings(t, settings), claudeApplyRule) || state.ClaudeRulesFile != settings || !slices.Contains(state.ClaudeRules, claudeApplyRule) {
			t.Fatalf("rule not written or not recorded: %s %+v", readClaudeSettings(t, settings), state)
		}
		if len(p.flow.pageNotes) != 1 || p.flow.pageNotes[0].Key != "setup.page.home.note_rule" {
			t.Fatalf("notes %+v", p.flow.pageNotes)
		}
	}
	// Already there: shown as on, nothing to write.
	paths, settings := claudeRuleTestPaths(t)
	writeClaudeSettings(t, settings, `{"permissions":{"allow":["`+strings.Join(claudeRules(paths), `","`)+`"]}}`)
	p := newPageStepsTest(t, paths, "claude")
	done := p.runControl()
	if ctl := p.waitFor("step 3", accepting("finish")).Control; ctl.Rule != "on" {
		t.Fatalf("control = %+v", ctl)
	}
	p.click("finish", "rule")
	<-done
}

// The Link row shows only while Link remote access is on, and Link is added
// only with its switch on.
func TestSetupPageLinkRowOnlyWhenRemoteIsActive(t *testing.T) {
	for _, test := range []struct {
		active      bool
		finish      string
		adds        int
		uninstalled bool // the install generation rotated while step 3 waited
	}{{false, "link", 0, false}, {true, "", 0, false}, {true, "link", 1, false}, {true, "link", 0, true}} {
		resetServerProfileSelection(t)
		paths := writeTestConfigFile(t, `{"schema_version":1}`)
		if err := saveConfig(paths, completedLocalCloudTestConfig()); err != nil {
			t.Fatal(err)
		}
		cfg, err := loadConfig(paths)
		if err != nil {
			t.Fatal(err)
		}
		coordinator := successfulCloudCoordinatorForTest()
		installCloudSetupTestSeams(t, coordinator, true, true)
		previous := setupPageLinkActive
		t.Cleanup(func() { setupPageLinkActive = previous })
		setupPageLinkActive = func(runtimeConfig) bool { return test.active }
		p := newPageStepsTest(t, paths, "codex")
		p.flow.cfg = cfg
		if test.uninstalled {
			p.flow.pageGeneration = []byte("the generation setup completed with")
		}
		done := p.runControl()
		ctl := p.waitFor("step 3", accepting("finish")).Control
		if want := map[bool]string{true: "offer", false: ""}[test.active]; ctl.Link != want {
			t.Fatalf("active=%v: link row %q", test.active, ctl.Link)
		}
		p.click("finish", test.finish)
		if code := <-done; (code != 0) != test.uninstalled || coordinator.addCalls != test.adds {
			t.Fatalf("%+v: exit %d adds %d", test, code, coordinator.addCalls)
		}
		if test.uninstalled && (len(p.flow.pageNotes) != 1 || p.flow.pageNotes[0].Key != "setup.page.home.note_link_failed") {
			t.Fatalf("a superseded setup added Link or said nothing: %+v", p.flow.pageNotes)
		}
		if test.adds == 1 && (!p.flow.cfg.Cloud.ready() || len(p.flow.pageNotes) != 1 || p.flow.pageNotes[0].Key != "setup.page.home.note_link") {
			t.Fatalf("after Link: cloud %+v notes %+v", p.flow.cfg.Cloud, p.flow.pageNotes)
		}
	}
}

// The new actions follow the A6 model: same-origin JSON with the session
// cookie, and only while the page waits for them.
func TestSetupPageStepActionsAreGuarded(t *testing.T) {
	page := startSetupPageTest(t)
	page.launch()
	for _, action := range []string{"phone", "finish", "preview", "save", "undo", "claude", "done"} {
		body := `{"action":"` + action + `","value":"x"}`
		if code := page.post(body, nil); code != http.StatusConflict {
			t.Errorf("%s while nothing waits = %d", action, code)
		}
		waited := make(chan struct{})
		go func() { _, _ = page.ui.wait(action); close(waited) }()
		page.waitFor(action, accepting(action))
		for name, header := range map[string]map[string]string{
			"foreign origin": {"Origin": "http://evil.example"},
			"no origin":      {"Origin": ""},
			"cross-site":     {"Sec-Fetch-Site": "cross-site"},
			"no cookie":      {"Cookie": ""},
			"form":           {"Content-Type": "application/x-www-form-urlencoded"},
		} {
			if code := page.post(body, header); code == http.StatusNoContent {
				t.Errorf("%s %s was taken", action, name)
			}
		}
		if code := page.post(`{"action":"`+action+`","value":"`+strings.Repeat("x", 300)+`"}`, nil); code != http.StatusBadRequest {
			t.Errorf("%s with an oversized value = %d", action, code)
		}
		page.click(action, "x")
		<-waited // no wait outlives the test (it reads setupPageIdle)
		if code := page.post(body, nil); code != http.StatusConflict {
			t.Errorf("%s twice = %d", action, code)
		}
	}
}

// No Home Assistant found: step 1 offers the demo house (`ha-nova try`);
// a failure stays on step 1 with one line.
func TestSetupPageOffersTheDemoHouse(t *testing.T) {
	for _, works := range []bool{false, true} {
		withClientRuntimeAvailability(t, map[string]bool{"codex": true})
		setupGoldenHome(t)
		stubSetupPageHome(t, "http://192.0.2.10:8123", false)
		discoverReachableHAHostsForSetup = func(runtimeConfig) ([]setupDiscoveryCandidate, string) { return nil, "" }
		tries := 0
		previous := setupPageTry
		setupPageTry = func(runtimePaths) int {
			tries++
			if works {
				return 0
			}
			return 1
		}
		previousIdle := setupPageIdle
		setupPageIdle = time.Second
		page, stdout, done := startSetupPageFlow(t, "")
		page.waitFor("the address question", accepting("demo"))
		page.click("demo", "")
		if works {
			if code := setupPageExit(t, done, stdout); code != 0 || page.ui.snapshot().Screen != "demo" || tries != 1 {
				t.Fatalf("exit %d screen %s tries %d", code, page.ui.snapshot().Screen, tries)
			}
		} else {
			back := page.waitFor("the demo problem", func(s setupPageState) bool { return s.Problem != nil })
			if back.Problem.Key != "setup.page.problem.demo" || !back.AskHA || tries != 1 {
				t.Fatalf("back = %+v", back)
			}
			setupPageExit(t, done, stdout)
		}
		setupPageTry, setupPageIdle = previous, previousIdle
	}
}

// `setup --demo` is `ha-nova try`; with other options it refuses.
func TestSetupDemoAliasRunsTry(t *testing.T) {
	fakeDemo(t, "")
	paths := tryTestPaths(t, "")
	if exit := runSetup(paths, []string{"--demo", "--no-browser"}); exit != 1 {
		t.Fatalf("--demo with another option: exit %d", exit)
	}
	t.Setenv(serverSelectionEnvVar, demoProfileName) // the demo guard lets the alias through
	exit := 0
	out := captureStdout(t, func() { exit = dispatch(paths, "ha-nova", []string{"setup", "--demo"}) })
	if _, err := os.Stat(demoProfilePath(paths)); exit != 0 || err != nil || !strings.Contains(out, "Demo house connected") {
		t.Fatalf("exit %d demo.json %v out %s", exit, err, out)
	}
}

// A save whose outcome is unclear is never offered again (it could save
// twice); a read-back mismatch never shows a ✓; a failed undo keeps the
// way back.
func TestSetupPageUnclearSavesAndUndoThatFails(t *testing.T) {
	env := func(body string) string { return `{"schema_version":1,"server":"default",` + body + `}` }
	changes := `"result":{"changes":[{"device_id":"d1","name":"Lamp","area_id":"kitchen","area":"Kitchen"}]}`
	answers := map[string][]struct {
		out  string
		exit int
	}{
		"map --insights": {{env(`"status":"ok","result":{"stats":{"rooms":1},"insights":[{"kind":"no_room","count":1,"action":"rooms"}],
			"automation":{"template":"lights_off_when_away","uses":[{"role":"light","entity_id":"light.a","name":"A"}]}}`), 0}},
		"plan template lights_off_when_away": {{env(`"status":"ok","tier":"preview","plan":{"id":"p_a","confirm":"natural"},
			"config":{"op":"write","domain":"automation","config_id":"1","path":"x","body":{"alias":"Lights off"}},"result":{"template":"lights_off_when_away","uses":[]}`), 3}},
		"apply --plan p_a": {{env(`"status":"unverified","error":{"code":"CONFIG_ERROR","message":"x"}`), 6}},
		"plan rooms":       {{env(`"status":"ok","tier":"preview","plan":{"id":"p_r","confirm":"natural"},` + changes), 3}},
		"apply --plan p_r": {{env(`"status":"unverified","undo":{"target_id":"rooms-0123456789ab"},` + changes), 6}},
		"plan revert --target rooms-0123456789ab": {{env(`"status":"error","error":{"code":"RELAY_ERROR","message":"x"}`), 1},
			{env(`"status":"ok","tier":"preview","plan":{"id":"p_u","confirm":"natural"},` + changes), 3}},
		"apply --plan p_u": {{env(`"status":"unverified","error":{"code":"ROOMS_PARTIAL","message":"x"}`), 6}},
	}
	var mu sync.Mutex
	usePageVerbs(t, runtimePaths{}, func(args []string) (string, int, bool) {
		mu.Lock()
		defer mu.Unlock()
		key := strings.Join(args, " ")
		queue := answers[key]
		if len(queue) == 0 {
			t.Errorf("unexpected verb %q", key)
			return env(`"status":"error","error":{"code":"NOPE","message":"x"}`), 1, true
		}
		answers[key] = queue[1:]
		return queue[0].out, queue[0].exit, true
	})
	p := newPageStepsTest(t, runtimePaths{})
	done := p.runHome()
	p.waitFor("step 4", accepting("preview"))

	p.click("preview", "automation")
	p.click("save", p.preview("automation", "preview").PlanID)
	if pv := p.preview("automation", "problem"); pv.Problem.Key != "setup.page.home.problem.unknown" || pv.Retry {
		t.Fatalf("an unclear save can be tried again: %+v", pv)
	}
	p.click("preview", "rooms")
	p.click("save", p.preview("rooms", "preview").PlanID)
	if pv := p.preview("rooms", "unverified"); pv.Problem.Key != "setup.page.home.problem.unverified" || !pv.Undo {
		t.Fatalf("unverified = %+v", pv)
	}
	p.click("undo", "rooms")
	if pv := p.preview("rooms", "problem"); !pv.Undo || pv.Retry {
		t.Fatalf("a failed undo preview lost the way back: %+v", pv)
	}
	p.click("undo", "rooms")
	undo := p.preview("rooms", "preview")
	if undo.Kind != "undo" || undo.PlanID != "p_u" {
		t.Fatalf("undo = %+v", undo)
	}
	p.click("save", "p_u")
	if pv := p.preview("rooms", "problem"); pv.Problem.Key != "setup.page.home.problem.rooms_partial" || !pv.Undo {
		t.Fatalf("a partial undo lost the way back: %+v", pv)
	}
	p.click("done", "")
	<-done
}
