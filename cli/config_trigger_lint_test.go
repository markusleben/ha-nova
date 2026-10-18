package main

import (
	"strings"
	"testing"
)

func TestDraftTriggerNoticesFlagSkippedTransitionalStates(t *testing.T) {
	for draft, want := range map[string]string{
		// rc4 lane: never fired, the lock reports unlocking first.
		`{"triggers":[{"trigger":"state","entity_id":"lock.front_door","from":"locked","to":"unlocked"}]}`:                        `trigger 1 may never fire: "lock.front_door" usually reports "unlocking" between "locked" and "unlocked"; add it to from: (from: [locked, unlocking])`,
		`{"trigger":[{"platform":"state","entity_id":["light.x","cover.garage"],"from":"closed","to":["open"]}]}`:                 `"cover.garage" usually reports "opening"`,
		`{"triggers":{"trigger":"state","entity_id":"alarm_control_panel.home","from":"disarmed","to":"armed_away"}}`:             `reports "arming"`,
		`{"triggers":[{"trigger":"state","entity_id":"alarm_control_panel.home","from":"armed_away","to":"triggered"}]}`:          `reports "pending"`,
		`{"triggers":[{"trigger":"time","at":"07:00"},{"trigger":"state","entity_id":"valve.main","from":"open","to":"closed"}]}`: `trigger 2 may never fire: "valve.main" usually reports "closing"`,
		// Fine: to: alone, the in-between state named, direct changes, other domains, same state.
		`{"triggers":[{"trigger":"state","entity_id":"lock.front_door","to":"unlocked"}]}`:                                  "",
		`{"triggers":[{"trigger":"state","entity_id":"lock.front_door","from":["locked","unlocking"],"to":"unlocked"}]}`:    "",
		`{"triggers":[{"trigger":"state","entity_id":"lock.front_door","from":"open","to":"unlocked"}]}`:                    "",
		`{"triggers":[{"trigger":"state","entity_id":"lock.front_door","from":["unavailable","unknown"],"to":"unlocked"}]}`: "",
		`{"triggers":[{"trigger":"state","entity_id":"alarm_control_panel.home","from":"armed_away","to":"disarmed"}]}`:     "",
		`{"triggers":[{"trigger":"state","entity_id":"alarm_control_panel.home","from":"triggered","to":"disarmed"}]}`:      "",
		`{"triggers":[{"trigger":"state","entity_id":"light.x","from":"off","to":"on"}]}`:                                   "",
		`{"triggers":[{"trigger":"state","entity_id":"cover.garage","from":"open","to":"open"}]}`:                           "",
		`{"sequence":[]}`: "",
		`not json`:        "",
	} {
		notices := draftTriggerNotices([]byte(draft), nil)
		switch {
		case want == "" && len(notices) != 0:
			t.Errorf("%s: unexpected %+v", draft, notices)
		case want != "" && (len(notices) != 1 || notices[0].Kind != "draft" || !strings.Contains(notices[0].Text, want)):
			t.Errorf("%s: notices %+v, want one containing %q", draft, notices, want)
		}
	}
}

// A trigger already live and unchanged (a rename, a device swap re-point)
// is not the draft's to fix; key order does not count as a change.
func TestDraftTriggerNoticesSkipUnchangedLiveTriggers(t *testing.T) {
	live := []byte(`{"alias":"Old","triggers":[{"to":"unlocked","from":"locked","entity_id":"lock.front_door","trigger":"state"}]}`)
	draft := []byte(`{"alias":"New","triggers":[{"trigger":"state","entity_id":"lock.front_door","from":"locked","to":"unlocked"}]}`)
	if notices := draftTriggerNotices(draft, live); len(notices) != 0 {
		t.Fatalf("unchanged live trigger flagged: %+v", notices)
	}
}

func TestPlanWriteNamesATriggerThatMayNeverFire(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	draft := hallDraft("light.turn_on")
	draft["triggers"] = []any{map[string]any{"trigger": "state", "entity_id": "lock.front", "from": "locked", "to": "unlocked"}}
	exit, env := h.run(runPlanCommand, "write", "--domain", "automation", "--file", draftFile(t, draft))
	if exit != 3 || len(env.Notices) != 1 || env.Notices[0].Kind != "draft" || !strings.Contains(env.Notices[0].Text, "lock.front") {
		t.Fatalf("exit %d notices %+v", exit, env.Notices)
	}
	// apply re-plans; the notice belongs to the plan only.
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != 0 {
		t.Fatalf("apply: exit %d env %+v", exit, applied)
	}
	for _, n := range applied.Notices {
		if n.Kind == "draft" {
			t.Fatalf("apply repeated the draft notice: %+v", applied.Notices)
		}
	}
}
