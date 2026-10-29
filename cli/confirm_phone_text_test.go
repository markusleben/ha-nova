package main

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// Every notification: a short question, one line naming the computer, the
// hint last; only a pending action is time-sensitive.
func TestPhoneAskTexts(t *testing.T) {
	host := "“" + phoneHost("en") + "”"
	requested := "Requested by " + host + "."
	cases := []struct {
		got                 phoneAsk
		title, body, button string
		urgent              bool
	}{
		{phoneLinkAsk("en"), "Get HA NOVA confirmations?", requested, "Allow", false},
		{phoneChangeAsk("en", ""), "Stop HA NOVA confirmations?", "After this, HA NOVA may ask in chat, where your AI assistant can answer. " + requested, "Stop", false},
		{phoneChangeAsk("en", "Anna’s Pixel"), "Move HA NOVA confirmations?", "New phone: “Anna's Pixel”. " + requested, "Move", false},
		{phonePlanAsk("en", actionGoal{verb: "delete", frame: "automation", names: []string{"“Night”"}}, true), "Delete the automation “Night”?", "Can't be undone. " + requested, "Delete", true},
		{phonePlanAsk("en", actionGoal{verb: "turn_on", names: []string{"“Guest mode”"}, why: "This also unlocks “Entrance Door” through the automation “Guest”"}, false),
			"Turn on “Guest mode”?", "This also unlocks “Entrance Door” through the automation “Guest”. " + requested, "Turn on", true},
		{phonePlanAsk("en", actionGoal{verb: "set_position", names: []string{"“Gate”"}, why: "This closes a gate"}, false), "Set the position of “Gate”?", "This closes a gate. " + requested, "Confirm", true},
		{phonePlanAsk("en", actionGoal{label: "Cancel", names: []string{"“Timer”"}}, false), "Cancel “Timer”?", requested, "Confirm", true},
	}
	for _, c := range cases {
		if c.got != (phoneAsk{"en", c.title, c.body, c.button, c.urgent, !c.urgent}) { // link, move, stop need the unlocked phone
			t.Errorf("ask %+v, want %q %q %q %v", c.got, c.title, c.body, c.button, c.urgent)
		}
	}
	// A long list keeps the first name and counts every other one; a cut name stays quoted.
	name := "“" + strings.Repeat("Front Door Lock", 3) + "”"
	title := phonePlanAsk("en", actionGoal{verb: "unlock", names: slices.Repeat([]string{name}, 7)}, false).title
	if n := len([]rune(title)); n > phoneTitleLimit || title != "Unlock "+name+" and 6 more?" {
		t.Errorf("long title %d %q", n, title)
	}
	long := "“" + strings.Repeat("x", 58) + "”"
	title = phonePlanAsk("en", actionGoal{verb: "set_position", names: []string{long, long, long}}, false).title
	if n := len([]rune(title)); n > phoneTitleLimit || !strings.HasSuffix(title, "…” and 2 more?") {
		t.Errorf("long names %d %q", n, title)
	}
	title = phonePlanAsk("en", actionGoal{verb: "unlock", names: []string{strings.Repeat("x", 200)}}, false).title
	if n := len([]rune(title)); n > phoneTitleLimit || !strings.HasSuffix(title, "…?") {
		t.Errorf("long name %d %q", n, title)
	}
}

