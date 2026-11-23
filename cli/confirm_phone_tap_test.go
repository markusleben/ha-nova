package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A plain tap on the question never answers it and never dead-ends: an ask
// the NOVA page shows opens there (iOS url, Android clickAction, sticky so it
// stays in the shade); any other does nothing on Android (noAction) and gets
// no URL on an iPhone (which would open clickAction as one too). The actions,
// their ids and their unlock never change with it.
func TestPhoneTapNeverDeadEnds(t *testing.T) {
	deadline := time.Now().Add(90 * time.Second)
	page := "/app/local_ha_nova_relay_test?nova_ask=" + strings.Repeat("a", 64)
	for _, lang := range []string{"en", "de"} {
		for _, c := range []struct {
			platform, page                string
			url, clickAction, sticky, msg any
		}{
			{phoneIOS, page, page, page, "true", confirmT(lang, "phone.hint.ios_page")},
			{phoneAndroid, page, page, page, "true", nil},
			{"", page, page, page, "true", confirmT(lang, "phone.hint.other")},
			{phoneIOS, "", nil, nil, nil, confirmT(lang, "phone.hint.ios")},
			{phoneAndroid, "", nil, "noAction", nil, nil},
			{"", "", nil, nil, nil, confirmT(lang, "phone.hint.other")},
		} {
			ask := phonePlanAsk(lang, actionGoal{verb: "unlock", names: []string{"“Front Door”"}}, false)
			ask.page = c.page
			got := phonePayload(ask, c.platform, "tag-1", "OK", "NO", deadline)
			data := got["data"].(map[string]any)
			if data["url"] != c.url || data["clickAction"] != c.clickAction || data["sticky"] != c.sticky {
				t.Errorf("%s %q page %q: url %v clickAction %v sticky %v", lang, c.platform, c.page, data["url"], data["clickAction"], data["sticky"])
			}
			if c.msg != nil && !strings.HasSuffix(got["message"].(string), " "+c.msg.(string)) {
				t.Errorf("%s %q page %q: message %q", lang, c.platform, c.page, got["message"])
			}
			actions := data["actions"].([]any)
			confirm, cancel := actions[0].(map[string]any), actions[1].(map[string]any)
			if len(actions) != 2 || confirm["action"] != "OK" || cancel["action"] != "NO" || confirm["authenticationRequired"] != nil || confirm["destructive"] != true {
				t.Errorf("%s %q page %q: actions %v", lang, c.platform, c.page, actions)
			}
			if c.platform == phoneAndroid {
				for _, key := range []string{"sticky", "clickAction"} {
					if v, set := data[key]; set {
						if _, ok := v.(string); !ok {
							t.Errorf("Android %s is %T", key, v) // FCM data takes strings only
						}
					}
				}
			}
		}
	}
}

// The link ask is the tutorial: its text says how to answer, first, on
// every platform and in every link flow (computer, NOVA page, chat app);
// Android keeps its one-line hint in front. Its Allow still needs the unlock
// and the gesture (the iPhone's tap alert leaves out an unlock action).
func TestLinkAskTeachesTheGesture(t *testing.T) {
	deadline := time.Now().Add(90 * time.Second)
	for _, lang := range []string{"en", "de"} {
		for _, ask := range []phoneAsk{phoneLinkAsk(lang), chatPhoneLinkAsk(lang, false), chatPhoneLinkAsk(lang, true)} {
			for platform, key := range map[string]string{phoneIOS: "phone.teach.ios", phoneAndroid: "phone.teach.android", "": "phone.teach.other"} {
				got := phonePayload(ask, platform, "t", "OK", "NO", deadline)
				want := confirmT(lang, key, "button", ask.button) + " " + ask.body
				if platform == phoneAndroid {
					hint := phoneHint(lang, platform, ask.button, ask.body != phoneRequestedBy(lang, true) && ask.body != phoneRequestedBy(lang, false), false)
					if len([]rune(hint)) > 38 {
						t.Errorf("%s Android first line %q", lang, hint)
					}
					want = hint + " " + want
				}
				if got["message"] != want {
					t.Errorf("%s %q %q: %q, want %q", lang, ask.title, platform, got["message"], want)
				}
				confirm := got["data"].(map[string]any)["actions"].([]any)[0].(map[string]any)
				if confirm["authenticationRequired"] == nil {
					t.Errorf("%s %q %q: the link lost its unlock", lang, ask.title, platform)
				}
			}
		}
		if ask := phonePlanAsk(lang, actionGoal{verb: "unlock", names: []string{"“Front Door”"}}, false); ask.teach {
			t.Errorf("%s: a plan ask teaches", lang)
		}
	}
}

