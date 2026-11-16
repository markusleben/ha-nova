package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGatePhoneProgressDistinguishesNeverAskedFromFailedDelivery(t *testing.T) {
	for _, scenario := range []string{"setup", "probe", "too_short", "failed_send", "accepted", "already_ended"} {
		t.Run(scenario, func(t *testing.T) {
			h, p, _ := familyLink(t)
			n := windowNotice("a")
			in := gatePhoneRequest{Link: n.Link, Lang: n.Lang, Title: n.Title, Body: "Requested by Study Mac", Button: "Unlock", WaitMS: 3000}
			ends := &gatePhoneEnds{entries: map[string]*gatePhoneEnd{}, send: func(runtimePaths, gateWindowNotice, string, bool, phoneSetup) {}}
			switch scenario {
			case "setup":
				in.Link.DeviceID = "gone"
			case "probe":
				p.probeErr = errors.New("cannot subscribe")
			case "too_short":
				windowMinWait = 4 * time.Second
			case "failed_send":
				phoneNotify = func(*relaySession, string, map[string]any) error { return errors.New("delivery uncertain") }
			case "accepted":
				p.tapBy = tapFrom(n.Link.Service, n.Link.UserID, true)
			case "already_ended":
				in.AskRef = n.AskRef
				ends.entries[n.AskRef] = &gatePhoneEnd{at: time.Now(), notice: &n}
			}
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/phone", nil)
			answer, err := gatePhoneWithProgress(h.paths, w, r, in, ends)
			if err != nil {
				t.Fatal(err)
			}
			never := scenario != "failed_send" && scenario != "accepted"
			if answer.NotAsked != never {
				t.Fatalf("never asked=%v, want %v: %+v", answer.NotAsked, never, answer)
			}
			accepted := scenario == "accepted"
			if w.Flushed != accepted || (w.Header().Get("X-Ha-Nova-Phone-Sent") == "1") != accepted {
				t.Fatalf("exposure=%v headers=%v, want accepted=%v", w.Flushed, w.Header(), accepted)
			}
		})
	}
}

func TestGatePhoneProgressKeepsAMoveChargedAfterTheFirstPhoneWasAsked(t *testing.T) {
	h, p, _ := familyLink(t)
	var sent atomic.Bool
	notify, collect := phoneNotify, phoneCollect
	phoneNotify = func(s *relaySession, service string, body map[string]any) error {
		err := notify(s, service, body)
		if body["data"].(map[string]any)["actions"] != nil {
			sent.Store(true)
		}
		return err
	}
	phoneCollect = func(ctx context.Context, s *relaySession, span time.Duration) ([]phoneEvent, error) {
		if sent.Load() && span < time.Second {
			return nil, errors.New("second phone cannot listen")
		}
		return collect(ctx, s, span)
	}
	p.tapBy = tapFrom("mobile_app_markuss_iphone", "owner-id", true)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/phone", nil)
	in := gatePhoneRequest{Words: "link", Device: "Study Mac", WaitMS: 3000,
		Link:    phoneLink{Service: "mobile_app_annas_pixel", DeviceID: "dev-anna", UserID: "anna-id"},
		Current: &phoneLink{Service: "mobile_app_markuss_iphone", DeviceID: "dev-markus", UserID: "owner-id"}}
	answer, err := gatePhoneWithProgress(h.paths, w, r, in, &gatePhoneEnds{})
	if err != nil || answer.Answer != "unavailable" || answer.NotAsked || !w.Flushed {
		t.Fatalf("the first phone's accepted question was forgotten: %+v err=%v flushed=%v", answer, err, w.Flushed)
	}
}

func TestGatePhoneProgressFlushesOnceAndLeavesTheFinalJSONReadable(t *testing.T) {
	h, p, _ := familyLink(t)
	n := windowNotice("a")
	p.tapBy = tapFrom(n.Link.Service, n.Link.UserID, true)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/phone", strings.NewReader(`{"lang":"en","title":"Unlock Front?","body":"Requested by Study Mac","button":"Unlock","link":{"service":"mobile_app_markuss_iphone","device_id":"dev-markus","user_id":"owner-id"},"wait_ms":3000}`))
	gateAskHandler(h.paths, w, r, &gatePhoneEnds{})
	if w.Code != http.StatusOK || !w.Flushed || !strings.Contains(w.Body.String(), `"answer":"approved"`) || strings.Contains(w.Body.String(), "not_asked") {
		t.Fatalf("exposure broke the final answer: HTTP %d %s flushed=%v", w.Code, w.Body.String(), w.Flushed)
	}
	plain := httptest.NewRecorder()
	progress := gatePhoneProgress{w: plain}
	progress.accepted()
	progress.accepted()
	if !progress.sent || plain.Header().Get("X-Ha-Nova-Phone-Sent") != "1" || plain.Body.Len() != 0 {
		t.Fatalf("accepted progress wrote a body or lost its header: %+v", plain)
	}
}
