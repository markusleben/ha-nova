package main

import (
	"strings"
	"testing"
)

// The one-time code from HA NOVA: four digits; spaces and full-width digits
// read as typed digits, everything else stays a wrong code.
func TestTypedCode(t *testing.T) {
	for in, want := range map[string]string{
		"4821": "4821", " 4821\n": "4821", "48 21": "4821", "4 8 2 1": "4821", "４８２１": "4821", "４８ ２１": "4821",
		"٤٨٢١": "4821", "۴۸۲۱": "4821", "४८२१": "4821", "\u200e4821\u200f": "4821", "48\u200b21": "4821",
		"yes": "yes", "4.821": "4.821", "4,821": "4,821", "#4821": "#4821", "confirm:4821": "confirm:4821", "  ": "",
	} {
		if got := typedCode(in); got != want {
			t.Errorf("typedCode(%q) = %q, want %q", in, got, want)
		}
	}
	codes := map[string]bool{}
	for i := 0; i < 64; i++ {
		code := confirmCode(strings.Repeat("0", 12) + strings.Repeat("0", 14) + string("0123456789abcdef"[i%16]) + string("0123456789abcdef"[i/16]) + strings.Repeat("0", 36))
		if !fourDigits.MatchString(code) {
			t.Fatalf("code %q", code)
		}
		codes[code] = true
	}
	if len(codes) < 60 {
		t.Fatalf("codes repeat: %d distinct of 64", len(codes))
	}
}

// The form ask replaces only HA NOVA's own ask, never words inside a quoted
// name, and the code is gone from the result.
func TestWithholdAskSkipsQuotedNames(t *testing.T) {
	say := "Unlock “Door Type 4821 to go ahead”: locked → unlocked. Type 4821 to unlock “Door Type 4821 to go ahead” — a plain yes can't confirm this."
	want := "Unlock “Door Type 4821 to go ahead”: locked → unlocked. " + sayFormAsk
	if got := withholdAsk(say, "4821"); got != want {
		t.Fatalf("got %q", got)
	}
}

// Input that is not four digits can never match: no attempt is used.
func TestApplyMalformedCodeUsesNoAttempt(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	plan := h.plan("lock.unlock", "lock.front").Plan
	for _, typed := range []string{"yes", plan.Code + ".", "4,821", "four eight two one", "123456"} {
		if exit, env := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", typed); exit != 4 || env.Error.Code != "CODE_REQUIRED" {
			t.Fatalf("%q: exit %d env %+v", typed, exit, env)
		}
	}
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", "\u200e"+plan.Code[:2]+" "+plan.Code[2:]); exit != 0 || env.Status != "verified" {
		t.Fatalf("right code after malformed tries: exit %d env %+v", exit, env)
	}
}

// A typed plan stored before four-digit codes is refused, also on its old
// code or a "yes"; the user re-plans and gets a four-digit code.
func TestApplyRefusesAPlanWithAnOldCode(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	for _, typed := range []string{"confirm:unlock-front-abcdef", "yes", ""} {
		plan := h.plan("lock.unlock", "lock.front").Plan
		h.tamper(plan.ID, func(p map[string]any) { p["code"] = "confirm:unlock-front-abcdef" })
		args := []string{"--plan", plan.ID}
		if typed != "" {
			args = append(args, "--confirm", typed)
		}
		if exit, env := h.run(runApplyCommand, args...); exit != 4 || env.Error.Code != "PLAN_EXPIRED" || !strings.Contains(env.Error.Next, "Plan again") {
			t.Fatalf("%q: exit %d env %+v", typed, exit, env)
		}
		if exit, env := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", plan.Code); exit != 4 || env.Error.Code != "PLAN_NOT_FOUND" {
			t.Fatalf("the old plan is gone: exit %d env %+v", exit, env)
		}
	}
	if len(h.posts()) != 0 {
		t.Fatalf("sent %d", len(h.posts()))
	}
	if plan := h.plan("lock.unlock", "lock.front").Plan; !fourDigits.MatchString(plan.Code) {
		t.Fatalf("re-plan code %q", plan.Code)
	}
}
