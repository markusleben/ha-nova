package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// The phone link on a gated App (gate_phone_move.go): moved lazily, on
// evidence only, never an approval; setup confirm-phone links through the
// Relay there; doctor says how the safety check stands.

// phone answers POST /gate/v1/phone as the Relay does (gate-asks.ts openLink).
func (g *fakeGate) phone(p map[string]any) any {
	g.mu.Lock()
	defer g.mu.Unlock()
	link, _ := p["link"].(map[string]any)
	g.links, g.ops = append(g.links, link), append(g.ops, "link")
	switch {
	case g.phoneAnswer != nil:
		return g.phoneAnswer
	case reflect.DeepEqual(g.link, link):
		return map[string]any{"needed": false}
	}
	return map[string]any{"needed": true, "ask_id": "link1", "channels": []string{"phone"}}
}

var annaWire = map[string]any{"service": "mobile_app_annas_pixel", "device_id": "dev-anna", "user_id": "anna-id", "platform": "android"}

// gatedFamily: the family home on a gated App, with Anna's Pixel linked on
// this computer (seedLink) when linked.
func gatedFamily(t *testing.T, g *fakeGate, linked bool) *writeHarness {
	t.Helper()
	f := newFakeHA()
	newFamilyHome().install(t, f)
	h := gatedHarness(t, f, g)
	if linked {
		seedLink(t, h, &annaLink)
	}
	return h
}

func TestGateMovesThePhoneOnEvidence(t *testing.T) {
	for name, g := range map[string]*fakeGate{
		"no channel": {phones: true},
		"page only":  {phones: true, channels: []string{"page"}},
	} {
		t.Run(name, func(t *testing.T) {
			h := gatedFamily(t, g, true)
			exit, applied := h.run(runApplyCommand, "--plan", h.plan("lock.unlock", "lock.front").Plan.ID)
			asks, ops, sends := g.snapshot()
			// The link is no approval: the typed ask is opened again and its own grant sends it.
			if exit != 0 || applied.Status != "verified" || len(asks) != 2 || len(g.links) != 1 || !reflect.DeepEqual(g.links[0], annaWire) ||
				strings.Join(ops, ",") != "open,link,wait link1,open,wait" || len(sends) != 1 || sends[0] != (gateSend{gateUnlock, "grant-ask2"}) {
				t.Fatalf("exit %d %+v; ops %v links %v sends %+v", exit, applied.Error, ops, g.links, sends)
			}
			if *linked(t, h) != annaLink { // a move never changes this computer's entry
				t.Fatalf("local link %+v", linked(t, h))
			}
		})
	}
}

func TestGatePhoneMoveSkipsWithoutCause(t *testing.T) {
	t.Run("same link", func(t *testing.T) { // paused: the Relay holds this link already
		g := &fakeGate{phones: true, channels: []string{"page"}, link: annaWire, paused: true}
		h := gatedFamily(t, g, true)
		exit, applied := h.run(runApplyCommand, "--plan", h.plan("lock.unlock", "lock.front").Plan.ID)
		asks, ops, sends := g.snapshot()
		if exit != 0 || len(asks) != 1 || strings.Join(ops, ",") != "open,link,wait" || len(sends) != 1 || sends[0].grant != "grant-ask1" {
			t.Fatalf("exit %d %+v; ops %v sends %+v", exit, applied.Error, ops, sends)
		}
	})
	t.Run("phone offered", func(t *testing.T) {
		g := &fakeGate{phones: true, link: annaWire}
		h := gatedFamily(t, g, true)
		exit, _ := h.run(runApplyCommand, "--plan", h.plan("lock.unlock", "lock.front").Plan.ID)
		if _, ops, _ := g.snapshot(); exit != 0 || strings.Join(ops, ",") != "open,wait" {
			t.Fatalf("exit %d ops %v", exit, ops)
		}
	})
	for name, link := range map[string]*phoneLink{"no local link": nil, "its phone gone": {Service: "mobile_app_old", DeviceID: "dev-gone", UserID: "owner-id", Name: "Old Phone"}} {
		t.Run(name, func(t *testing.T) {
			g := &fakeGate{phones: true}
			h := gatedFamily(t, g, false)
			if link != nil {
				seedLink(t, h, link)
			}
			exit, applied := h.run(runApplyCommand, "--plan", h.plan("lock.unlock", "lock.front").Plan.ID)
			if _, ops, sends := g.snapshot(); exit != verbExitRefused || applied.Error.Code != "GATE_NO_CHANNEL" || strings.Join(ops, ",") != "open" || len(sends) != 0 ||
				!strings.Contains(applied.Error.Next, "ha-nova setup confirm-phone") || !strings.Contains(applied.Error.Next, "ha-nova doctor") {
				t.Fatalf("exit %d %+v ops %v", exit, applied.Error, ops)
			}
		})
	}
}

