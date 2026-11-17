package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type windowDelivery struct {
	ref, tag string
	latest   bool
}

func windowNotice(ref string) gateWindowNotice {
	n := gateWindowNotice{AskRef: strings.Repeat(ref, 64), Seq: uint64(ref[0]), Outcome: "window", Lang: "en", Title: "Unlock “Front Door”?",
		Link: phoneLink{Service: "mobile_app_markuss_iphone", DeviceID: "dev-markus", UserID: "owner-id"}}
	n.Computer.ID, n.Computer.Name = "computer-one", "Study Mac"
	return n
}

func windowOriginal(n gateWindowNotice) phoneSetup {
	return phoneSetup{owner: "owner-id", targets: []phoneTarget{{Service: n.Link.Service, DeviceID: n.Link.DeviceID}}}
}

func endWindow(t *testing.T, ends *gatePhoneEnds, n gateWindowNotice, supplied ...runtimePaths) {
	t.Helper()
	paths := runtimePaths{}
	if len(supplied) != 0 {
		paths = supplied[0]
	}
	r := httptest.NewRequest(http.MethodPost, "/phone/end", strings.NewReader(string(mustJSON(n))))
	w := httptest.NewRecorder()
	ends.handler(paths, w, r)
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"recorded":true}` {
		t.Fatalf("window end: %d %s", w.Code, w.Body.String())
	}
}

func windowEnds() (*gatePhoneEnds, chan windowDelivery) {
	sent := make(chan windowDelivery, 16)
	return &gatePhoneEnds{entries: map[string]*gatePhoneEnd{}, send: func(_ runtimePaths, n gateWindowNotice, tag string, latest bool, original phoneSetup) {
		sent <- windowDelivery{n.AskRef, tag, latest}
	}}, sent
}

func awaitWindow(t *testing.T, sent <-chan windowDelivery, ref, tag string, latest bool) {
	t.Helper()
	select {
	case got := <-sent:
		if got != (windowDelivery{ref, tag, latest}) {
			t.Fatalf("delivery %+v, want %s / %s / %t", got, ref, tag, latest)
		}
	case <-time.After(time.Second):
		t.Fatal("window notice was not sent")
	}
}

func noWindow(t *testing.T, sent <-chan windowDelivery) {
	t.Helper()
	select {
	case got := <-sent:
		t.Fatalf("unexpected delivery %+v", got)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestGatePhoneEndBeforeRegistration(t *testing.T) {
	ends, sent := windowEnds()
	n := windowNotice("a")
	endWindow(t, ends, n)
	awaitWindow(t, sent, n.AskRef, "", true)
	answer, err := ends.phone(context.Background(), runtimePaths{}, gatePhoneRequest{AskRef: n.AskRef})
	if err != nil || answer.Answer != "none" {
		t.Fatalf("late registration: %+v %v", answer, err)
	}
	endWindow(t, ends, n)
	noWindow(t, sent)
}

func TestGatePhoneEndWaitsForOwnResult(t *testing.T) {
	for _, when := range []string{"during", "after", "zero reply"} {
		t.Run(when, func(t *testing.T) {
			ends, sent := windowEnds()
			n, tag := windowNotice("a"), "original-question"
			ends.entries[n.AskRef] = &gatePhoneEnd{at: time.Now(), registered: true}
			if when == "zero reply" {
				tag = ""
			}
			notice, finish := ends.claim(n.AskRef, tag, phoneSetup{})
			if notice != nil {
				t.Fatal("a window outcome existed before it was recorded")
			}
			if when == "after" {
				finish()
			}
			endWindow(t, ends, n)
			endWindow(t, ends, n)
			if when != "after" {
				noWindow(t, sent)
				finish()
			}
			awaitWindow(t, sent, n.AskRef, tag, true)
			noWindow(t, sent)
		})
	}
}

func TestGatePhoneEndWaitsForTheAskToUnwind(t *testing.T) {
	ends, sent := windowEnds()
	n := windowNotice("a")
	ends.entries[n.AskRef] = &gatePhoneEnd{at: time.Now(), registered: true}
	endWindow(t, ends, n)
	noWindow(t, sent)
	notice, finish := ends.claim(n.AskRef, "question", phoneSetup{})
	finish()
	if notice == nil || notice.Outcome != "window" {
		t.Fatal("the worker lost the window outcome during a send")
	}
	ends.emit(runtimePaths{}, *notice, "question")
	awaitWindow(t, sent, n.AskRef, "question", true)
	endWindow(t, ends, n)
	noWindow(t, sent)
}

func TestGatePhoneEndSerializesPerComputer(t *testing.T) {
	ends, sent := windowEnds()
	first, second := windowNotice("a"), windowNotice("b")
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	ends.send = func(_ runtimePaths, n gateWindowNotice, tag string, latest bool, original phoneSetup) {
		if n.AskRef == first.AskRef {
			close(started)
			<-release
		}
		sent <- windowDelivery{n.AskRef, tag, latest}
	}
	endWindow(t, ends, first)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first send did not start")
	}
	endWindow(t, ends, second)
	third := windowNotice("c")
	third.Computer.ID = "another-computer"
	endWindow(t, ends, third)
	awaitWindow(t, sent, third.AskRef, "", true)
	noWindow(t, sent)
	once.Do(func() { close(release) })
	awaitWindow(t, sent, first.AskRef, "", true)
	awaitWindow(t, sent, second.AskRef, "", true)
}

func TestGatePhoneEndOlderAskCannotReplaceNewerNote(t *testing.T) {
	ends, sent := windowEnds()
	first, second := windowNotice("a"), windowNotice("b")
	ends.entries[first.AskRef] = &gatePhoneEnd{at: time.Now(), registered: true}
	endWindow(t, ends, first)
	endWindow(t, ends, second)
	awaitWindow(t, sent, second.AskRef, "", true)
	ends.emit(runtimePaths{}, first, "old-question")
	awaitWindow(t, sent, first.AskRef, "old-question", false)
	ends.emit(runtimePaths{}, first, "")
	noWindow(t, sent)
}

func TestGatePhoneEndValidationAndAuthentication(t *testing.T) {
	f := newFakeHA()
	paths, _ := setupVerbRelayTest(t, f.reply)
	post := gateTestServer(t, paths)
	valid := windowNotice("a")
	for _, token := range []string{"", strings.Repeat("0", 64)} {
		if status, _ := post("/phone/end", token, map[string]any{}); status != http.StatusUnauthorized {
			t.Fatalf("unauthenticated end: %d", status)
		}
	}
	for _, change := range []func(map[string]any){
		func(n map[string]any) { n["ask_ref"] = "wrong" },
		func(n map[string]any) { n["seq"] = 0 },
		func(n map[string]any) { n["seq"] = -1 },
		func(n map[string]any) { n["seq"] = 1.5 },
		func(n map[string]any) { n["outcome"] = "approved" },
		func(n map[string]any) { n["extra"] = true },
		func(n map[string]any) { n["lang"] = "xx" },
		func(n map[string]any) { n["title"] = "" },
		func(n map[string]any) { n["computer"].(map[string]any)["id"] = "" },
		func(n map[string]any) { n["computer"].(map[string]any)["extra"] = true },
		func(n map[string]any) { n["link"].(map[string]any)["service"] = "notify_everyone" },
	} {
		var in map[string]any
		_ = json.Unmarshal(mustJSON(valid), &in)
		change(in)
		if status, _ := post("/phone/end", gateTestSecret, in); status != http.StatusBadRequest {
			t.Fatalf("invalid end accepted: %v (%d)", in, status)
		}
	}
	ends, sent := windowEnds()
	for _, suffix := range []string{` {}`, ` invalid`} {
		w := httptest.NewRecorder()
		ends.handler(paths, w, httptest.NewRequest(http.MethodPost, "/phone/end", strings.NewReader(string(mustJSON(valid))+suffix)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("trailing JSON accepted: %d", w.Code)
		}
	}
	noWindow(t, sent)
}

func TestGatePhoneEndCapacityAndExpiry(t *testing.T) {
	ends, _ := windowEnds()
	for i := range 1024 {
		ends.entries[fmt.Sprintf("%064x", i)] = &gatePhoneEnd{at: time.Now()}
	}
	if ends.entry(strings.Repeat("f", 64)) != nil {
		t.Fatal("the worker exceeded its phone lifecycle bound")
	}
	ends.entries[fmt.Sprintf("%064x", 0)].at = time.Now().Add(-5 * time.Minute)
	if ends.entry(strings.Repeat("f", 64)) == nil {
		t.Fatal("expired entries still consume capacity")
	}
}
