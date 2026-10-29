package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// Setup's offer to move chat apps after "Use another phone" (spec
// docs/work/2026-10-24-chat-app-phone-move-spec.md, "App and page"): only
// an App that says phone_change gets it, and the move asks the phone chat
// apps use now first. Older Apps keep setup.chat.phone_kept.

const (
	moveIPhoneID = "0123456789abcdef0123456789abcdef"
	movePixelID  = "fedcba9876543210fedcba9876543210"
	moveJobID    = "0123456789abcdef0123456789abcdef"
)

// moveTestPaths: this computer links the owner's own Pixel.
func moveTestPaths(t *testing.T, user string) runtimePaths {
	t.Helper()
	return writeTestConfigFile(t, `{"schema_version":1,"confirm_phone":{"owner-id":{"service":"mobile_app_markus_pixel","device_id":"`+movePixelID+`","user_id":"`+user+`","name":"Markus’ Pixel"}}}`)
}

// useChatMove answers the App's calls with answer and keeps every request
// body (nil: a GET).
func useChatMove(t *testing.T, answer func(body map[string]any) (chatAppsState, error)) func() []map[string]any {
	t.Helper()
	call, every, wait, session, user := chatAppsCall, chatAppsPollEvery, chatAppsMoveWait, setupOwnerIngress, setupSignedInUser
	t.Cleanup(func() {
		chatAppsCall, chatAppsPollEvery, chatAppsMoveWait, setupOwnerIngress, setupSignedInUser = call, every, wait, session, user
	})
	setupOwnerIngress, setupSignedInUser, chatAppsPollEvery, chatAppsMoveWait = &CloudIngressClient{}, "owner-id", time.Millisecond, 5*time.Second
	var mu sync.Mutex
	var bodies []map[string]any
	chatAppsCall = func(_ context.Context, _ *CloudIngressClient, body map[string]any) (chatAppsState, error) {
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		return answer(body)
	}
	return func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return bodies
	}
}

func TestSetupPageChatMoveOfferNeedsAnAppThatMovesIt(t *testing.T) {
	useChatMove(t, func(map[string]any) (chatAppsState, error) { return chatAppsState{}, errors.New("no call expected") })
	iphone, pixel := "Markus’ iPhone", "Markus’ Pixel"
	f := &setupFlow{paths: moveTestPaths(t, "owner-id")}
	moved := func(c *setupPageChat, state chatAppsState) *setupPageChat {
		f.pageChatMoved(c, &chatAppsOffer{state: state})
		return c
	}

	older := moved(&setupPageChat{State: "on"}, chatAppsState{Enabled: true, OwnerPhone: &iphone})
	if older.Problem == nil || older.Problem.Key != "setup.chat.phone_kept" || older.Move != nil || older.accepts() != nil {
		t.Fatalf("an older App: %+v", older)
	}
	newer := moved(&setupPageChat{State: "on"}, chatAppsState{Enabled: true, OwnerPhone: &iphone, OwnerPhoneID: moveIPhoneID, PhoneChange: true})
	if want := (&setupPageChatMove{State: "offer", From: iphone, To: pixel, id: movePixelID}); newer.Problem != nil || !reflect.DeepEqual(newer.Move, want) {
		t.Fatalf("a newer App: %+v %+v", newer, newer.Move)
	}
	if got := newer.accepts(); !reflect.DeepEqual(got, []string{"chat_move"}) {
		t.Fatalf("accepts %v", got)
	}
	if same := moved(&setupPageChat{State: "on"}, chatAppsState{Enabled: true, OwnerPhone: &iphone, OwnerPhoneID: movePixelID, PhoneChange: true}); same.Problem != nil || same.Move != nil {
		t.Fatalf("chat apps ask that phone already (under its old name): %+v", same)
	}
	// Another phone with the old one's name: the device id tells them apart.
	if twin := moved(&setupPageChat{State: "on"}, chatAppsState{Enabled: true, OwnerPhone: &pixel, OwnerPhoneID: moveIPhoneID, PhoneChange: true}); twin.Move == nil || twin.Move.To != pixel {
		t.Fatalf("a phone with the old one's name: %+v", twin)
	}
	if rerun := moved(&setupPageChat{State: "page_on", PageMove: true}, chatAppsState{}); rerun.Problem == nil || rerun.Problem.Key != "setup.chat.phone_page" {
		t.Fatalf("a re-run without the owner session: %+v", rerun)
	}
	f.paths = moveTestPaths(t, "anna-id") // a link here that isn't the owner's own can't confirm for chat apps
	if other := moved(&setupPageChat{State: "on"}, chatAppsState{Enabled: true, OwnerPhone: &iphone, PhoneChange: true}); other.Problem == nil || other.Problem.Key != "setup.chat.phone_kept" || other.Move != nil {
		t.Fatalf("someone else's phone: %+v", other)
	}
}