// Android's pop-up cuts a title after about 30 characters and shows one
// line of text: the fixed titles fit, and the hint (first there) is true
// whether the buttons show (pop-up, opened) or not (collapsed).
func TestPhoneTextsFitAndroid(t *testing.T) {
	for _, ask := range []phoneAsk{phoneLinkAsk("en"), phoneChangeAsk("en", ""), phoneChangeAsk("en", "A very long phone name, Pixel 9 Pro XL")} {
		if n := len([]rune(ask.title)); n > 30 {
			t.Errorf("title %q: %d characters", ask.title, n)
		}
	}
	android := func(ask phoneAsk) string {
		return phonePayload(ask, phoneAndroid, "t", "OK", "NO", time.Now().Add(time.Minute))["message"].(string)
	}
	// Only who asked: the button, true with or without it in view.
	for button, want := range map[string]string{
		"Allow":           "Tap “Allow”. No button? Pull down.",
		"Turn off":        "Tap “Turn off”. No button? Pull down.",
		"Set temperature": "No buttons? Pull down to answer.", // a long verb would cut "Pull down"
	} {
		if got := phoneHint("en", phoneAndroid, button, false); got != want || len([]rune(got)) > 38 {
			t.Errorf("android hint %q: %q", button, got)
		}
	}
	if got := android(phoneLinkAsk("en")); got != "Tap “Allow”. No button? Pull down. "+phoneRequested("en") {
		t.Errorf("link %q", got)
	}
	// More to read (a hidden effect, no undo, the new phone): read it before a tap from the pop-up.
	for _, ask := range []phoneAsk{phoneChangeAsk("en", "Anna’s Pixel"), phoneChangeAsk("en", ""), phonePlanAsk("en", actionGoal{verb: "delete", frame: "automation", names: []string{"“Night”"}}, true),
		phonePlanAsk("en", actionGoal{verb: "turn_on", names: []string{"“Guest mode”"}, why: "This also unlocks “Entrance Door”"}, false)} {
		if got := android(ask); !strings.HasPrefix(got, "Read it all. No button? Pull down. ") {
			t.Errorf("%q: %q", ask.title, got)
		}
	}
	if got := phoneHint("en", phoneIOS, "Allow", true); got != "Press and hold to answer." {
		t.Errorf("ios hint %q", got)
	}
	// The move's new phone is in the text, so the old phone sees where it goes.
	if got := android(phoneChangeAsk("en", "Anna’s Pixel")); !strings.Contains(got, "New phone: “Anna's Pixel”.") {
		t.Errorf("move text %q", got)
	}
}

// A plan whose risk is not its target: the phone says what else happens.
func TestPhonePlanNamesTheHiddenEffect(t *testing.T) {
	f := newFakeHA()
	named := func(id, name string) { f.states[id]["attributes"].(map[string]any)["friendly_name"] = name }
	named("lock.front", "Entrance Door")
	f.ent("input_boolean.guest", "input_boolean", "off", map[string]any{"friendly_name": "Guest mode"})
	f.related["entity:input_boolean.guest"] = []string{"automation.guest"}
	f.automation("guest", "g1", map[string]any{
		"triggers": []any{map[string]any{"trigger": "state", "entity_id": "input_boolean.guest"}},
		"actions":  []any{step("lock.unlock", "lock.front")},
	})
	named("automation.guest", "Guest mode unlocks")
	h := newWriteHarness(t, f)
	p := usePhones(t, "mobile_app_iphone")
	plan := h.plan("input_boolean.turn_on", "input_boolean.guest").Plan
	p.answer = func(_, no string) []phoneEvent { return []phoneEvent{tap(no, "owner-id", "REMOTE")} }
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID); plan.Channel != channelPhone || exit != 4 || env.Error.Code != "CONFIRM_DECLINED" || len(p.sent) == 0 {
		t.Fatalf("plan %+v exit %d %+v", plan, exit, env.Error)
	}
	if p.sent[0]["title"] != "Turn on “Guest mode”?" ||
		p.sent[0]["message"] != "This also unlocks “Entrance Door” through the automation “Guest mode unlocks”. Requested by “"+phoneHost("en")+"”. "+phoneHint("en", "", "Turn on", true) {
		t.Fatalf("notification %q / %q", p.sent[0]["title"], p.sent[0]["message"])
	}
}

