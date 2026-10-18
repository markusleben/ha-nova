package main

import (
	"fmt"
	"slices"
	"strings"
)

// settledStates: domains whose entities pass through a transitional state
// between two settled ones (a lock reports unlocking before unlocked), so a
// state trigger `from:` one settled state `to:` another may never match.
var settledStates = map[string]struct {
	states  []string
	between string
}{
	"lock":                {[]string{"locked", "unlocked", "open"}, "locking, unlocking, or opening"},
	"cover":               {[]string{"open", "closed"}, "opening or closing"},
	"valve":               {[]string{"open", "closed"}, "opening or closing"},
	"alarm_control_panel": {[]string{"disarmed", "armed_home", "armed_away", "armed_night", "armed_vacation", "armed_custom_bypass", "triggered"}, "arming, pending, or disarming"},
}

// draftTriggerNotices flags automation state triggers that go from one
// settled state straight to another on those domains. A notice, not a
// refusal: some integrations do skip the transitional state, and the user
// may want exactly that transition.
func draftTriggerNotices(draft []byte) []verbNotice {
	cfg, err := configObjectFromBytes(draft)
	if err != nil {
		return nil
	}
	triggers := cfg["triggers"]
	if triggers == nil {
		triggers = cfg["trigger"]
	}
	list, ok := triggers.([]any)
	if !ok && triggers != nil {
		list = []any{triggers}
	}
	var notices []verbNotice
	for i, raw := range list {
		t, _ := raw.(map[string]any)
		if t == nil || (t["trigger"] != "state" && t["platform"] != "state") {
			continue
		}
		from, to := stringList(t["from"]), stringList(t["to"])
		if len(from) == 0 || len(to) == 0 || slices.ContainsFunc(from, func(s string) bool { return slices.Contains(to, s) }) {
			continue
		}
		for _, entity := range stringList(t["entity_id"]) {
			domain, _, _ := strings.Cut(entity, ".")
			settled, ok := settledStates[domain]
			if !ok || !allIn(from, settled.states) || !allIn(to, settled.states) {
				continue
			}
			notices = append(notices, verbNotice{Kind: "draft", Text: fmt.Sprintf(
				"trigger %d may never fire: %s goes through %s between %q and %q; use to: without from: unless the user asked for that exact change",
				i+1, entity, settled.between, strings.Join(from, "/"), strings.Join(to, "/"))})
		}
	}
	return notices
}

// stringList reads a string or a list of strings; anything else is empty.
func stringList(v any) []string {
	switch v := v.(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil
			}
			out = append(out, s)
		}
		return out
	}
	return nil
}

func allIn(values, set []string) bool {
	for _, v := range values {
		if !slices.Contains(set, v) {
			return false
		}
	}
	return true
}
