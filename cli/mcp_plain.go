package main

import "fmt"

// Plain things for the model to relay (chat apps run small models that
// copy what a result gives them): the answer rule, and a find that showed
// only part of its matches.

// addOutcomeSay sets answer_rule; the CLI's result line is the say
// (resultSay, say_lines.go).
func addOutcomeSay(envelope map[string]any) {
	envelope["answer_rule"] = mcpResultRule
}

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
