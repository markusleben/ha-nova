package main

import (
	"fmt"
	"strings"
)

// Plain things for the model to relay (chat apps run small models that
// copy what a result gives them): a call's outcome as one sentence with
// names, never ids, and a find that showed only part of its matches.

// addOutcomeSay sets answer_rule, and say on a call or apply that ran a service call and
// has none: “Kitchen Lights” is on now. Rows without a name are counted,
// never shown by id; failures with an error keep their own message.
func addOutcomeSay(envelope map[string]any) {
	envelope["answer_rule"] = mcpResultRule
	rows, _ := envelope["verification"].([]any)
	if envelope["say"] != nil || envelope["error"] != nil || envelope["action"] == nil || len(rows) == 0 {
		return
	}
	names := map[string]string{}
	resolved, _ := envelope["resolved"].([]any)
	for _, raw := range resolved {
		t, _ := raw.(map[string]any)
		id, _ := t["entity_id"].(string)
		name, _ := t["name"].(string)
		names[id] = sayText(name, true)
	}
	var order []string
	groups := map[string][]string{}
	unnamed := map[string]int{}
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		id, _ := row["entity_id"].(string)
		outcome, _ := row["outcome"].(string)
		observed, _ := row["observed"].(string)
		key := outcome + "\x00" + plainStates[observed] // observed is "state, field=value…": a comma names no plain state
		if _, seen := groups[key]; !seen {
			order, groups[key] = append(order, key), nil
		}
		if names[id] != "" {
			groups[key] = append(groups[key], names[id])
		} else {
			unnamed[key]++
		}
	}
	var parts []string
	for _, key := range order {
		who := groups[key]
		if n := unnamed[key]; n > 0 {
			who = append(who, map[bool]string{true: "1 device", false: fmt.Sprint(n, " devices")}[n == 1])
		}
		many := len(who) > 1 || unnamed[key] > 1
		subject, is, it := sayList(who), map[bool]string{true: " are", false: " is"}[many], map[bool]string{true: "they", false: "it"}[many]
		outcome, state, _ := strings.Cut(key, "\x00")
		switch {
		case outcome == "verified" && state != "":
			parts = append(parts, subject+is+" "+state+" now.")
		case outcome == "verified":
			parts = append(parts, subject+" changed as asked.")
		case outcome == "failed" && state != "":
			parts = append(parts, subject+" did not change as asked; "+it+map[bool]string{true: " show ", false: " shows "}[many]+state+".")
		case outcome == "failed":
			parts = append(parts, subject+" did not change as asked.")
		case outcome == "accepted" && state != "":
			parts = append(parts, "Sent to "+subject+"; "+it+map[bool]string{true: " are", false: " is"}[many]+" still "+state+".")
		case outcome == "accepted":
			parts = append(parts, "Sent to "+subject+".")
		default:
			parts = append(parts, "Sent to "+subject+", but the change does not show yet.")
		}
	}
	envelope["say"] = strings.Join(parts, " ")
}

// plainStates: the end states a say may name, in plain words; any other
// state (free text, select options, attribute changes) is "changed as asked".
var plainStates = map[string]string{"on": "on", "off": "off", "locked": "locked", "unlocked": "unlocked", "open": "open", "closed": "closed",
	"jammed": "jammed", "disarmed": "disarmed", "triggered": "triggered", "armed_away": "armed away", "armed_home": "armed home",
	"armed_night": "armed night", "armed_vacation": "armed vacation", "armed_custom_bypass": "armed custom bypass",
	"opening": "opening", "closing": "closing", "locking": "locking", "unlocking": "unlocking", "arming": "arming", "disarming": "disarming", "pending": "pending"}

// findTruncated: a find that returned fewer rows than matched.
func findTruncated(envelope map[string]any) string {
	result, _ := envelope["result"].(map[string]any)
	shown, _ := result["returned"].(float64)
	total, _ := result["total"].(float64)
	switch {
	case envelope["verb"] != "find" || shown >= total:
		return ""
	case shown >= 200: // the limit's ceiling: asking again shows the same rows
		return fmt.Sprintf("Showing %v of %v matches: narrow it by domain, area, or state; say only what is shown.", shown, total)
	}
	return fmt.Sprintf("Showing %v of %v matches: call ha_find again with limit %v before you say what is or is not there.", shown, total, min(total, 200))
}
