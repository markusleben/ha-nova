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
		state, _, _ := strings.Cut(observed, ", ")
		key := outcome
		if outcome == "verified" || outcome == "failed" {
			key += "\x00" + sayText(strings.ReplaceAll(state, "_", " "), false)
		}
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
		subject := sayList(who)
		is := map[bool]string{true: " are", false: " is"}[len(who) > 1 || unnamed[key] > 1]
		outcome, state, _ := strings.Cut(key, "\x00")
		switch outcome {
		case "verified":
			parts = append(parts, subject+is+" "+state+" now.")
		case "failed":
			parts = append(parts, subject+" did not change as asked; it shows "+state+".")
		case "accepted":
			parts = append(parts, "Sent to "+subject+".")
		default:
			parts = append(parts, "Sent to "+subject+", but the change does not show yet.")
		}
	}
	envelope["say"] = strings.Join(parts, " ")
}

// findTruncated: a find that returned fewer rows than matched.
func findTruncated(envelope map[string]any) string {
	result, _ := envelope["result"].(map[string]any)
	shown, _ := result["returned"].(float64)
	total, _ := result["total"].(float64)
	if envelope["verb"] != "find" || shown >= total {
		return ""
	}
	return fmt.Sprintf("Showing %v of %v matches: call ha_find again with limit %v before you say what is or is not there.", shown, total, min(total, 200))
}