// A move that ends without a link refuses, nothing sent, unless it expired,
// couldn't ask, or was paused where the first open offered the page: then
// the typed ask waits on the page, never on something weaker.
func TestGatePhoneMoveThatFails(t *testing.T) {
	paused := fakeRelayStatus{429, "GATE_PAUSED", "paused"}
	for _, c := range []struct {
		name        string
		end         string
		answer      any
		page        bool
		code        string
		typedAnswer string
	}{
		{name: "declined", end: "cancelled", page: true, code: "CONFIRM_DECLINED"},
		{name: "expired with the page", end: "expired", page: true},
		{name: "unavailable with the page", end: "unavailable", page: true},
		{name: "paused with the page", answer: paused, page: true},
		{name: "expired without the page", end: "expired", code: "CONFIRM_CANCELLED"},
		{name: "unavailable without the page", end: "unavailable", code: "CONFIRM_UNAVAILABLE"},
		{name: "paused without the page", answer: paused, code: "GATE_PAUSED"},
		{name: "lost answer, even with the page", end: "lost", page: true, code: "CONFIRM_UNAVAILABLE"},
		{name: "page declines the reopened ask", end: "expired", page: true, typedAnswer: "cancelled", code: "CONFIRM_DECLINED"},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := &fakeGate{phones: true, linkEnd: c.end, phoneAnswer: c.answer}
			if c.page {
				g.channels = []string{"page"}
			}
			if c.typedAnswer != "" {
				g.endFor = map[string]string{"ask2": c.typedAnswer}
			}
			h := gatedFamily(t, g, true)
			exit, applied := h.run(runApplyCommand, "--plan", h.plan("lock.unlock", "lock.front").Plan.ID)
			asks, ops, sends := g.snapshot()
			if c.code != "" {
				if exit != verbExitRefused || applied.Error == nil || applied.Error.Code != c.code || len(sends) != 0 {
					t.Fatalf("exit %d %+v ops %v sends %v", exit, applied.Error, ops, sends)
				}
				if c.typedAnswer == "" && (!strings.Contains(applied.Error.Message, "the phone link to HA NOVA wasn't confirmed") ||
					!strings.Contains(applied.Error.Next, "ha-nova setup confirm-phone") || !strings.Contains(applied.Error.Next, "ha-nova doctor") || strings.Contains(applied.Error.Next, "NOVA page")) {
					t.Fatalf("link words: %+v", applied.Error)
				}
				return
			}
			if exit != 0 || len(asks) != 2 || len(sends) != 1 || sends[0].grant != "grant-ask2" {
				t.Fatalf("exit %d %+v ops %v sends %v", exit, applied.Error, ops, sends)
			}
		})
	}
}