// The App's version tells a re-run whether the NOVA page can move them.
func TestSetupPageChatMoveOnARerunNeedsANewerApp(t *testing.T) {
	read := chatAppsReadHA
	t.Cleanup(func() { chatAppsReadHA = read })
	for version, want := range map[string]bool{"0.12.1": false, "0.12.2": true, "0.12.2-rc1": true, "0.13.0": true} {
		chatAppsReadHA = func(runtimePaths) (chatAppsHA, error) {
			return chatAppsHA{version: "2026.10.2", present: true, loaded: true, externalURL: "https://home.example.com", appVersion: version}, nil
		}
		if row := (&setupFlow{}).pageRerunChat(func() string { return "" }); row == nil || row.State != "page_on" || row.PageMove != want {
			t.Errorf("App %s: %+v, want PageMove %v", version, row, want)
		}
	}
}

func TestSetupPageChatMoveAsksTheOldPhoneFirstThenLinks(t *testing.T) {
	reads := []chatAppsPhoneJob{
		{ID: moveJobID, Status: "unread"},
		{ID: moveJobID, Status: "running", Step: "old", Phone: "Markus’ iPhone", Platform: "ios"},
		{ID: moveJobID, Status: "running", Step: "new", Phone: "Markus’ Pixel", Platform: "android"},
		{ID: moveJobID, Status: "done", Step: "new", Phone: "Markus’ Pixel", Platform: "android"},
	}
	var mu sync.Mutex
	bodies := useChatMove(t, func(body map[string]any) (chatAppsState, error) {
		if body != nil {
			return chatAppsState{Enabled: true, PhoneChange: true, PhoneJob: &chatAppsPhoneJob{ID: moveJobID, Status: "running"}}, nil
		}
		mu.Lock()
		defer mu.Unlock()
		job := reads[0]
		if len(reads) > 1 {
			reads = reads[1:]
		}
		return chatAppsState{Enabled: true, PhoneChange: true, PhoneJob: &job}, nil
	})
	p := newPageStepsTest(t, moveTestPaths(t, "owner-id"), "codex")
	iphone := "Markus’ iPhone"
	offer := &chatAppsOffer{state: chatAppsState{Enabled: true, OwnerPhone: &iphone, OwnerPhoneID: moveIPhoneID, PhoneChange: true}}
	c := &setupPageChat{State: "on"}
	p.flow.pageChatMoved(c, offer)
	var shown []string
	published := c.snapshot() // the page encodes its own copy while the move runs
	if published.Move == c.Move {
		t.Fatal("the published row shares the move it keeps changing")
	}
	p.flow.pageChatMove(c, offer, true, func() { shown = append(shown, c.Move.State+" "+c.Move.Step+" "+c.Move.Phone+" "+c.Move.OS) })
	if want := []map[string]any{{"owner_phone_move": map[string]any{"device_id": movePixelID}}}; !reflect.DeepEqual(bodies()[:1], want) {
		t.Fatalf("posted %v, want only the move, without override", bodies())
	}
	if want := []string{"run old Markus’ iPhone ", "run old Markus’ iPhone ios", "run new Markus’ Pixel android"}; !reflect.DeepEqual(shown, want) {
		t.Fatalf("shown %q, want %q", shown, want)
	}
	if c.Move.State != "done" || offer.state.OwnerPhone == nil || *offer.state.OwnerPhone != "Markus’ Pixel" || offer.state.OwnerPhoneID != movePixelID || !c.Confirmed || c.accepts() != nil {
		t.Fatalf("after the move: %+v %+v", c, c.Move)
	}
	if published.Move.State != "offer" {
		t.Fatalf("the published copy changed with the row: %+v", published.Move)
	}
}

