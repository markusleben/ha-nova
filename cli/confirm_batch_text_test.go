package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// A batch's texts (docs/work/2026-10-29-batch-confirmation-spec.md §6): one
// list in the window, the form, and the chat; the phone's count and its
// most sensitive item; the window budget. Chat apps: mcp_batch_test.go.

// batchFourList: batchFour's list, every item named, through the status.
const batchFourList = "I'm about to make these 4 changes in Home Assistant, in this order:\n" +
	"1. Change the automation “Night mode”.\n2. Change the script “Arrival”.\n3. Delete the automation “Kitchen motion old”.\n4. Delete the script “Old arrival”.\n" +
	"One confirmation covers all 4, so I need you to confirm it. A copy of each one deleted is saved first, so it can be restored. " +
	"HA NOVA checks each one again right before it is sent and stops at the first one that doesn't go through. Nothing has changed yet."

// The window shows the list the chat shows; one answer sends all four.
func TestBatchWindowShowsEveryItem(t *testing.T) {
	h := newWriteHarness(t, batchHome())
	w := useWindow(t, windowConfirmed)
	exit, env := h.batch(batchFour(h)...)
	if exit != 3 || env.Plan.Channel != channelWindow || env.Plan.Code != "" || env.Say != batchFourList+" "+sayWindowAsk ||
		!strings.Contains(env.NextStep, "apply --plan "+env.Plan.ID+" without --confirm") || strings.Contains(env.NextStep, "up to 2 minutes") ||
		!strings.HasSuffix(env.NextStep, " Give that apply up to 10 minutes: it checks each one again, sends it, and checks it after, in turn.") {
		t.Fatalf("plan batch: exit %d say %q next %q", exit, env.Say, env.NextStep)
	}
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != 0 || itemStatuses(batchItems(t, applied)) != "verified,verified,verified,verified" || len(w.messages) != 1 || w.messages[0] != batchFourList+"\n\n"+windowAsked {
		t.Fatalf("apply: exit %d %+v window %q", exit, applied.Error, w.messages)
	}
}

// A window that can't open makes the batch a chat batch with a code, said
// and with the same list; the window batch is used up.
func TestBatchWindowFallbackKeepsTheList(t *testing.T) {
	h := newWriteHarness(t, batchHome())
	useWindow(t, windowFailed)
	_, old := h.batch(batchFour(h)...)
	exit, env := h.run(runApplyCommand, "--plan", old.Plan.ID)
	if exit != 3 || env.Plan == nil || env.Plan.Channel != channelChat || env.Plan.Code == "" || len(h.writes()) != 0 ||
		env.Say != sayNoWindowHere+batchFourList+"\n"+sayChatNote+sayCodeBlock(env.Plan.Code) || !strings.HasSuffix(env.NextStep, "up to 10 minutes: it checks each one again, sends it, and checks it after, in turn.") {
		t.Fatalf("fallback: exit %d say %q next %q", exit, env.Say, env.NextStep)
	}
	if exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code); exit != 0 || len(h.writes()) != 4 {
		t.Fatalf("the fallback batch: exit %d %+v", exit, applied.Error)
	}
	if files := h.planFiles(); len(files) != 0 {
		t.Fatalf("plans left: %v", files)
	}
}

// The phone asks with the count, the most sensitive item with its reason,
// and Confirm; its result takes the question's place.
func TestBatchPhoneAsksWithTheCountAndTheMostSensitive(t *testing.T) {
	for name, c := range map[string]struct {
		refuse        string
		exit          int
		title, result string
	}{
		"done":   {"", 0, "Done: Change or delete 4 automations and scripts", "Home Assistant confirms each one."},
		"partly": {"/s1", 6, "Partly done: Change or delete 4 automations and scripts", "2 of 4 went through. The computer says why."},
	} {
		t.Run(name, func(t *testing.T) {
			h := newWriteHarness(t, batchHome())
			p := usePhones(t, "mobile_app_iphone")
			p.answer = func(ok, _ string) []phoneEvent { return []phoneEvent{tap(ok, "owner-id", "REMOTE")} }
			ids := []string{h.writePlan("automation", "a1", map[string]any{"alias": "Night mode", "actions": []any{step("light.turn_on", "light.hall")}}),
				h.member("delete", "automation.kitchen_motion_old"),
				h.writePlan("script", "s1", map[string]any{"alias": "Arrival", "sequence": []any{step("lock.unlock", "lock.front")}}),
				h.member("delete", "script.old_arrival")}
			exit, env := h.batch(ids...)
			if exit != 3 || env.Plan.Channel != channelPhone || env.Plan.Code != "" || !strings.HasSuffix(env.Say, sayPhoneAsk("Confirm", "")) ||
				!strings.Contains(env.Say, "\n3. Change the script “Arrival”. ") {
				t.Fatalf("plan batch: exit %d say %q", exit, env.Say)
			}
			if c.refuse != "" {
				h.f.configWrite = writeThen(c.refuse, nil, coreReply(400, map[string]any{"message": "Message malformed"}))
			}
			if exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID); exit != c.exit || len(p.sent) != 2 {
				t.Fatalf("apply: exit %d %+v notifications %d", exit, applied.Error, len(p.sent))
			}
			confirm := p.sent[0]["data"].(map[string]any)["actions"].([]any)[0].(map[string]any)
			message := p.sent[0]["message"].(string)
			if p.sent[0]["title"] != "Change or delete 4 automations and scripts?" || confirm["title"] != "Confirm" ||
				!strings.HasPrefix(message, "Most sensitive: Change the script “Arrival”. ") || !strings.Contains(message, "Requested by “"+phoneHost()+"”.") ||
				strings.Contains(message, "Kitchen") || strings.Contains(message, "Night") {
				t.Fatalf("the ask: %q %q button %v", p.sent[0]["title"], message, confirm["title"])
			}
			if p.sent[1]["title"] != c.title || p.sent[1]["message"] != c.result {
				t.Fatalf("the result: %q %q", p.sent[1]["title"], p.sent[1]["message"])
			}
		})
	}
}

