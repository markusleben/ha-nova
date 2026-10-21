package main

import (
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

	s := p.waitFor("step 3", accepting("recheck"))
	if c := s.Control.Chat; c == nil || c.State != "noaddress" || c.LinkPage != "http://homeassistant.local:8123/config/cloud" {
		t.Fatalf("chat row %+v, want noaddress with the local Link page", c)
	}
	p.click("recheck", "chat") // remote access still off: the same row
	s = p.waitFor("still off", accepting("recheck"))
	if s.Control.Chat.State != "noaddress" {
		t.Fatalf("chat row %q after Check again with remote access off", s.Control.Chat.State)
	}

	mu.Lock()
	link = chatTestLink // the user turned remote access on
	mu.Unlock()
	p.click("recheck", "chat")
	s = p.waitFor("the switch", accepting("chat"))
	if c := s.Control.Chat; c.State != "offer" || c.LinkPage != "" {
		t.Fatalf("chat row %+v, want the offer", c)
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
	if c := s.Control.Chat; c.State != "noaddress" || c.Problem == nil || c.LinkPage != "" {
		t.Fatalf("chat row %+v", c)
	}
	p.click("finish", "")
	<-done
}
