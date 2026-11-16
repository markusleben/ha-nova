package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGateWindowNoticeReachesALinkWithoutAPersonTracker(t *testing.T) {
	for _, relay := range []string{"supervisor-id", "owner-id"} {
		t.Run(relay, func(t *testing.T) {
			h, p, home := familyLink(t)
			home.relay, home.devices[0].UserID = relay, ""
			n := windowNotice("a")
			gateWindowEmit(h.paths, n, "old-question", true, windowOriginal(n))
			if len(p.sent) != 2 || len(p.cleared) != 1 || p.services[0] != n.Link.Service || p.services[1] != n.Link.Service ||
				!strings.HasPrefix(p.sent[1]["data"].(map[string]any)["tag"].(string), "ha-nova-window-") ||
				p.sent[0]["data"].(map[string]any)["tag"] != "old-question" {
				t.Fatalf("the retained phone link lost its note or correction without a person tracker: %v %v %v", p.sent, p.services, p.cleared)
			}
		})
	}
}

func TestGatePhoneEndSequenceRejectsAnOlderLateArrival(t *testing.T) {
	ends, sent := windowEnds()
	older, newer := windowNotice("a"), windowNotice("b")
	endWindow(t, ends, newer)
	awaitWindow(t, sent, newer.AskRef, "", true)
	endWindow(t, ends, older)
	noWindow(t, sent)
	ends.emit(runtimePaths{}, older, "old-question")
	awaitWindow(t, sent, older.AskRef, "old-question", false)
	newer.Seq += 100
	endWindow(t, ends, newer)
	noWindow(t, sent)
}

func TestGatePhoneWindowWithdrawsTheOriginalPhoneWithoutReverification(t *testing.T) {
	for _, scenario := range []string{"unreadable", "demoted", "renamed"} {
		t.Run(scenario, func(t *testing.T) {
			h, p, home := familyLink(t)
			n := windowNotice("a")
			n.Link = phoneLink{Service: "mobile_app_annas_pixel", DeviceID: "dev-anna", UserID: "anna-id"}
			service := n.Link.Service
			if scenario == "renamed" {
				service = "mobile_app_renamed_pixel"
				home.devices[1].Service, home.devices[1].Name = service, "Renamed Pixel"
				home.notify[1] = service
				home.install(t, h.f)
			}
			started, release, noticed := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			t.Cleanup(func() { once.Do(func() { close(release) }) })
			notify := phoneNotify
			phoneNotify = func(s *relaySession, service string, payload map[string]any) error {
				err := notify(s, service, payload)
				if payload["data"].(map[string]any)["actions"] != nil {
					close(started)
					<-release
				}
				return err
			}
			ends := &gatePhoneEnds{entries: map[string]*gatePhoneEnd{}, send: func(paths runtimePaths, notice gateWindowNotice, tag string, latest bool, original phoneSetup) {
				gateWindowEmit(paths, notice, tag, latest, original)
				close(noticed)
			}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan gatePhoneAnswer, 1)
			go func() {
				answer, err := ends.phone(ctx, h.paths, gatePhoneRequest{AskRef: n.AskRef, Link: n.Link,
					Lang: n.Lang, Title: n.Title, Body: "Requested by Study Mac", Button: "Unlock", WaitMS: 2000})
				if err != nil {
					t.Error(err)
				}
				done <- answer
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("question did not reach its original phone")
			}
			if scenario == "unreadable" {
				home.usersOK = false
			} else if scenario == "demoted" {
				home.users[2]["group_ids"] = []string{"system-users"}
			}
			endWindow(t, ends, n, h.paths)
			cancel()
			once.Do(func() { close(release) })
			select {
			case answer := <-done:
				if answer.Answer != "none" {
					t.Fatalf("window outcome was lost: %+v", answer)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("phone request did not end")
			}
			select {
			case <-noticed:
			case <-time.After(3 * time.Second):
				t.Fatal("original question was not withdrawn")
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			want := 2
			if scenario == "renamed" {
				want = 3
			}
			if len(p.sent) != want || len(p.cleared) != 1 || p.services[0] != service || p.services[1] != service ||
				p.sent[1]["data"].(map[string]any)["actions"] != nil || p.sent[1]["title"] != confirmT("en", "outcome.ended") ||
				p.sent[1]["data"].(map[string]any)["tag"] != p.sent[0]["data"].(map[string]any)["tag"] ||
				phoneLastResults(h.paths)["owner-id "+service] != p.sent[0]["data"].(map[string]any)["tag"] {
				t.Fatalf("cleanup depended on re-verification or used the stale service: %v %v %v", p.sent, p.services, p.cleared)
			}
		})
	}
}

func TestGatePhoneWindowCorrectsAQuestionThatOutlivesTheWithdrawal(t *testing.T) {
	h, p, _ := familyLink(t)
	n := windowNotice("a")
	phoneTellSpan = 20 * time.Millisecond
	started, release, noticed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	notify := phoneNotify
	var mu sync.Mutex
	var trace []string
	phoneNotify = func(s *relaySession, service string, payload map[string]any) error {
		kind := "note"
		if payload["data"].(map[string]any)["actions"] != nil {
			kind = "question"
			close(started)
			<-release
		} else if payload["message"] == "clear_notification" {
			kind = "clear"
		}
		err := notify(s, service, payload)
		mu.Lock()
		trace = append(trace, kind)
		mu.Unlock()
		return err
	}
	ends := &gatePhoneEnds{entries: map[string]*gatePhoneEnd{}, send: func(paths runtimePaths, notice gateWindowNotice, tag string, latest bool, original phoneSetup) {
		gateWindowEmit(paths, notice, tag, latest, original)
		close(noticed)
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan gatePhoneAnswer, 1)
	go func() {
		answer, err := ends.phone(ctx, h.paths, gatePhoneRequest{AskRef: n.AskRef, Link: n.Link,
			Lang: n.Lang, Title: n.Title, Body: "Requested by Study Mac", Button: "Unlock", WaitMS: 2000})
		if err != nil {
			t.Error(err)
		}
		done <- answer
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("question send did not start")
	}
	endWindow(t, ends, n, h.paths)
	cancel()
	select {
	case answer := <-done:
		if answer.Answer != "none" {
			t.Fatalf("the ended question returned %s", answer.Answer)
		}
	case <-time.After(time.Second):
		t.Fatal("the HTTP answer waited for the stalled question")
	}
	select {
	case <-noticed:
		t.Fatal("final correction preceded the actual question send")
	case <-time.After(3 * phoneTellSpan):
	}
	once.Do(func() { close(release) })
	select {
	case <-noticed:
	case <-time.After(3 * time.Second):
		t.Fatal("the late question was not corrected")
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(trace, ",") != "question,note,clear,note" {
		t.Fatalf("question-tag cleanup did not follow the actual late send: %v", trace)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.sent) != 3 || len(p.cleared) != 1 || p.sent[1]["data"].(map[string]any)["actions"] != nil ||
		p.sent[1]["data"].(map[string]any)["tag"] != p.sent[0]["data"].(map[string]any)["tag"] ||
		p.sent[1]["data"].(map[string]any)["channel"] != gateNoticeChannel || p.sent[1]["data"].(map[string]any)["importance"] != "low" {
		t.Fatalf("the old question kept its buttons: %v %v", p.sent, p.cleared)
	}
}