// Every phone title names the count and what happens, on one line at 20.
func TestBatchPhoneTitles(t *testing.T) {
	member := func(op, domain string) batchMember {
		return batchMember{stored: storedPlan{ConfigAction: &configAction{Op: op, Domain: domain}, ConfigRequest: &configRequest{Op: op, Domain: domain}}}
	}
	for _, c := range []struct {
		members []batchMember
		want    string
	}{
		{[]batchMember{member("delete", "automation"), member("delete", "automation")}, "Delete 2 automations?"},
		{[]batchMember{member("delete", "script"), member("delete", "script")}, "Delete 2 scripts?"},
		{[]batchMember{member("delete", "script"), member("delete", "automation")}, "Delete 2 automations and scripts?"},
		{[]batchMember{member("write", "automation"), member("write", "automation")}, "Change 2 automations?"},
		{[]batchMember{member("write", "script"), member("write", "script")}, "Change 2 scripts?"},
		{[]batchMember{member("write", "automation"), member("write", "script")}, "Change 2 automations and scripts?"},
		{[]batchMember{member("write", "automation"), member("delete", "automation")}, "Change or delete 2 automations?"},
		{[]batchMember{member("delete", "script"), member("write", "script")}, "Change or delete 2 scripts?"},
		{[]batchMember{member("write", "script"), member("delete", "automation")}, "Change or delete 2 automations and scripts?"},
	} {
		rp := batchReplan{members: c.members, lang: "en"}
		if got := batchPhoneTitle(rp); got != c.want {
			t.Errorf("title %q, want %q", got, c.want)
		}
		for len(rp.members) < batchMaxMembers {
			rp.members = append(rp.members, c.members[0])
		}
		if title := batchPhoneTitle(rp); utf8.RuneCountInString(phoneTitle(title, "")) > 44 || phoneTitle(title, "") != title || !strings.Contains(title, " 20 ") {
			t.Errorf("at 20: %q", title)
		}
	}
}

// The longest list that may show (20 deletes, 60-character names) fits the
// window; a longer one is refused, never cut, and its plans come back.
func TestBatchWindowBudget(t *testing.T) {
	f := newFakeHA()
	name := func(i int) string { return fmt.Sprintf("Long automation name %02d %s", i, strings.Repeat("y", 36)) }
	for i := 1; i <= batchMaxMembers; i++ {
		f.storeConfig(fmt.Sprintf("/api/config/automation/config/l%d", i), map[string]any{"alias": name(i), "actions": []any{step("light.turn_on", "light.hall")}})
	}
	h := newWriteHarness(t, f)
	var deletes, unlocks []string
	for i := 1; i <= batchMaxMembers; i++ {
		deletes = append(deletes, h.member("delete", "automation."+strings.ReplaceAll(strings.ToLower(name(i)), " ", "_")))
	}
	exit, env := h.batch(deletes...)
	if exit != 3 || env.Plan == nil {
		t.Fatalf("20 deletes: exit %d %+v", exit, env.Error)
	}
	stored, err := readPlanFile(filepath.Join(planDir(h.paths), env.Plan.ID+".json"))
	text := confirmText(stored, false) + "\n\n" + windowBoth + "\n" + windowAsked
	lines, chars := strings.Count(text, "\n")+1, utf8.RuneCountInString(text)
	t.Logf("the longest window: %d lines, %d characters", lines, chars)
	if err != nil || lines > batchWindowLines || chars > batchWindowChars || !strings.Contains(text, "\n20. Delete the automation “"+name(20)+"”.\n") {
		t.Fatalf("%d lines, %d characters (%v): %s", lines, chars, err, text)
	}
	for i := 1; i <= batchMaxMembers; i++ {
		unlocks = append(unlocks, h.writePlan("automation", fmt.Sprintf("l%d", i), map[string]any{"alias": name(i), "actions": []any{step("lock.unlock", "lock.front")}}))
	}
	before := h.planFiles()
	if exit, env := h.batch(unlocks...); exit != 4 || env.Error.Code != "BATCH_TOO_LONG" || !strings.Contains(env.Error.Next, "Split the list") ||
		strings.Join(h.planFiles(), ",") != strings.Join(before, ",") {
		t.Fatalf("20 unlocks: exit %d %+v files %d", exit, env.Error, len(h.planFiles()))
	}
}
