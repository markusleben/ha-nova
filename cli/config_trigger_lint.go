package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// transitionalSteps: changes that Home Assistant entities usually make
// through an in-between state (a lock reports unlocking before unlocked), so a
// state trigger `from:` the start `to:` the end may never match. Only pairs
// whose in-between state comes from that start alone, so adding it to
// `from:` keeps the transition: → open (opening also starts from unlocked)
// and armed → triggered (pending follows every armed mode) stay unflagged,
// as do direct changes (lock open → unlocked, alarm → disarmed).
var transitionalSteps = map[string][]struct{ from, to []string }{
	"lock":  {{[]string{"locked"}, []string{"unlocked"}}, {[]string{"unlocked"}, []string{"locked"}}},
	"cover": {{[]string{"open"}, []string{"closed"}}, {[]string{"closed"}, []string{"open"}}},
	"valve": {{[]string{"open"}, []string{"closed"}}, {[]string{"closed"}, []string{"open"}}},
	"alarm_control_panel": {
		{[]string{"disarmed"}, []string{"armed_home", "armed_away", "armed_night", "armed_vacation", "armed_custom_bypass"}},
	},
}

// inBetween names the state an entity reports between from and to.
func inBetween(domain, _, to string) string {
	switch {
	case domain == "alarm_control_panel":
		return "arming"
	case to == "unlocked":
		return "unlocking"
	case to == "locked":
		return "locking"
	case to == "open":
		return "opening"
	}
	return "closing"
}

// draftTriggerNotices flags new or changed automation state triggers that go
// from one settled state straight to another on those domains. A notice, not
// a refusal: some integrations do skip the in-between state. The fix it names
// keeps the user's transition: add the in-between state to `from:`.
func draftTriggerNotices(draft, current []byte) []verbNotice {
	live := map[string]bool{}
	for _, t := range automationTriggers(current) {
		live[triggerKey(t)] = true
	}
	var notices []verbNotice
	for i, raw := range automationTriggers(draft) {
		t, _ := raw.(map[string]any)
		if t == nil || (t["trigger"] != "state" && t["platform"] != "state") || live[triggerKey(raw)] {
			continue
		}
		from, to := stringList(t["from"]), stringList(t["to"])
		for _, entity := range stringList(t["entity_id"]) {
			domain, _, _ := strings.Cut(entity, ".")
			var steps []string
			for _, f := range from {
				for _, o := range to {
					if transitionalPair(domain, f, o) && !slices.Contains(steps, inBetween(domain, f, o)) {
						steps = append(steps, inBetween(domain, f, o))
					}
				}
			}
			if len(steps) == 0 || !allTransitional(domain, from, to) {
				continue
			}
			notices = append(notices, verbNotice{Kind: "draft", Text: fmt.Sprintf(
				"trigger %d may never fire: %q usually reports %q between %q and %q; add it to from: (from: [%s])",
				i+1, entity, strings.Join(steps, "/"), strings.Join(from, "/"), strings.Join(to, "/"), strings.Join(append(slices.Clone(from), steps...), ", "))})
		}
	}
	return notices
}

// allTransitional: every from/to combination is a flagged pair, so no
// combination already matches directly.
func allTransitional(domain string, from, to []string) bool {
	for _, f := range from {
		for _, o := range to {
			if !transitionalPair(domain, f, o) {
				return false
			}
		}
	}
	return true
}

func transitionalPair(domain, from, to string) bool {
	for _, step := range transitionalSteps[domain] {
		if slices.Contains(step.from, from) && slices.Contains(step.to, to) {
			return true
		}
	}
	return false
}

func automationTriggers(config []byte) []any {
	cfg, err := configObjectFromBytes(config)
	if err != nil {
		return nil
	}
	triggers := cfg["triggers"]
	if triggers == nil {
		triggers = cfg["trigger"]
	}
	if list, ok := triggers.([]any); ok {
		return list
	}
	if triggers != nil {
		return []any{triggers}
	}
	return nil
}

// triggerKey: map keys sort on marshal, so key order never counts.
func triggerKey(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
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
