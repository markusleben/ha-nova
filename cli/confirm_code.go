package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// The one-time code from HA NOVA (docs/work/2026-10-14-typed-verbs-spec.md
// §2.1 Code): four digits a typed plan carries and only the user types.

// confirmCode is four digits drawn from the plan identity hex(sha256(hash ‖
// nonce)): uniform over 0000-9999 (the bias of a 64-bit value mod 10^4 is
// below 10^-15), unpredictable without the 16-byte CSPRNG nonce, and
// recomputed at apply from the recomputed hash, never read from the file.
// The id shows only the first 12 hex characters; the code takes the next 16.
func confirmCode(idHex string) string {
	n, _ := strconv.ParseUint(idHex[12:28], 16, 64)
	return fmt.Sprintf("%04d", n%10000)
}

var fourDigits = regexp.MustCompile(`^[0-9]{4}$`)

// typedCode is what the user typed as a code: spaces and invisible format
// characters (LRM, RLM, ZWSP) removed, and full-width (CJK keyboards),
// Arabic-Indic, Persian, and Devanagari digits read as 0-9, so "48 21" and
// "４８２１" are 4821. Nothing else is read: letters, punctuation, and "yes"
// stay as typed and never match.
func typedCode(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		for _, zero := range []rune{'０', '٠', '۰', '०'} {
			if r >= zero && r <= zero+9 {
				return '0' + r - zero
			}
		}
		return r
	}, s)
}

// staleCode: a typed plan stored by an HA NOVA before four-digit codes
// (`confirm:…`). Apply refuses it and no form is shown; the user re-plans.
// The file field only ever narrows: apply still compares against the code
// it derives itself.
func staleCode(plan storedPlan) bool {
	return plan.Tier == tierTyped.String() && !fourDigits.MatchString(plan.Code)
}

// sayTypedAsk ends a typed plan's `say`; goal is the plan's own action
// ("unlock “Front Door”"), so the code reads as bound to it.
func sayTypedAsk(code, goal string) string {
	return "Type " + code + " to " + goal + " — a plain yes can't confirm this."
}

// sayFormAsk replaces sayTypedAsk where the client shows a confirmation
// form: the code is never in a tool result there (mcp_confirm.go).
const sayFormAsk = "Your app will ask for the four-digit one-time code from HA NOVA in a form — a plain yes can't confirm this."

// withholdAsk swaps the closing ask for sayFormAsk: the last "Type <code> to "
// outside “…”, since a Home Assistant name (always quoted; sayText strips
// quote marks from it) may contain the same words.
func withholdAsk(say, code string) string {
	at, depth := -1, 0
	for i, r := range say {
		switch {
		case r == '“':
			depth++
		case r == '”':
			depth--
		case depth == 0 && strings.HasPrefix(say[i:], "Type "+code+" to "):
			at = i
		}
	}
	if at < 0 { // unreachable (say and code come from one plan); fail closed
		return sayFormAsk
	}
	return say[:at] + sayFormAsk
}

// lowerFirst: "Unlock “Door”" → "unlock “Door”" (names are quoted, so only
// the verb changes).
func lowerFirst(s string) string {
	r := []rune(s)
	if len(r) > 0 {
		r[0] = unicode.ToLower(r[0])
	}
	return string(r)
}
