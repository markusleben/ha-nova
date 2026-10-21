package main

import (
	"slices"
	"sync"
	"testing"
)

// Chat apps off and nothing can reach them: the row links Home Assistant's
// own Link page on the saved local address, and Check again reads remote
// access again, so the switch shows once it is on.
func TestSetupPageChatRowChecksLinkAgain(t *testing.T) {
	app := &fakeChatApp{state: chatAppsState{Available: true}}
	useChatApps(t, app, &fakeChatHA{first: chatHAReady}, "")
	var mu sync.Mutex
	link := ""
	setupLinkRemoteOrigin = func(runtimeConfig) string { mu.Lock(); defer mu.Unlock(); return link }
	p := chatPage(t)
	p.flow.cfg.HAURL = "http://homeassistant.local:8123/"
	done := p.runControl()

	s := p.waitFor("step 3", accepting("chat_recheck"))
	if c := s.Control.Chat; c == nil || c.State != "noaddress" || c.LinkPage != "http://homeassistant.local:8123/config/cloud" {
		t.Fatalf("chat row %+v, want noaddress with the local Link page", c)
	}
	p.click("chat_recheck", "") // remote access still off: the same row
	s = p.waitFor("still off", accepting("chat_recheck"))
	if s.Control.Chat.State != "noaddress" {
		t.Fatalf("chat row %q after Check again with remote access off", s.Control.Chat.State)
	}

	mu.Lock()
	link = chatTestLink // the user turned remote access on
	mu.Unlock()
	p.click("chat_recheck", "")
	s = p.waitFor("the switch", accepting("chat"))
	if c := s.Control.Chat; c.State != "offer" || c.LinkPage != "" || slices.Contains(s.Accepts, "chat_recheck") {
		t.Fatalf("chat row %+v accepts %v, want the offer", c, s.Accepts)
	}
	p.click("finish", "")
	<-done
	if len(app.posted()) != 0 {
		t.Fatalf("Check again changed chat apps: %v", app.posted())
	}
}

// On, but nothing reaches them, is a problem: no Link page.
func TestSetupPageChatRowOnWithoutAddressHasNoRecheck(t *testing.T) {
	useChatApps(t, &fakeChatApp{state: chatAppsState{Available: true, Enabled: true}}, &fakeChatHA{first: chatHAReady}, "")
	p := chatPage(t)
	p.flow.cfg.HAURL = "http://homeassistant.local:8123"
	done := p.runControl()
	s := p.waitFor("step 3", accepting("finish"))
	if c := s.Control.Chat; c.State != "noaddress" || c.Problem == nil || c.LinkPage != "" || slices.Contains(s.Accepts, "chat_recheck") {
		t.Fatalf("chat row %+v accepts %v", c, s.Accepts)
	}
	p.click("finish", "")
	<-done
}

// A re-run (no owner sign-in): Check again reads the row as the re-run
// does, so chat apps off with remote access on now become the switch.
func TestSetupPageRerunChatRowChecksLinkAgain(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	stubRerunApp(t, true, chatAppsHA{version: "2026.10.1", appVersion: "0.11.0"}, nil)
	var mu sync.Mutex
	link := ""
	setupLinkRemoteOrigin = func(runtimeConfig) string { mu.Lock(); defer mu.Unlock(); return link }
	page, stdout, done := startSetupPageFlowWith(t, "", rerunFlow(paths, cfg, state, marker))

	s := page.waitFor("the re-run screen", accepting("chat_recheck"))
	if c := s.Control.Chat; c == nil || c.State != "noaddress" || c.LinkPage == "" {
		t.Fatalf("chat row %+v, want noaddress with the Link page", c)
	}
	mu.Lock()
	link = chatTestLink
	mu.Unlock()
	page.click("chat_recheck", "")
	s = page.waitFor("the switch", func(s setupPageState) bool {
		return s.Control != nil && s.Control.Chat != nil && s.Control.Chat.State == "page_off" && slices.Contains(s.Accepts, "finish")
	})
	if slices.Contains(s.Accepts, "chat_recheck") || s.Control.Chat.LinkPage != "" {
		t.Fatalf("accepts %v, chat row %+v", s.Accepts, s.Control.Chat)
	}
	page.click("finish", "")
	if code := setupPageExit(t, done, stdout); code != 0 {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
}
