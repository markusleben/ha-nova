package main

import (
	"strings"
	"testing"
)

// Every notification: a short question, one line naming the computer, the
// hint last; only a pending action is time-sensitive.
func TestPhoneAskTexts(t *testing.T) {
	host := "“" + phoneHost() + "”"
	requested := "Requested by " + host + ". " + phoneHint
	cases := []struct {
		got                 phoneAsk
		title, body, button string
		urgent              bool
	}{
		{phoneLinkAsk(), "Link this phone to HA NOVA?", "For confirmations from " + host + ". " + phoneHint, "Link", false},
		{phoneChangeAsk(""), "Stop HA NOVA confirmations here?", "After this, HA NOVA may ask in chat, where your AI assistant can answer. " + requested, "Stop", false},
		{phoneChangeAsk("Anna’s Pixel"), "Move HA NOVA confirmations to “Anna’s Pixel”?", requested, "Move", false},
		{phonePlanAsk([3]string{"Delete", "the automation “Night”"}, true), "Delete the automation “Night”?", "Can't be undone. " + requested, "Delete", true},
		{phonePlanAsk([3]string{"Turn on", "“Guest mode”", "This also unlocks “Entrance Door” through the automation “Guest”"}, false),
			"Turn on “Guest mode”?", "This also unlocks “Entrance Door” through the automation “Guest”. " + requested, "Turn on", true},
		{phonePlanAsk([3]string{"Set the position of", "“Gate”", "This closes a gate"}, false), "Set the position of “Gate”?", "This closes a gate. " + requested, "Confirm", true},
		{phonePlanAsk([3]string{"Cancel", "“Timer”"}, false), "Cancel “Timer”?", requested, "Confirm", true},
	}
	for _, c := range cases {
		if c.got != (phoneAsk{c.title, c.body, c.button, c.urgent, !c.urgent}) { // link, move, stop need the unlocked phone
			t.Errorf("ask %+v, want %q %q %q %v", c.got, c.title, c.body, c.button, c.urgent)
		}
	}
	// A long list keeps its count and never cuts inside a name.
	name := "“" + strings.Repeat("Front Door Lock ", 3) + "”"
	title := phonePlanAsk([3]string{"Unlock", name + ", " + name + ", " + name + " and 4 more"}, false).title
	if n := len([]rune(title)); n > phoneTitleLimit || !strings.HasSuffix(title, "”… and 4 more?") {
		t.Errorf("long title %d %q", n, title)
	}
	title = phonePlanAsk([3]string{"Unlock", strings.Repeat("x", 200)}, false).title
	if n := len([]rune(title)); n > phoneTitleLimit || !strings.HasSuffix(title, "…?") {
		t.Errorf("long name %d %q", n, title)
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
		p.sent[0]["message"] != "This also unlocks “Entrance Door” through the automation “Guest mode unlocks”. Requested by “"+phoneHost()+"”. "+phoneHint {
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
