package main

import (
	"io"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// The gated phone link, after review (gate_phone_move.go): one move per
// session, a link wait that leaves the typed ask its share, the link phase
// in ha-nova mcp, the setup page through the Relay, and the battery
// reminder on a gated App.

// A move that ended without a link isn't tried again in the session: a
// later 428 asks on the page alone, without paging the phone again.
func TestGatePhoneMovesOncePerSession(t *testing.T) {
	g := &fakeGate{phones: true, channels: []string{"page"}, linkEnd: "expired", hold: func(send gateSend) any {
		if strings.Contains(send.key, "/api/config/script/config/s2") && send.grant == "grant-ask2" {
			return fakeRelayStatus{428, "GATE_APPROVAL_REQUIRED", "grant expired"}
		}
		return nil
	}}
	f := batchHome()
	newFamilyHome().install(t, f)
	h := gatedHarness(t, f, g)
	seedLink(t, h, &annaLink)
	unlock := func(alias string) map[string]any {
		return map[string]any{"alias": alias, "sequence": []any{step("lock.unlock", "lock.front")}}
	}
	_, env := h.batch(h.writePlan("script", "s1", unlock("Arrival")), h.writePlan("script", "s2", unlock("Old arrival")))
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	asks, ops, sends := g.snapshot()
	var grants []string
	for _, send := range sends {
		grants = append(grants, send.grant)
	}
	if exit != 0 || applied.Status != "verified" || len(asks) != 3 || len(g.links) != 1 || strings.Join(grants, ",") != "grant-ask2,grant-ask2,grant-ask3" {
		t.Fatalf("exit %d %+v; ops %v links %d grants %v", exit, applied.Error, ops, len(g.links), grants)
	}
}

// An unanswered link (the CLI's own wait ends: "timeout") waits half of
// what is left of the command's wait; with the page on offer the typed ask
// is opened again and the page approves it within the command's budget.
// Without the page it refuses in the link's own words.
func TestGateUnansweredLinkLeavesTheAskItsShare(t *testing.T) {
	for _, page := range []bool{true, false} {
		t.Run(map[bool]string{true: "page", false: "no page"}[page], func(t *testing.T) {
			g := &fakeGate{phones: true, linkEnd: "waiting"}
			if page {
				g.channels = []string{"page"}
			}
			h := gatedFamily(t, g, true)
			plan := h.plan("lock.unlock", "lock.front").Plan
			savedWait, savedStart := windowWait, commandStart
			t.Cleanup(func() { windowWait, commandStart = savedWait, savedStart })
			windowWait, commandStart = 4*time.Second, time.Now()
			exit, applied := h.run(runApplyCommand, "--plan", plan.ID)
			_, ops, sends := g.snapshot()
			if !page {
				if exit != verbExitRefused || applied.Error.Code != "CONFIRM_CANCELLED" || !strings.Contains(applied.Error.Message, "the phone link to HA NOVA wasn't confirmed") || len(sends) != 0 {
					t.Fatalf("exit %d %+v ops %v", exit, applied.Error, ops)
				}
				return
			}
			if exit != 0 || applied.Status != "verified" || len(g.links) != 1 || !slices.Contains(ops, "cancel") || len(sends) != 1 || sends[0].grant != "grant-ask2" ||
				time.Since(commandStart) > windowWait {
				t.Fatalf("exit %d %+v after %v; ops %v sends %+v", exit, applied.Error, time.Since(commandStart), ops, sends)
			}
		})
	}
}

// While ha-nova mcp's job waits for the link, its envelope says the phone
// asks to link HA NOVA first; the status stays waiting_for_approval.
func TestGateMCPApplyNamesTheLinkPhase(t *testing.T) {
	shortApplyWait(t)
	g := &fakeGate{phones: true, channels: []string{"page"}, linkEnd: "waiting"}
	h := gatedFamily(t, g, true)
	plan := h.plan("lock.unlock", "lock.front").Plan
	c := startMCP(t, h.paths)
	mcpInit(c)
	apply := map[string]any{"plan_id": plan.ID}
	var env map[string]any
	waitFor(t, "the link ask", func() bool {
		env = envelopeOf(c.tool("ha_apply", apply))
		return env["status"] == "waiting_for_approval"
	})
	if env["say"] != confirmT("en", "say.relay_link_waiting") || !strings.Contains(nextOf(env), "first asks whether it gets HA NOVA's confirmations") ||
		!strings.Contains(nextOf(env), "call ha_apply again with plan_id "+plan.ID) {
		t.Fatalf("link phase: %v", env)
	}
	g.mu.Lock()
	g.linkEnd = ""
	g.mu.Unlock()
	waitFor(t, "the outcome", func() bool {
		env = envelopeOf(c.tool("ha_apply", apply))
		return env["status"] != "applying" && env["status"] != "waiting_for_approval"
	})
	if asks, _, sends := g.snapshot(); env["status"] != "verified" || len(asks) != 2 || len(sends) != 1 || sends[0].grant != "grant-ask2" {
		t.Fatalf("outcome %v; asks %q sends %+v", env, asks, sends)
	}
}

// The setup page links and moves the phone through the Relay on a gated
// App: no local notification, and no window before a move.
func TestSetupPageLinksThroughTheRelayOnAGatedApp(t *testing.T) {
	g := &fakeGate{phones: true}
	h := gatedFamily(t, g, false)
	phones := usePhones(t)
	phoneDiscover = discoverPhone
	w := useWindow(t)
	saved := commandStart // the page waited long: the command's own wait is spent, the link's isn't
	t.Cleanup(func() { commandStart = saved })
	commandStart = time.Now().Add(-10 * time.Minute)
	p := newPageStepsTest(t, h.paths, "codex")
	ctl := setupPageControl{PhoneButtons: &phoneButtons{Allow: phoneLinkButton("en"), Move: confirmT("en", "phone.move.button"), lang: "en"}}
	p.flow.pageLinkPhone(&ctl, "dev-anna")
	if ctl.Phone != "linked" || !reflect.DeepEqual(g.link, annaWire) || linked(t, h) == nil || linked(t, h).DeviceID != "dev-anna" || len(phones.sent) != 0 {
		t.Fatalf("link: %+v relay %v", ctl, g.link)
	}
	ctl.PhoneName = "Anna’s Pixel"
	if !p.flow.pageMovePhone(&ctl, "dev-markus") || g.link["device_id"] != "dev-markus" || linked(t, h).DeviceID != "dev-markus" || len(w.messages) != 0 || len(phones.sent) != 0 {
		t.Fatalf("move: %+v relay %v windows %q", ctl, g.link, w.messages)
	}
}

// Under ha-nova mcp one ask life covers the link and the reopened ask: an
// unanswered link waits half, and the page approves the change in the rest.
func TestGateMCPLinkAndAskShareOneLife(t *testing.T) {
	shortApplyWait(t)
	saved := gateAskLife
	t.Cleanup(func() { gateAskLife = saved })
	gateAskLife = time.Second // with the 5 s margin: 6 s in all, the link 3 s
	g := &fakeGate{phones: true, channels: []string{"page"}, linkEnd: "waiting"}
	h := gatedFamily(t, g, true)
	plan := h.plan("lock.unlock", "lock.front").Plan
	c := startMCP(t, h.paths)
	mcpInit(c)
	start, apply := time.Now(), map[string]any{"plan_id": plan.ID}
	var env map[string]any
	for env = envelopeOf(c.tool("ha_apply", apply)); env["status"] == "applying" || env["status"] == "waiting_for_approval"; env = envelopeOf(c.tool("ha_apply", apply)) {
		if time.Since(start) > 5*time.Second {
			t.Fatalf("still %v after %v: the link took the whole life", env["status"], time.Since(start))
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, ops, sends := g.snapshot(); env["status"] != "verified" || !slices.Contains(ops, "cancel") || len(sends) != 1 || sends[0].grant != "grant-ask2" {
		t.Fatalf("outcome %v; ops %v sends %+v", env, ops, sends)
	}
}

// The move's phone lookup has a read budget of its own: a session whose
// budget preparing the ask spent still finds the linked phone.
func TestGateMoveLooksUpThePhoneOnItsOwnBudget(t *testing.T) {
	h := gatedFamily(t, &fakeGate{phones: true}, true)
	previous := relaySessionOverallTimeout
	relaySessionOverallTimeout = 300 * time.Millisecond
	t.Cleanup(func() { relaySessionOverallTimeout = previous })
	s, err := newRelaySession(h.paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	time.Sleep(400 * time.Millisecond)
	if link, ok := s.gateLocalPhone(); !ok || link.DeviceID != "dev-anna" {
		t.Fatalf("lookup after the session's budget: %+v %v", link, ok)
	}
}

// A link answer that never arrives leaves the outcome unknown: setup says
// so and keeps this computer's setting; confirm-phone again sets both right.
func TestSetupConfirmPhoneLostAnswerIsUnknown(t *testing.T) {
	g := &fakeGate{phones: true, linkEnd: "lost"}
	h := gatedFamily(t, g, true)
	usePhones(t)
	phoneDiscover = discoverPhone
	exit, out := runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"})
	if exit != 1 || !strings.Contains(out, "can't tell whether “Markus’s iPhone” is linked") || strings.Contains(out, "Nothing linked") || linked(t, h) == nil || *linked(t, h) != annaLink {
		t.Fatalf("lost: %d %q local %+v", exit, out, linked(t, h))
	}
}

// A battery reminder only notifies the linked phone: a gated App's phone
// counts there, though it never confirms from this computer.
func TestBatteryReminderFindsTheGatedAppsPhone(t *testing.T) {
	h := gatedFamily(t, &fakeGate{}, true)
	usePhones(t)
	phoneDiscover = discoverPhone
	s, err := newRelaySession(h.paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if phoneLinkedHere(s) {
		t.Fatal("a gated App's phone confirmed from this computer")
	}
	if phone := linkedPhone(s); phone == nil || phone.Service != "mobile_app_annas_pixel" {
		t.Fatalf("reminder phone %+v", phone)
	}
}

// A read finds the phone of its own call, never of the chat app a change
// running beside it set (chatCall): ha_map runs without mcpVerbLock.
func TestBatteryReminderReadsItsOwnCallsLinks(t *testing.T) {
	h := gatedFamily(t, &fakeGate{}, true)
	usePhones(t)
	phoneDiscover = discoverPhone
	defer func() { chatCall = nil }()
	chatCall = &chatPolicy{user: "cccccccccccccccccccccccccccccccc", links: filepath.Join(t.TempDir(), "phone-links.json")} // another call's chat app, without a link
	s, err := newRelaySession(h.paths, verbOptions{call: &verbCall{out: io.Discard, err: io.Discard}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if phone := linkedPhone(s); phone == nil || phone.Service != "mobile_app_annas_pixel" {
		t.Fatalf("reminder phone %+v", phone)
	}
}
