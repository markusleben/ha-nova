package main

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// The setup page's user stories (the 2026-10-20 review): Claude or ChatGPT
// in the browser, a new phone on a re-run, and why the terminal waits.

// No AI app on this computer: "I use Claude or ChatGPT in the browser" goes
// on as the terminal's no-app path. One sign-in pairs this computer, nothing
// is taught, and the page ends with step 4.
func TestSetupPageGoesOnForClaudeOrChatGPTInTheBrowser(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{})
	t.Cleanup(clearSetupNextPromptSkipsStaleBlankInput) // the sign-in arms it; no terminal prompt follows here
	setupGoldenHome(t)
	ha := newFakeSignInHA(t)
	pairs := stubSetupPageHome(t, ha.server.URL, true)
	page, stdout, done := startSetupPageFlow(t, "")

	none := page.waitFor("no AI app", accepting("web"))
	if none.Screen != "noclient" || !slices.Contains(none.Accepts, "recheck") {
		t.Fatalf("no AI app = %+v", none)
	}
	page.click("web", "")
	found := page.waitFor("the found screen", accepting("signin"))
	if found.Screen != "found" || !found.Web || len(found.Clients) != 0 || found.Primary != "signin" {
		t.Fatalf("found = %+v", found)
	}
	page.click("signin", "")
	waiting := page.waitFor("the sign-in URL", func(s setupPageState) bool { return s.SignInURL != "" })
	if slices.ContainsFunc(waiting.Rows, func(r setupPageRow) bool { return r.ID == "teach" }) {
		t.Fatalf("rows teach an AI app that isn't here: %s", pageRows(waiting))
	}
	authorize, err := url.Parse(waiting.SignInURL)
	if err != nil {
		t.Fatal(err)
	}
	callback := authorize.Query().Get("redirect_uri") + "?" + signedInQuery(authorize.Query().Get("state"))
	page.do(http.MethodGet, strings.TrimPrefix(callback, page.base), "", nil)

	control := page.waitFor("step 3", accepting("finish"))
	if control.Screen != "control" || !control.Web || control.Control.Rule != "" {
		t.Fatalf("step 3 = %+v %+v", control, control.Control)
	}
	page.click("finish", "")
	page.waitFor("step 4", accepting("done"))
	page.click("done", "")
	if code := setupPageExit(t, done, stdout); code != 0 || page.ui.snapshot().Screen != "closed" || *pairs != 1 {
		t.Fatalf("exit %d screen %s pairs %d\n%s", code, page.ui.snapshot().Screen, *pairs, stdout)
	}
	out := stdout.String()
	if !strings.Contains(out, setupT("setup.done.connected_no_client")) || strings.Contains(out, setupT("setup.done.complete")) {
		t.Fatalf("terminal says an AI app was set up:\n%s", out)
	}
	paths, _ := detectPaths()
	if installed := loadStateOrDefault(paths).InstalledClients; len(installed) != 0 {
		t.Fatalf("installed into %v", installed)
	}
}

// A re-run with no AI app here goes on as the browser path, without the
// "No AI app" screen.
func TestSetupPageRerunWithoutAnAIAppSkipsTheQuestion(t *testing.T) {
	page := startSetupPageTest(t)
	page.launch()
	flow := &setupFlow{page: page.ui, ui: page.ui, cfg: applySelectedSetupHost(runtimeConfig{}, "ha.local", "http://ha.local:8123", "")}
	if result := flow.pageWaitForClient(); result.done || !flow.noClient || !page.ui.snapshot().Web || page.ui.snapshot().Screen == "noclient" {
		t.Fatalf("result %+v noClient=%v state %+v", result, flow.noClient, page.ui.snapshot())
	}
}

// After a move, chat apps that are on keep their own phone, and say so;
// still off, the question names the phone linked now.
func TestSetupPageChatAfterAPhoneMove(t *testing.T) {
	f := &setupFlow{}
	on := &setupPageChat{State: "page_on"}
	f.pageChatMoved(on, &chatAppsOffer{})
	if on.Problem == nil || on.Problem.Key != "setup.chat.phone_kept" {
		t.Fatalf("on: %+v", on)
	}
	f.pageChatMoved(nil, &chatAppsOffer{}) // no row
}