// A no-answer result says how to answer next time where a plain tap could
// have lost the question; an ask the NOVA page showed opened there instead.
func TestNoAnswerSaysHowNextTime(t *testing.T) {
	for _, lang := range []string{"en", "de"} {
		for platform, key := range map[string]string{phoneIOS: "outcome.line.no_answer_ios", phoneAndroid: "outcome.line.no_answer_android", "": "outcome.line.no_answer"} {
			if _, line := phoneNothing(lang, phoneNoAnswer, platform); line != confirmT(lang, key) || !strings.HasPrefix(line, confirmT(lang, "outcome.line.no_answer")) {
				t.Errorf("%s %q: %q", lang, platform, line)
			}
			if line := phoneNoAnswerLine(lang, platform, true); line != confirmT(lang, "outcome.line.no_answer") {
				t.Errorf("%s %q page: %q", lang, platform, line)
			}
		}
		if !strings.Contains(confirmT(lang, "outcome.line.no_answer_ios"), "2026.9.3") {
			t.Errorf("%s: the iPhone setting tip lost its version floor", lang)
		}
	}
}

// The NOVA page link names only a well-formed ref, on the panel the App
// named (the packaged worker has no build identity to name it itself).
func TestGatePhonePage(t *testing.T) {
	ref, panel := strings.Repeat("ab", 32), "/app/2368fcfa_ha_nova_relay"
	if got := gatePhonePage(ref, panel); got != panel+"?nova_ask="+ref {
		t.Errorf("page %q", got)
	}
	for _, bad := range []string{"", "ask1", strings.Repeat("A", 64), ref + "&x=1"} {
		if got := gatePhonePage(bad, panel); got != "" {
			t.Errorf("ref %q: %q", bad, got)
		}
	}
	for _, bad := range []string{"", "/", "/app/", "https://evil.example/app/x", "/app/x?y=1", "/app/../config", "//evil.example/app/x"} {
		if got := gatePhonePage(ref, bad); got != "" {
			t.Errorf("panel %q: %q", bad, got)
		}
	}
	// A panel comes only with an ask's ref, never with a link ask.
	link := phoneLink{Service: "mobile_app_x", DeviceID: "d", UserID: "u"}
	for _, c := range []struct {
		in   gatePhoneRequest
		want bool
	}{
		{gatePhoneRequest{AskRef: ref, Panel: panel, Lang: "en", Title: "T", Body: "B", Button: "OK", Link: link, WaitMS: 2000}, true},
		{gatePhoneRequest{Panel: panel, Lang: "en", Title: "T", Body: "B", Button: "OK", Link: link, WaitMS: 2000}, false},
		{gatePhoneRequest{AskRef: ref, Panel: "/", Lang: "en", Title: "T", Body: "B", Button: "OK", Link: link, WaitMS: 2000}, false},
		{gatePhoneRequest{Words: "link", Device: "Mac", Panel: panel, Link: link, WaitMS: 2000}, false},
	} {
		if got := c.in.valid(); got != c.want {
			t.Errorf("%+v valid %v", c.in, got)
		}
	}
}