// Several targets: the title may cut them, so the reason always shows.
func TestPhonePlanReasonWithSeveralTargets(t *testing.T) {
	f := newFakeHA()
	f.ent("lock.back", "zwave_js", "locked", map[string]any{"friendly_name": "Back Door"})
	h := newWriteHarness(t, f)
	p := usePhones(t, "mobile_app_iphone")
	plan := h.plan("lock.unlock", "lock.front", "lock.back").Plan
	p.answer = func(_, no string) []phoneEvent { return []phoneEvent{tap(no, "owner-id", "REMOTE")} }
	if exit, _ := h.run(runApplyCommand, "--plan", plan.ID); exit != 4 || len(p.sent) == 0 || !strings.HasPrefix(p.sent[0]["message"].(string), "This unlocks") {
		t.Fatalf("exit %d sent %v", exit, p.sent)
	}
	confirm := p.sent[0]["data"].(map[string]any)["actions"].([]any)[0].(map[string]any)
	if _, auth := confirm["authenticationRequired"]; auth { // a plan's Confirm: the watch can answer
		t.Fatalf("plan confirm needs an unlock: %v", confirm)
	}
}

// One unregistered lock: the title says it; "can't see what it does" stays off the phone.
func TestPhonePlanSkipsTheTargetsOwnReason(t *testing.T) {
	f := newFakeHA()
	f.ent("lock.yaml", "", "locked", map[string]any{"friendly_name": "Front Door"}) // no registry row
	h := newWriteHarness(t, f)
	p := usePhones(t, "mobile_app_iphone")
	plan := h.plan("lock.unlock", "lock.yaml").Plan
	p.answer = func(_, no string) []phoneEvent { return []phoneEvent{tap(no, "owner-id", "REMOTE")} }
	if exit, _ := h.run(runApplyCommand, "--plan", plan.ID); exit != 4 || len(p.sent) == 0 ||
		p.sent[0]["title"] != "Unlock “Front Door”?" || p.sent[0]["message"] != "Requested by “"+phoneHost("en")+"”. "+phoneHint("en", "", "Unlock", false) {
		t.Fatalf("exit %d sent %v", exit, p.sent)
	}
}

// Only a verified result is "Done"; a claimed plan may have run.
func TestPhoneApplyResult(t *testing.T) {
	claimed := verbEnvelope{Error: &verbError{Code: "PLAN_CLAIMED"}}
	cases := []struct {
		env   verbEnvelope
		exit  int
		title string
	}{
		{verbEnvelope{Status: "verified"}, verbExitOK, "Done: X"},
		{verbEnvelope{Status: "accepted"}, verbExitOK, "Sent: X"},
		{verbEnvelope{Status: "failed"}, verbExitUnverified, "Didn't work: X"},
		{verbEnvelope{Status: "unverified"}, verbExitUnverified, "Check: X"},
		{verbEnvelope{}, verbExitUnknown, "Unclear: X"},
		{claimed, verbExitRefused, "Unclear: X"},
		{verbEnvelope{}, verbExitRefused, "Not done: X"},
	}
	for _, c := range cases {
		if prefix, _ := phoneApplyResult("en", c.env, c.exit); prefix+": X" != c.title {
			t.Errorf("%+v %d: %q, want %q", c.env, c.exit, prefix, c.title)
		}
	}
}

// A stop after the tap points where the reason shows: the chat in a chat
// app (no computer there), else the computer.
func TestPhoneStopSaysWhereWhyShows(t *testing.T) {
	t.Cleanup(func() { chatCall = nil })
	for _, c := range []struct {
		chat *chatPolicy
		want string
	}{{nil, "The computer says why."}, {&chatPolicy{codes: true}, "The chat says why."}} {
		chatCall = c.chat
		_, tapped := phoneNothing("en", phoneConfirmed)
		_, refused := phoneApplyResult("en", verbEnvelope{}, verbExitRefused)
		for _, line := range []string{tapped, refused} {
			if !strings.HasSuffix(line, "Nothing changed. "+c.want) {
				t.Errorf("chat %v: %q, want it to end %q", c.chat != nil, line, c.want)
			}
		}
	}
}

