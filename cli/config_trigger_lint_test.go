package main

import (
	"strings"
	"testing"
)

func TestDraftTriggerNoticesFlagSkippedTransitionalStates(t *testing.T) {
	for draft, want := range map[string]string{
		// rc4 lane: never fired, the lock reports unlocking first.
		`{"triggers":[{"trigger":"state","entity_id":"lock.front_door","from":"locked","to":"unlocked"}]}`:                        `trigger 1 may never fire: lock.front_door goes through locking, unlocking, or opening between "locked" and "unlocked"`,
		`{"trigger":[{"platform":"state","entity_id":["light.x","cover.garage"],"from":"closed","to":["open"]}]}`:                 `trigger 1 may never fire: cover.garage goes through opening or closing`,
		`{"triggers":{"trigger":"state","entity_id":"alarm_control_panel.home","from":"disarmed","to":"armed_away"}}`:             `arming, pending, or disarming`,
		`{"triggers":[{"trigger":"time","at":"07:00"},{"trigger":"state","entity_id":"valve.main","from":"open","to":"closed"}]}`: `trigger 2 may never fire: valve.main`,
		// Fine: to: alone, a transitional state named, not_from, other domains, same state.
		`{"triggers":[{"trigger":"state","entity_id":"lock.front_door","to":"unlocked"}]}`:                                  "",
		`{"triggers":[{"trigger":"state","entity_id":"lock.front_door","from":"unlocking","to":"unlocked"}]}`:               "",
		`{"triggers":[{"trigger":"state","entity_id":"lock.front_door","not_from":"unlocked","to":"unlocked"}]}`:            "",
		`{"triggers":[{"trigger":"state","entity_id":"lock.front_door","from":["unavailable","unknown"],"to":"unlocked"}]}`: "",
		`{"triggers":[{"trigger":"state","entity_id":"light.x","from":"off","to":"on"}]}`:                                   "",
		`{"triggers":[{"trigger":"state","entity_id":"cover.garage","from":"open","to":"open"}]}`:                           "",
		`{"sequence":[]}`: "",
		`not json`:        "",
	} {
		notices := draftTriggerNotices([]byte(draft))
		switch {
		case want == "" && len(notices) != 0:
			t.Errorf("%s: unexpected %+v", draft, notices)
		case want != "" && (len(notices) != 1 || notices[0].Kind != "draft" || !strings.Contains(notices[0].Text, want)):
			t.Errorf("%s: notices %+v, want one containing %q", draft, notices, want)
		}
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
}