// A NOVA App ask with a ref opens the NOVA page with a tap; an iPhone gets
// no reminder then (the tap lost nothing).
func TestGatePhoneAskOpensTheNOVAPage(t *testing.T) {
	h, p, _ := familyLink(t)
	saved := phoneRemindAfter
	phoneRemindAfter = 50 * time.Millisecond
	t.Cleanup(func() { phoneRemindAfter = saved })
	n := windowNotice("c")
	answer, err := gatePhone(context.Background(), h.paths, gatePhoneRequest{AskRef: n.AskRef, Link: n.Link, Panel: "/app/2368fcfa_ha_nova_relay", Lang: "en", Title: n.Title, Body: "Requested by Study Mac", Button: "Unlock", WaitMS: 2500})
	if err != nil || answer.Answer != "none" {
		t.Fatalf("answer %+v %v", answer, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	asked := 0
	for _, body := range p.sent {
		if body["data"].(map[string]any)["actions"] != nil {
			asked++
		}
	}
	if len(p.sent) == 0 || asked != 1 || p.sent[0]["data"].(map[string]any)["url"] != "/app/2368fcfa_ha_nova_relay?nova_ask="+n.AskRef {
		t.Fatalf("sent %v", p.sent)
	}
	if last := p.sent[len(p.sent)-1]; last["message"] != confirmT("en", "outcome.line.no_answer") {
		t.Errorf("result %v", last)
	}
}

// An iPhone removes a tapped question without a word to Home Assistant: it
// gets the same question once more, quietly, with the same tag, ids, level
// and deadline, before the result (which never overtakes it). Android keeps
// its question (a plain tap does nothing), so it gets none.
func TestIPhoneGetsTheQuestionOnceMore(t *testing.T) {
	for _, c := range []struct {
		maker string
		asked int
	}{{"Apple", 2}, {"Google", 1}} {
		h, p, _ := familyLink(t)
		saved := phoneRemindAfter
		phoneRemindAfter = 50 * time.Millisecond
		s, err := newRelaySession(h.paths, verbOptions{})
		if err != nil {
			t.Fatal(err)
		}
		setup := phoneSetup{owner: "owner-id", relayUser: "relay-id", confirmer: "owner-id", targets: []phoneTarget{{Service: "mobile_app_x", Maker: c.maker}}}
		ask := phonePlanAsk("en", actionGoal{verb: "unlock", names: []string{"“Front Door”"}}, false)
		answer, reply := askPhone(context.Background(), s, setup, "ha-nova-t", ask, 2500*time.Millisecond, nil)
		reply.say("Not confirmed: Unlock “Front Door”", "x")
		<-reply.landed
		s.close()
		phoneRemindAfter = saved
		p.mu.Lock()
		if answer != phoneNoAnswer || len(p.sent) != c.asked+1 {
			p.mu.Unlock()
			t.Fatalf("%s: %v, sent %v", c.maker, answer, p.sent)
		}
		first := p.sent[0]["data"].(map[string]any)
		if c.asked == 2 {
			again := p.sent[1]["data"].(map[string]any)
			if p.sent[1]["message"] != p.sent[0]["message"] || again["tag"] != first["tag"] || again["apns_headers"].(map[string]any)["apns-expiration"] != first["apns_headers"].(map[string]any)["apns-expiration"] ||
				again["actions"].([]any)[0].(map[string]any)["action"] != first["actions"].([]any)[0].(map[string]any)["action"] ||
				again["push"].(map[string]any)["sound"] != "none" || again["push"].(map[string]any)["interruption-level"] != "time-sensitive" || first["push"].(map[string]any)["sound"] != nil {
				t.Errorf("reminder %v after %v", again, first)
			}
		}
		if last := p.sent[len(p.sent)-1]; last["data"].(map[string]any)["actions"] != nil || last["data"].(map[string]any)["tag"] != first["tag"] {
			t.Errorf("%s: the result is not last: %v", c.maker, p.sent)
		}
		p.mu.Unlock()
	}
}