// The browser user gets step 3's chat-app question open; nothing turns on
// before its own button. Others get it closed, as before.
func TestSetupPageOpensTheChatQuestionForTheBrowserOnly(t *testing.T) {
	for _, web := range []bool{true, false} {
		app := &fakeChatApp{state: chatAppsState{Available: true}}
		useChatApps(t, app, &fakeChatHA{first: chatHAReady}, chatTestLink)
		p := chatPage(t)
		p.flow.noClient = web
		done := p.runControl()
		s := p.waitFor("step 3", accepting("chat"))
		if c := s.Control.Chat; c.State != "offer" || c.Ask != web {
			t.Fatalf("web=%v: chat row %+v", web, c)
		}
		p.click("finish", "")
		<-done
		if len(app.posted()) != 0 {
			t.Fatalf("web=%v: chat apps changed without a click: %v", web, app.posted())
		}
	}
}

// "Use another phone" keeps `confirm-phone`'s move rule: the click on the
// page is no consent; the phone linked now OKs the move, then the tap on
// the new phone links it. Declined there: nothing changes, and the new
// phone is never asked.
func TestSetupPageMovesThePhoneOnlyWithTheLinkedPhonesOK(t *testing.T) {
	h, phones, _ := familyLink(t)
	seedLink(t, h, &annaLink)
	phones.tapBy = tapFrom("mobile_app_annas_pixel", "anna-id", false)
	p := newPageStepsTest(t, h.paths, "codex")
	p.flow.pairedThisRun = true
	done := p.runControl()
	ctl := p.waitFor("step 3", accepting("phone")).Control
	if ctl.Phone != "linked" || ctl.PhoneName != "Anna’s Pixel" || len(ctl.Phones) != 2 ||
		slices.ContainsFunc(ctl.Phones, func(ph setupPagePhone) bool { return ph.ID == "dev-anna" }) || len(phones.sent) != 0 {
		t.Fatalf("control = %+v sent %v", ctl, phones.services)
	}
	p.click("phone", "dev-kid") // not listed: nothing is asked
	p.waitFor("the page again", accepting("phone"))
	if len(phones.sent) != 0 {
		t.Fatalf("an unlisted phone was asked: %v", phones.services)
	}

	p.click("phone", "dev-markus")
	ctl = p.waitFor("the refusal", func(s setupPageState) bool { return s.Control.PhoneProblem != nil }).Control
	if ctl.Phone != "linked" || ctl.PhoneName != "Anna’s Pixel" || ctl.PhoneProblem.Key != "setup.page.control.move_refused" ||
		ctl.PhoneProblem.Args["phone"] != "Anna’s Pixel" || linked(t, h).DeviceID != "dev-anna" ||
		slices.Contains(phones.services, "mobile_app_markuss_iphone") {
		t.Fatalf("declined: control %+v link %+v notified %v", ctl, linked(t, h), phones.services)
	}

	phones.services, phones.sent = nil, nil
	phones.tapBy = func(service, ok, no string) []phoneEvent {
		switch service {
		case "mobile_app_annas_pixel":
			return []phoneEvent{tap(ok, "anna-id", "REMOTE")}
		case "mobile_app_markuss_iphone":
			return []phoneEvent{tap(ok, "owner-id", "REMOTE")}
		}
		return nil
	}
	p.click("phone", "dev-markus")
	ctl = p.waitFor("the move", func(s setupPageState) bool {
		return s.Control.Phone == "linked" && s.Control.PhoneName == "Markus’s iPhone" && slices.Contains(s.Accepts, "phone")
	}).Control
	if l := linked(t, h); l == nil || l.DeviceID != "dev-markus" || ctl.PhoneProblem != nil ||
		!slices.ContainsFunc(ctl.Phones, func(ph setupPagePhone) bool { return ph.ID == "dev-anna" }) {
		t.Fatalf("moved: control %+v link %+v", ctl, l)
	}
	if phones.services[0] != "mobile_app_annas_pixel" || !slices.Contains(phones.services, "mobile_app_markuss_iphone") {
		t.Fatalf("the linked phone wasn't asked first: %v", phones.services)
	}
	p.click("finish", "")
	if code := <-done; code != 0 {
		t.Fatalf("exit %d", code)
	}
}

// The terminal screen says why it waits: the code, or the keyring; the
// first reason stays.
func TestSetupPageHandOverSaysWhy(t *testing.T) {
	ui := newSetupWebUI(strings.NewReader(""), &strings.Builder{})
	ui.handoverFor("code")
	ui.handoverFor("keyring_locked")
	if s := ui.snapshot(); s.Screen != "terminal" || s.Handover != "code" {
		t.Fatalf("hand-over = %+v", s)
	}
	for err, want := range map[error]string{
		errDesktopKeyringLocked:                 "keyring_locked",
		errDesktopKeyringInitializationRequired: "keyring_setup",
		errors.New("disk full"):                 "",
	} {
		if got := secureStorageHandover(err); got != want {
			t.Errorf("%v: %q, want %q", err, got, want)
		}
	}
}