// A move or stop that gets no answer on the old phone still ends there.
func TestPhoneChangeTimeoutTellsTheOldPhone(t *testing.T) {
	h, p, _ := familyLink(t)
	seedLink(t, h, &annaLink)
	for _, run := range []func() (int, string){
		func() (int, string) { return runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"}) },
		func() (int, string) { return runUnlink(t, h) },
	} {
		p.sent, p.services = nil, nil
		if exit, out := run(); exit != 1 || len(p.sent) != 2 || p.sent[1]["title"] != "Not confirmed" || p.services[1] != "mobile_app_annas_pixel" {
			t.Fatalf("exit %d out %q sent %v", exit, out, p.sent)
		}
	}
}

// A direct press on a wiping button: "Press “Maintenance”?" doesn't say it wipes, so the body does.
func TestPhonePlanKeepsADirectRiskTheTitleDoesNotSay(t *testing.T) {
	f := newFakeHA()
	f.ent("button.plug_wipe", "zha", "unknown", map[string]any{"friendly_name": "Maintenance"})
	h := newWriteHarness(t, f)
	p := usePhones(t, "mobile_app_iphone")
	plan := h.plan("button.press", "button.plug_wipe").Plan
	p.answer = func(_, no string) []phoneEvent { return []phoneEvent{tap(no, "owner-id", "REMOTE")} }
	if exit, _ := h.run(runApplyCommand, "--plan", plan.ID); plan.Channel != channelPhone || exit != 4 || len(p.sent) == 0 ||
		!strings.Contains(p.sent[0]["message"].(string), "wipe") {
		t.Fatalf("plan %+v exit %d sent %v", plan, exit, p.sent)
	}
}

// The result keeps the count; a name can't fake "and N more"; dismissing verbs are Confirm.
func TestPhoneResultTitleAndButtons(t *testing.T) {
	name := "“" + strings.Repeat("Front Door Lock", 3) + "”"
	if got := phoneResultTitle("en", "Unclear", actionGoal{verb: "unlock", names: slices.Repeat([]string{name}, 7)}); got != "Unclear: Unlock "+name+" and 6 more" {
		t.Errorf("result title %q", got)
	}
	long := "“" + strings.Repeat("x", 58) + "”"
	if got := (actionGoal{verb: "set_position", names: []string{long, "“B and -1 more”"}}).title("en", ""); !strings.HasSuffix(got, "” and 1 more?") {
		t.Errorf("a name steered the count: %q", got)
	}
	for _, verb := range []actionGoal{{verb: "close"}, {verb: "stop"}, {label: "Cancel"}} {
		if verb.button("en") != "Confirm" {
			t.Errorf("button %+v", verb)
		}
	}
}

// A raw step's list of targets is counted in a long title like any names;
// quoted values that are no list stay one name, cut.
func TestRawTitleCountsItsList(t *testing.T) {
	listed := []string{"“sensor.kitchen_energy_total”", "“sensor.living_room_energy_total”", "“sensor.bedroom_energy_total”", "“sensor.office_energy_total”"}
	goal := actionGoal{label: "Delete", names: rawNames("the long-term statistics of "+sayList("en", listed), listed)}
	if got := goal.title("en", ""); got != "Delete the long-term statistics of “sensor.kitchen_energy_total” and 3 more?" {
		t.Fatalf("title = %q", got)
	}
	if got := goal.title("en", "Done"); !strings.HasPrefix(got, "Done: Delete the long-term statistics of “sensor.") || !strings.HasSuffix(got, "” and 3 more") {
		t.Fatalf("result title = %q", got)
	}
	short := []string{"“A”", "“B”"}
	if got := (actionGoal{label: "Delete", names: rawNames("the statistics of "+sayList("en", short), short)}).title("en", ""); got != "Delete the statistics of “A” and “B”?" {
		t.Fatalf("short title = %q", got)
	}
	fields := "the entity “Desk”: name “" + strings.Repeat("Long name ", 8) + "”, area “Office”"
	if got := rawNames(fields, []string{"“Desk”"}); len(got) != 1 || got[0] != fields {
		t.Fatalf("names = %q", got)
	}
}
