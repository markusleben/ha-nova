package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGatePhoneWindowCorrectionFollowsTheActualResult(t *testing.T) {
	for _, delayed := range []string{"recheck", "notify"} {
		t.Run(delayed, func(t *testing.T) {
			h, p, home := familyLink(t)
			n := windowNotice("a")
			phoneTellSpan = 20 * time.Millisecond
			started, release, noticed := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			t.Cleanup(func() { once.Do(func() { close(release) }) })
			p.tapBy = tapFrom(n.Link.Service, n.Link.UserID, true)
			if delayed == "recheck" {
				extra := h.f.extra
				h.f.extra = func(endpoint string, payload map[string]any) (any, bool) {
					if payload["type"] == "config/auth/list" && home.reads == 2 {
						close(started)
						<-release
					}
					return extra(endpoint, payload)
				}
			} else {
				notify := phoneNotify
				phoneNotify = func(s *relaySession, service string, payload map[string]any) error {
					if title, _ := payload["title"].(string); strings.HasPrefix(title, "Approved:") {
						close(started)
						<-release
					}
					return notify(s, service, payload)
				}
			}
			ends := &gatePhoneEnds{entries: map[string]*gatePhoneEnd{}, send: func(paths runtimePaths, notice gateWindowNotice, tag string, latest bool) {
				gateWindowEmit(paths, notice, tag, latest)
				close(noticed)
			}}
			done := make(chan gatePhoneAnswer, 1)
			go func() {
				answer, err := ends.phone(context.Background(), h.paths, gatePhoneRequest{AskRef: n.AskRef, Link: n.Link,
					Lang: n.Lang, Title: n.Title, Body: "Requested by Study Mac", Button: "Unlock", WaitMS: 2000})
				if err != nil {
					t.Error(err)
				}
				done <- answer
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("the result did not reach the delayed stage")
			}
			endWindow(t, ends, n, h.paths)
			select {
			case <-noticed:
				t.Fatal("window correction overtook the actual phone result")
			case <-time.After(3 * phoneTellSpan):
			}
			once.Do(func() { close(release) })
			select {
			case answer := <-done:
				if answer.Answer != "none" {
					t.Fatalf("the late result was returned as %s", answer.Answer)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("the phone result did not finish")
			}
			select {
			case <-noticed:
			case <-time.After(3 * time.Second):
				t.Fatal("the window correction did not follow the result")
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			if len(p.sent) != 4 || !strings.HasPrefix(p.sent[1]["title"].(string), "Approved:") ||
				!strings.HasPrefix(p.sent[2]["data"].(map[string]any)["tag"].(string), "ha-nova-window-") ||
				p.sent[3]["data"].(map[string]any)["tag"] != p.sent[0]["data"].(map[string]any)["tag"] || len(p.cleared) != 1 {
				t.Fatalf("the actual result was not followed by a stable note and question cleanup: %v %v", p.sent, p.cleared)
			}
		})
	}
}

func TestGatePhoneEndHTTPWiring(t *testing.T) {
	h, p, _ := familyLink(t)
	post := gateTestServer(t, h.paths)
	n := windowNotice("a")
	for _, in := range []map[string]any{
		{"ask_ref": "invalid", "lang": "en", "title": "T", "body": "B", "button": "OK", "link": n.Link, "wait_ms": 2000},
		{"ask_ref": n.AskRef, "words": "link", "device": "Study Mac", "link": n.Link, "wait_ms": 2000},
	} {
		if status, _ := post("/phone", gateTestSecret, in); status != http.StatusBadRequest {
			t.Fatalf("invalid correlated phone request: %d", status)
		}
	}
	var end map[string]any
	_ = json.Unmarshal(mustJSON(n), &end)
	if status, _ := post("/phone/end", gateTestSecret, end); status != http.StatusOK {
		t.Fatalf("end status: %d", status)
	}
	if status, out := post("/phone", gateTestSecret, map[string]any{"ask_ref": n.AskRef, "lang": "en", "title": "T", "body": "B", "button": "OK", "link": n.Link, "wait_ms": 2000}); status != http.StatusOK || out["answer"] != "none" {
		t.Fatalf("late registration: %d %v", status, out)
	}
	waitFor(t, "the stable window note", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.sent) > 0
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.sent) != 1 || p.sent[0]["data"].(map[string]any)["actions"] != nil {
		t.Fatalf("HTTP late registration sent a question: %v", p.sent)
	}
}