func TestSetupConfirmPhoneOnAGatedAppLinksThroughTheRelay(t *testing.T) {
	g := &fakeGate{phones: true}
	h := gatedFamily(t, g, false)
	p := usePhones(t)
	phoneDiscover = discoverPhone
	exit, out := runLink(t, h, "", phoneLinkRun{pick: "Anna’s Pixel"})
	if exit != 0 || !strings.Contains(out, "Linked “Anna’s Pixel”.") || len(g.links) != 1 || !reflect.DeepEqual(g.link, annaWire) || len(p.sent) != 0 {
		t.Fatalf("link: %d %q links %v sent %v", exit, out, g.links, p.sent)
	}
	if got := linked(t, h); got == nil || got.DeviceID != "dev-anna" || got.Platform != "android" {
		t.Fatalf("local entry %+v", got)
	}
	g.linkEnd = "cancelled" // the phone linked there now, or the new one, said no
	if exit, out := runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"}); exit != 1 || !strings.Contains(out, "Nothing linked: it was cancelled on a phone") || linked(t, h).DeviceID != "dev-anna" {
		t.Fatalf("declined relink: %d %q", exit, out)
	}
	g.linkEnd = ""
	if exit, out := runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"}); exit != 0 || g.link["device_id"] != "dev-markus" || g.link["platform"] != "ios" || linked(t, h).DeviceID != "dev-markus" || len(p.sent) != 0 {
		t.Fatalf("relink: %d %q relay %v", exit, out, g.link)
	}
	useWindow(t, windowConfirmed) // this computer can't hear the phone here (phoneDeaf)
	if exit, out := runUnlink(t, h); exit != 0 || !strings.Contains(out, phoneOffGated) || linked(t, h) != nil || g.link["device_id"] != "dev-markus" || len(p.sent) != 0 {
		t.Fatalf("off: %d %q", exit, out)
	}
}

func TestSetupConfirmPhoneWithoutTheGateStaysLocal(t *testing.T) {
	h, p, _ := familyLink(t)
	p.tapBy = tapFrom("mobile_app_annas_pixel", "anna-id", true)
	if exit, out := runLink(t, h, "", phoneLinkRun{pick: "Anna’s Pixel"}); exit != 0 || len(p.sent) == 0 || linked(t, h).DeviceID != "dev-anna" {
		t.Fatalf("local link: %d %q", exit, out)
	}
	if exit, out := runUnlink(t, h); exit != 0 || strings.Contains(out, phoneOffGated) {
		t.Fatalf("local off: %d %q", exit, out)
	}
}

