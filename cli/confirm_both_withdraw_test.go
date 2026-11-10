package main

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
)

// Settled on the computer, the phone's question goes from every asked
// phone, as exact payloads in order: a quiet note without buttons and with
// the question's tag (iOS replaces the notification itself, where a clear
// alone needs the app woken and an iPhone kept the question), then the
// clear. Unanswered, the result replaces it as before (no clear). A stop
// on the computer: TestBothAskStoppedOnTheComputer.
func TestComputerAnswerReplacesThePhoneQuestion(t *testing.T) {
	cases := map[string]struct {
		window      windowAnswer
		gate        bool // the window answers once the question is out; else never (the deadline)
		title, line string
		exit        int
		withdrawn   bool
	}{
		"confirmed in the window":   {windowConfirmed, true, "Confirmed on the computer: Unlock “lock.front”", "Nothing more to do; the computer shows the result.", 0, true},
		"cancelled in the window":   {windowCancelled, true, "Cancelled: Unlock “lock.front”", "Stopped on the computer. Nothing changed.", 4, true},
		"no answer by the deadline": {windowConfirmed, false, "Not confirmed: Unlock “lock.front”", "No answer reached Home Assistant. Nothing changed.", 4, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := newWriteHarness(t, newFakeHA())
			p := usePhones(t, "mobile_app_iphone", "mobile_app_pixel")
			gate := make(chan struct{})
			useRaceWindow(t, c.window, gate, false)
			if c.gate {
				go func() { p.waitSent(); close(gate) }()
			}
			fake := phoneNotify
			var mu sync.Mutex
			calls := map[string][]map[string]any{}
			phoneNotify = func(s *relaySession, service string, body map[string]any) error {
				mu.Lock()
				calls[service] = append(calls[service], body)
				mu.Unlock()
				return fake(s, service, body)
			}
			plan := h.plan("lock.unlock", "lock.front").Plan
			if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != c.exit || len(h.posts()) != map[bool]int{true: 1, false: 0}[c.exit == 0] {
				t.Fatalf("exit %d err %+v posts %d", exit, env.Error, len(h.posts()))
			}
			mu.Lock()
			defer mu.Unlock()
			for _, service := range []string{"mobile_app_iphone", "mobile_app_pixel"} {
				got := calls[service]
				if len(got) == 0 || got[0]["data"].(map[string]any)["actions"] == nil {
					t.Fatalf("%s: no question first: %v", service, got)
				}
				tag := got[0]["data"].(map[string]any)["tag"].(string)
				want := []map[string]any{got[0], {"title": c.title, "message": c.line, "data": withdrawnNoteData(tag)},
					{"message": "clear_notification", "data": map[string]any{"tag": tag, "priority": "high"}}}
				if !c.withdrawn {
					want = []map[string]any{got[0], phoneResultPayload(c.title, c.line, tag)}
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("%s sent\n%v\nwant\n%v", service, fmt.Sprint(got[1:]), fmt.Sprint(want[1:]))
				}
			}
		})
	}
}