func TestSetupPageChatMoveSaysHowItEnded(t *testing.T) {
	iphone := "Markus’ iPhone"
	ended := func(status, step string) func(map[string]any) (chatAppsState, error) {
		return func(body map[string]any) (chatAppsState, error) {
			job := chatAppsPhoneJob{ID: moveJobID, Status: status, Step: step, Phone: "Markus’ iPhone"}
			if body != nil {
				job.Status = "running"
			}
			return chatAppsState{Enabled: true, PhoneChange: true, PhoneJob: &job}, nil
		}
	}
	refusedWith := func(status int, code string) func(map[string]any) (chatAppsState, error) {
		return func(map[string]any) (chatAppsState, error) {
			return chatAppsState{}, &chatAppsRefused{status: status, code: code}
		}
	}
	refused := func(code string) func(map[string]any) (chatAppsState, error) { return refusedWith(409, code) }
	failing := func(err error) func(map[string]any) (chatAppsState, error) {
		return func(map[string]any) (chatAppsState, error) { return chatAppsState{}, err }
	}
	for name, test := range map[string]struct {
		answer      func(map[string]any) (chatAppsState, error)
		wait        time.Duration
		state, asks string
		lost        bool // the line says how to go on without a lost phone
	}{
		"cancelled":                 {answer: ended("declined", "old"), state: "declined", asks: "Markus’ iPhone"},
		"no tap on the old phone":   {answer: ended("timeout", "old"), state: "timeout", asks: "Markus’ iPhone", lost: true},
		"failed on the new phone":   {answer: ended("failed", "new"), state: "failed", asks: "Markus’ iPhone"},
		"lost by the bridge":        {answer: ended("lost", "old"), state: "unknown", asks: "Markus’ iPhone"},
		"another change runs":       {answer: refused("PHONE_CHANGE_RUNNING"), state: "busy", asks: iphone},
		"the old phone gone":        {answer: refused("OLD_PHONE_GONE"), state: "gone", asks: iphone},
		"the old phone unavailable": {answer: refused("OLD_PHONE_UNAVAILABLE"), state: "unavailable", asks: iphone},
		"not a phone that confirms": {answer: refused("PHONE_NOT_ELIGIBLE"), state: "refused", asks: iphone},
		"the bridge not reached":    {answer: refusedWith(503, "CHAT_PHONE_UNAVAILABLE"), state: "refused", asks: iphone},
		"the App's answer lost":     {answer: refusedWith(504, "CHAT_PHONE_UNKNOWN"), state: "unknown", asks: iphone},
		"the sign-in ran out":       {answer: failing(newCloudError(CloudErrOuterSessionExpired, "reach the NOVA App", errors.New("expired"))), state: "expired", asks: iphone},
		"the start answer lost":     {answer: failing(newCloudError(CloudErrHAProtocol, "reach the NOVA App", errors.New("connection reset"))), state: "unknown", asks: iphone},
		"it never ends":             {answer: ended("running", "old"), wait: 20 * time.Millisecond, state: "unknown", asks: iphone},
	} {
		t.Run(name, func(t *testing.T) {
			useChatMove(t, test.answer)
			if test.wait > 0 {
				chatAppsMoveWait = test.wait
			}
			p := newPageStepsTest(t, moveTestPaths(t, "owner-id"), "codex")
			offer := &chatAppsOffer{state: chatAppsState{Enabled: true, OwnerPhone: &iphone, OwnerPhoneID: moveIPhoneID, PhoneChange: true}}
			c := &setupPageChat{State: "on"}
			p.flow.pageChatMoved(c, offer)
			p.flow.pageChatMove(c, offer, true, func() {})
			if c.Move.State != test.state || c.Move.Phone != test.asks || c.Move.Step != "" || c.Move.Lost != test.lost || *offer.state.OwnerPhone != iphone || c.Confirmed {
				t.Fatalf("%+v, want %s asked by %s (lost %v) and chat apps on the iPhone still", c.Move, test.state, test.asks, test.lost)
			}
		})
	}
	t.Run("linked meanwhile", func(t *testing.T) {
		useChatMove(t, refused("PHONE_ALREADY_LINKED"))
		p := newPageStepsTest(t, moveTestPaths(t, "owner-id"), "codex")
		offer := &chatAppsOffer{state: chatAppsState{Enabled: true, OwnerPhone: &iphone, OwnerPhoneID: moveIPhoneID, PhoneChange: true}}
		c := &setupPageChat{State: "on"}
		p.flow.pageChatMoved(c, offer)
		p.flow.pageChatMove(c, offer, true, func() {})
		if c.Move.State != "done" || c.Move.Phone != "Markus’ Pixel" || offer.state.OwnerPhoneID != movePixelID {
			t.Fatalf("%+v %+v", c.Move, offer.state)
		}
	})
	t.Run("Keep", func(t *testing.T) {
		bodies := useChatMove(t, refused("NOT_CALLED"))
		c := &setupPageChat{State: "on", Move: &setupPageChatMove{State: "offer", From: iphone, To: "Markus’ Pixel", id: movePixelID}}
		(&setupFlow{}).pageChatMove(c, &chatAppsOffer{}, false, func() {})
		if c.Move.State != "kept" || len(bodies()) != 0 {
			t.Fatalf("%+v, calls %v", c.Move, bodies())
		}
	})
	if !strings.Contains(setupT("setup.chat.move_offer", "old", "A", "phone", "B"), "Each phone asks once more") {
		t.Fatal("the offer says each phone asks once more")
	}
}
