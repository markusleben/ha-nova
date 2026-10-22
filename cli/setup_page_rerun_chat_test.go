package main

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"reflect"
	"testing"
)

// The chat-app switch on a re-run (no owner sign-in yet): Done signs the
// owner in, as `ha-nova chat-apps on|off` does. Off goes through the
// command's own off; on asks the consent on the row afterwards, as step 3
// does, and nothing turns on before that click.

// rerunChat: a re-run whose NOVA App has chat apps; Home Assistant holds
// them exactly while the App says they are on. signIn nil: the owner signs in.
func rerunChat(t *testing.T, app *fakeChatApp, signIn error) (*setupPageTest, chan int, *bytes.Buffer, *int) {
	t.Helper()
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	stubRerunApp(t, true, chatAppsHA{}, nil)
	useChatApps(t, app, &fakeChatHA{}, chatTestLink)
	setupOwnerIngress, setupSignedInUser = nil, "" // a re-run: no sign-in this run
	chatAppsReadHA = func(runtimePaths) (chatAppsHA, error) {
		app.mu.Lock()
		on := app.state.Enabled
		app.mu.Unlock()
		return chatAppsHA{version: "2026.10.2", appVersion: "0.11.0", present: on, loaded: on, running: on}, nil
	}
	signIns := 0
	previousSignIn, previousOK, previousOrigin := chatAppsSignIn, chatAppsSignInOK, resolveHASignInOriginForSetup
	t.Cleanup(func() {
		chatAppsSignIn, chatAppsSignInOK, resolveHASignInOriginForSetup = previousSignIn, previousOK, previousOrigin
	})
	resolveHASignInOriginForSetup = func(context.Context, string) (*url.URL, error) { return url.Parse("http://homeassistant.local") }
	chatAppsSignInOK = func(io.Writer, runtimeConfig) bool { return true }
	chatAppsSignIn = func(io.Writer, string) (haSignInSession, error) {
		signIns++
		if signIn != nil {
			return haSignInSession{}, signIn
		}
		return haSignInSession{ingress: &CloudIngressClient{}, user: HACurrentUser{ID: "owner-id", IsOwner: true}}, nil
	}
	page, stdout, done := startSetupPageFlowWith(t, "", rerunFlow(paths, cfg, state, marker))
	return page, done, stdout, &signIns
}

func TestSetupPageRerunChatOffOnDone(t *testing.T) {
	app := &fakeChatApp{state: chatAppsState{Available: true, Enabled: true}}
	page, done, stdout, signIns := rerunChat(t, app, nil)
	rerun := page.waitFor("the re-run screen", accepting("finish"))
	connector := chatTestLink + chatAppsConnectorPath
	if chat := rerun.Control.Chat; chat == nil || chat.State != "page_on" || chat.URL != connector || chat.ClaudeLink != claudeConnectorLink(connector) || chat.OrgLink != claudeOrgConnectorLink(connector) || chat.Problem != nil {
		t.Fatalf("chat row = %+v", rerun.Control.Chat)
	}
	if *signIns != 0 || len(app.posted()) != 0 {
		t.Fatalf("before Done: sign-ins %d posted %v", *signIns, app.posted())
	}
	page.click("finish", "chat_off")
	code := setupPageExit(t, done, stdout)
	end := page.ui.snapshot()
	if code != 0 || end.Screen != "saved" || end.Home == nil || len(end.Home.Notes) != 1 || end.Home.Notes[0].Key != "setup.page.home.note_chat_off" {
		t.Fatalf("exit %d screen %s home %+v\n%s", code, end.Screen, end.Home, stdout)
	}
	if *signIns != 1 || !reflect.DeepEqual(app.posted(), []map[string]any{{"enabled": false}}) {
		t.Fatalf("sign-ins %d posted %v", *signIns, app.posted())
	}
}

func TestSetupPageRerunChatOnAsksAfterTheSignIn(t *testing.T) {
	app := &fakeChatApp{state: chatAppsState{Available: true}}
	page, done, stdout, signIns := rerunChat(t, app, nil)
	if chat := page.waitFor("the re-run screen", accepting("finish")).Control.Chat; chat == nil || chat.State != "page_off" {
		t.Fatalf("chat row = %+v", chat)
	}
	page.click("finish", "rule,chat_on")
	asked := page.waitFor("the consent", accepting("chat"))
	if chat := asked.Control.Chat; chat.State != "offer" || !chat.Ask || chat.NoRestart || *signIns != 1 || len(app.posted()) != 0 {
		t.Fatalf("after the sign-in: %+v sign-ins %d posted %v", chat, *signIns, app.posted())
	}
	if asked.Control.Rule != "offer" { // the next Done applies the other switches
		t.Fatalf("rule = %q", asked.Control.Rule)
	}
	page.click("chat", "on")
	on := page.waitFor("chat apps on", func(s setupPageState) bool {
		return s.Control != nil && s.Control.Chat != nil && s.Control.Chat.State == "on" && accepting("finish")(s)
	})
	if want := []map[string]any{{"enabled": true, "allow_restart": true}}; !reflect.DeepEqual(app.posted(), want) || on.Control.Chat.URL != chatTestLink+chatAppsConnectorPath {
		t.Fatalf("posted %v row %+v", app.posted(), on.Control.Chat)
	}
	page.click("finish", "")
	if code := setupPageExit(t, done, stdout); code != 0 || page.ui.snapshot().Screen != "saved" || *signIns != 1 {
		t.Fatalf("exit %d screen %s sign-ins %d\n%s", code, page.ui.snapshot().Screen, *signIns, stdout)
	}
}

func TestSetupPageRerunChatSignInFailsKeepsThemOff(t *testing.T) {
	app := &fakeChatApp{state: chatAppsState{Available: true}}
	page, done, stdout, signIns := rerunChat(t, app, errSignInNotOwner)
	page.waitFor("the re-run screen", accepting("finish"))
	page.click("finish", "chat_on")
	back := page.waitFor("the re-run screen again", accepting("finish"))
	if chat := back.Control.Chat; chat.State != "page_off" || chat.Problem == nil || chat.Problem.Key != "setup.chat.problem_signin" || chat.Attempt != 1 || *signIns != 1 || len(app.posted()) != 0 {
		t.Fatalf("chat row = %+v sign-ins %d posted %v", chat, *signIns, app.posted())
	}
	page.click("finish", "")
	setupPageExit(t, done, stdout)
}

func TestPageFinishChoices(t *testing.T) {
	for value, want := range map[string]pageFinish{
		"":              {},
		"rule,link":     {rule: true, link: true},
		"chat_on":       {chatOn: true},
		"rule,chat_off": {rule: true, chatOff: true},
	} {
		if got, ok := pageFinishChoices(value); !ok || got != want {
			t.Errorf("%q = %+v %v", value, got, ok)
		}
	}
	for _, value := range []string{"rule,rule", "chat", "chat_on,chat_off", "link,"} {
		if _, ok := pageFinishChoices(value); ok {
			t.Errorf("%q was taken", value)
		}
	}
}

// The connector note shows once, also when the row is read again.
func TestPageChatNoteOnce(t *testing.T) {
	f := &setupFlow{}
	f.pageChatNote("https://a.example/api/ha_nova/mcp")
	f.pageChatNote("https://b.example/api/ha_nova/mcp")
	if len(f.pageNotes) != 1 || f.pageNotes[0].Args["url"] != "https://b.example/api/ha_nova/mcp" {
		t.Fatalf("notes = %+v", f.pageNotes)
	}
}
