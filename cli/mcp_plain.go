package main

import (
	"fmt"
	"strings"
	"time"
)

// Plain things for the model to relay (chat apps run small models that
// copy what a result gives them): the answer rule, a chat code's say with
// everything above its code, and a find that showed only part of its
// matches.

// addOutcomeSay sets answer_rule; the CLI's result line is the say
// (resultSay, say_lines.go).
func addOutcomeSay(envelope map[string]any) {
	envelope["answer_rule"] = mcpResultRule
}

// demoSayNote: the user's part of the demo notice (demoNoticeText), in
// every chat code's say there (all visitors share one demo user, so no
// once-per-session memory fits). The relock clause only for a lock; near
// the reset, the reset line (demoResetText) replaces the hourly clause.
func demoSayNote(lock, resetSoon bool) string {
	note := "Never type real names or passwords here."
	if lock {
		note = "Its entrance door locks again 10 minutes after it is unlocked. " + note
	}
	if resetSoon {
		return demoResetText(verbNow()) + "\n" + note
	}
	return "The demo house resets every hour. " + note
}

// sayAboveCode keeps a chat code the last thing the user reads: what they
// need besides the code (the demo notices, when the code expires) goes into
// say above the chat note and the code block, and the returned step tells
// the model to end with say. "" when say has no code block (forms withheld
// it, or no code).
func sayAboveCode(envelope map[string]any) string {
	plan, _ := envelope["plan"].(map[string]any)
	code, _ := plan["code"].(string)
	say, _ := envelope["say"].(string)
	head, found := strings.CutSuffix(say, sayCodeBlock(code))
	if code == "" || !found {
		return ""
	}
	note := ""
	for _, n := range chatNotes { // the note stays right above the code
		if cut, ok := strings.CutSuffix(head, "\n"+n); ok {
			head, note = cut, "\n"+n
		}
	}
	var lines, held []string
	kinds := map[any]bool{}
	notices, _ := envelope["notices"].([]any)
	for _, raw := range notices {
		notice, _ := raw.(map[string]any)
		kinds[notice["kind"]] = true
	}
	if kinds["demo"] {
		action, _ := envelope["action"].(map[string]any)
		service, _ := action["service"].(string)
		lines = append(lines, demoSayNote(strings.HasPrefix(service, "lock."), kinds["demo_reset"] && demoResetText(verbNow()) != ""))
		held = append(held, "the demo house notices")
	}
	expiresAt, _ := plan["expires_at"].(string)
	expires, err := time.Parse(time.RFC3339, expiresAt)
	if reset := verbNow().Truncate(time.Hour).Add(time.Hour); !demoPlansSince.IsZero() && reset.Before(expires) {
		expires = reset // the hosted demo's hourly reset drops every plan
	}
	if err == nil && expires.After(verbNow()) {
		left := int((expires.Sub(verbNow()) + time.Minute - 1) / time.Minute) // rounded up: a fresh plan says 15
		lines, held = append(lines, fmt.Sprintf("This code works for %d %s.", left, map[bool]string{true: "minute", false: "minutes"}[left == 1])), append(held, "when the code expires")
	}
	if len(lines) > 0 {
		envelope["say"] = head + "\n" + strings.Join(lines, "\n") + note + sayCodeBlock(code)
	}
	step := "End your message with `say` in their language, its lines in order: anything else you tell the user goes above it"
	if len(held) > 0 {
		step += " (never repeat what `say` already tells: " + strings.Join(held, " and ") + ")"
	}
	return step + ", keep its line breaks and the bold, and add nothing after the code. If the user answers without the code, or it does not match, end your reply the same way: `say`'s last lines (its note and code block), nothing after the code. Several codes in one message: each `say`'s other lines above (each note once), then one line \"To go ahead, type these codes:\" and one line per code, last, named as its `say` names it: <name>: **<code>**."
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