func TestDoctorGateStates(t *testing.T) {
	on := []byte(`{"ok":true,"data":{"gate":1}}`)
	off := []byte(`{"ok":true,"data":{}}`)
	run := func(h *writeHarness, health []byte, link, device bool) (bool, string) {
		var ok bool
		_, out := captureCommandOutput(t, func() int {
			ok = doctorGate(h.paths, false, health, link, device, func(f string, a ...any) { printHumanInfo(f, a...) }) == 0
			return 0
		})
		return ok, out
	}
	h := gatedFamily(t, &fakeGate{}, true)
	if ok, out := run(h, on, false, true); !ok || !strings.Contains(out, "Safety check in Home Assistant: on.") || strings.Contains(out, "limits") || strings.Contains(out, "can't be replaced") {
		t.Fatalf("on: %v %q", ok, out)
	}
	if ok, out := run(h, off, false, true); !ok || !strings.Contains(out, "The NOVA App is too old for it") {
		t.Fatalf("too old: %v %q", ok, out)
	}
	if err := markGateSeen(h.paths, activeServerProfile()); err != nil {
		t.Fatal(err)
	}
	if ok, out := run(h, off, false, true); ok || !strings.Contains(out, "rolled back") {
		t.Fatalf("rolled back: %v %q", ok, out)
	}

	f := newFakeHA()
	home := newFamilyHome()
	home.install(t, f)
	family := f.extra
	registryFails := false
	f.extra = func(endpoint string, p map[string]any) (any, bool) {
		if p["type"] == "auth/current_user" && home.relay == "down" {
			return fakeRelayStatus{503, "GATE_UNAVAILABLE", "down"}, true
		}
		if p["type"] == "config/device_registry/list" && registryFails {
			return fakeRelayStatus{500, "UPSTREAM_ERROR", "busy"}, true
		}
		return family(endpoint, p)
	}
	h = gatedHarness(t, f, &fakeGate{})
	home.relay = "down"
	if ok, out := run(h, on, false, true); ok || !strings.Contains(out, "on, but not running") {
		t.Fatalf("worker down: %v %q", ok, out)
	}
	home.relay = "owner-id" // the owner's token, on a Link computer: one warning line
	ok, out := run(h, on, true, true)
	if lines := strings.Count(out, "Safety check limits:"); !ok || lines != 1 || !strings.Contains(out, "as the owner, so only the NOVA page can confirm") || !strings.Contains(out, "Home Assistant Link") {
		t.Fatalf("limits: %v %q", ok, out)
	}
	// The Relay on its own has no page: the owner's token leaves nothing that can confirm.
	if _, out := run(h, on, false, false); !strings.Contains(out, "nothing can confirm") || !strings.Contains(out, "a Home Assistant user who isn't the owner") || strings.Contains(out, "NOVA page can confirm") {
		t.Fatalf("owner token, standalone: %q", out)
	}
	registryFails = true // a lookup that fails after the owner was read: no "on", the owner check still runs
	if ok, out := run(h, on, false, true); !ok || strings.Contains(out, "Safety check in Home Assistant: on.") || !strings.Contains(out, "couldn't check that it runs") || !strings.Contains(out, "as the owner") {
		t.Fatalf("lookup failed: %v %q", ok, out)
	}
	registryFails = false
	home.relay = "supervisor-id"
	seedLink(t, h, &annaLink) // then off: the Relay keeps its link, and doctor can't read it
	useWindow(t, windowConfirmed)
	if exit, out := runUnlink(t, h); exit != 0 || linked(t, h) != nil {
		t.Fatalf("off: %d %q", exit, out)
	}
	if _, out := run(h, on, false, false); !strings.Contains(out, "No phone is linked for confirmations on this computer; your NOVA Relay may still have one") || !strings.Contains(out, "ha-nova setup confirm-phone") {
		t.Fatalf("no phone, standalone: %q", out)
	}
	if _, out := run(h, on, false, true); strings.Contains(out, "No phone is linked") {
		t.Fatalf("no phone, App: %q", out)
	}
	seedLink(t, h, &phoneLink{Service: "mobile_app_old", DeviceID: "dev-gone", UserID: "owner-id", Name: "Old Phone"})
	if _, out := run(h, on, false, false); !strings.Contains(out, "no longer in Home Assistant. Link another one: ha-nova setup confirm-phone") {
		t.Fatalf("gone phone, standalone: %q", out)
	}
	if _, out := run(h, on, false, true); strings.Contains(out, "no longer in Home Assistant") {
		t.Fatalf("gone phone, App: %q", out)
	}
}

// Through Link the phone route is a functional endpoint like the ask route.
func TestGatePhoneMoveOverLink(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	var sent map[string]any
	ingress := newProtocolTestCloudIngressClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		var p map[string]any
		_ = json.Unmarshal(raw, &p)
		mu.Lock()
		defer mu.Unlock()
		paths = append(paths, r.URL.Path)
		switch {
		case strings.HasSuffix(r.URL.Path, "/gate/v1/phone"):
			sent, _ = p["link"].(map[string]any)
			return relayAnswer(http.StatusOK, `{"ok":true,"data":{"needed":true,"ask_id":"link1","channels":["phone"]}}`), nil
		case strings.HasSuffix(r.URL.Path, "/gate/v1/ask") && p["op"] == "wait":
			return relayAnswer(http.StatusOK, `{"ok":true,"data":{"state":"linked"}}`), nil
		}
		return nil, errors.New("unexpected " + r.URL.Path)
	}))
	transport, err := newCloudRelayTransport(ingress, validCredential(41))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := &relaySession{transport: transport, ctx: ctx, cancel: cancel, slots: make(chan struct{}, relaySessionMaxConcurrent), memo: map[string]*relayMemo{}}
	state, err := s.gateMovePhone(phoneLink{Service: "mobile_app_annas_pixel", DeviceID: "dev-anna", UserID: "anna-id", Platform: "android"}, 5*time.Second)
	if err != nil || state != "linked" || !reflect.DeepEqual(sent, annaWire) || len(paths) != 2 {
		t.Fatalf("state %q err %v sent %v paths %v", state, err, sent, paths)
	}
}
