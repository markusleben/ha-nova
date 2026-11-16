package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGateWindowNoticeIsQuietAndStable(t *testing.T) {
	n := windowNotice("a")
	payload := n.payload()
	data := payload["data"].(map[string]any)
	if data["actions"] != nil || data["push"].(map[string]any)["interruption-level"] != "passive" ||
		data["channel"] != "HA NOVA confirmations" || data["importance"] != "low" || data["alert_once"] != true || data["timeout"] != nil ||
		payload["title"] != "Confirmed in the window on Study Mac" || payload["message"] != "Unlock 'Front Door'" {
		t.Fatalf("window payload: %v", payload)
	}
	n.AskRef, n.Title, n.Computer.Name = strings.Repeat("b", 64), "Delete a file?", "Renamed Mac"
	if n.payload()["data"].(map[string]any)["tag"] != data["tag"] {
		t.Fatal("a new confirmation or renamed computer stacked another note")
	}
	n.Computer.ID = "another-computer"
	if n.payload()["data"].(map[string]any)["tag"] == data["tag"] {
		t.Fatal("two computers shared a note")
	}
	n.Lang = "de"
	if title, _ := n.text(); title != "Im Fenster auf Renamed Mac bestätigt" {
		t.Fatalf("German notice: %q", title)
	}
	n.Title, n.Computer.Name = "Unlock\x1b[31m door?", "Mac\n\u202e"
	title, body := n.text()
	if strings.ContainsAny(title+body, "\x1b\n\u202e") {
		t.Fatalf("unsafe notice text: %q / %q", title, body)
	}
}

func TestGateWindowNoticeChecksItsExactTarget(t *testing.T) {
	for _, scenario := range []string{"owner App", "demoted", "changed phone", "unreadable"} {
		t.Run(scenario, func(t *testing.T) {
			h, p, home := familyLink(t)
			home.relay = "owner-id"
			n := windowNotice("a")
			switch scenario {
			case "demoted":
				n.Link = phoneLink{Service: "mobile_app_kids_phone", DeviceID: "dev-kid", UserID: "kid-id"}
			case "changed phone":
				n.Link.DeviceID = "missing"
			case "unreadable":
				home.usersOK = false
			}
			gateWindowEmit(h.paths, n, "old-question", true)
			if scenario != "owner App" {
				if len(p.sent)+len(p.cleared) != 0 {
					t.Fatalf("unverified recipient was notified: %v", p.services)
				}
				return
			}
			if len(p.sent) != 2 || len(p.cleared) != 1 || p.services[0] != n.Link.Service || p.services[1] != n.Link.Service ||
				!strings.HasPrefix(p.sent[0]["data"].(map[string]any)["tag"].(string), "ha-nova-window-") ||
				p.sent[1]["data"].(map[string]any)["tag"] != "old-question" ||
				p.cleared[0] != n.Link.Service+" old-question" {
				t.Fatalf("notice or cleanup targeted the wrong phone/tag: %v %v %v", p.services, p.sent, p.cleared)
			}
			p.sent, p.services, p.cleared = nil, nil, nil
			gateWindowEmit(h.paths, n, "older-question", false)
			if len(p.sent) != 1 || p.sent[0]["data"].(map[string]any)["tag"] != "older-question" || len(p.cleared) != 1 {
				t.Fatalf("an obsolete ask changed the stable note: %v %v", p.sent, p.cleared)
			}
		})
	}
}

func TestGatePhoneWindowEndWinsLateTapAndZeroReply(t *testing.T) {
	for _, scenario := range []string{"late tap", "zero reply"} {
		t.Run(scenario, func(t *testing.T) {
			h, p, _ := familyLink(t)
			n := windowNotice("a")
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			t.Cleanup(func() { once.Do(func() { close(release) }) })
			if scenario == "late tap" {
				p.tapBy = tapFrom(n.Link.Service, n.Link.UserID, true)
				notify := phoneNotify
				phoneNotify = func(s *relaySession, service string, payload map[string]any) error {
					if payload["data"].(map[string]any)["actions"] != nil {
						close(started)
						<-release
					}
					return notify(s, service, payload)
				}
			} else {
				phoneCollect = func(context.Context, *relaySession, time.Duration) ([]phoneEvent, error) {
					onceStarted := false
					select {
					case <-started:
					default:
						onceStarted = true
					}
					if onceStarted {
						close(started)
					}
					<-release
					return nil, errors.New("phone cannot listen")
				}
			}
			ends := &gatePhoneEnds{entries: map[string]*gatePhoneEnd{}, send: gateWindowEmit}
			type result struct {
				answer gatePhoneAnswer
				err    error
			}
			done := make(chan result, 1)
			go func() {
				answer, err := ends.phone(context.Background(), h.paths, gatePhoneRequest{AskRef: n.AskRef, Link: n.Link,
					Lang: n.Lang, Title: n.Title, Body: "Requested by Study Mac", Button: "Unlock", WaitMS: 2000})
				done <- result{answer, err}
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("phone did not start")
			}
			endWindow(t, ends, n)
			once.Do(func() { close(release) })
			select {
			case got := <-done:
				if got.err != nil || got.answer.Answer == "approved" {
					t.Fatalf("late phone answer won: %+v %v", got.answer, got.err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("phone did not finish")
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			last := p.sent[len(p.sent)-1]
			data := last["data"].(map[string]any)
			if last["title"] != "Confirmed in the window on Study Mac" || data["actions"] != nil || !strings.HasPrefix(data["tag"].(string), "ha-nova-window-") {
				t.Fatalf("window outcome was lost: %v", p.sent)
			}
			if scenario == "late tap" && (len(p.sent) != 3 || len(p.cleared) != 1) {
				t.Fatalf("question was not withdrawn: %v %v", p.sent, p.cleared)
			}
			if scenario == "zero reply" && len(p.sent) != 1 {
				t.Fatalf("a zero reply did not yield exactly one stable note: %v", p.sent)
			}
		})
	}
}
