package main

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Chat apps from the computer (chat_apps.go): setup's step 3 row and its
// consent, the turn-on through Home Assistant's restart, the text path's
// question, and `ha-nova chat-apps`. The App and Home Assistant are fakes
// behind the seams; the ingress request itself runs against httptest.

const chatTestLink = "https://abc123.ui.nabu.casa"

type fakeChatApp struct {
	mu       sync.Mutex
	state    chatAppsState
	getErr   error
	setErr   error
	restart  bool
	phoneErr error // only for an owner_phone request
	bodies   []map[string]any
}

func (a *fakeChatApp) posted() []map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.bodies)
}

type chatHARead struct {
	ha  chatAppsHA
	err error
}

// fakeChatHA answers the first read (the offer) with first, every later one
// with what the test sends on next (or first again when next is nil).
type fakeChatHA struct {
	first chatHARead
	next  chan chatHARead
}

func useChatApps(t *testing.T, app *fakeChatApp, ha *fakeChatHA, link string) {
	t.Helper()
	call, read, origin, wait, every, session, user := chatAppsCall, chatAppsReadHA, setupLinkRemoteOrigin, chatAppsReadyWait, chatAppsPollEvery, setupOwnerIngress, setupSignedInUser
	t.Cleanup(func() {
		chatAppsCall, chatAppsReadHA, setupLinkRemoteOrigin, chatAppsReadyWait, chatAppsPollEvery, setupOwnerIngress, setupSignedInUser = call, read, origin, wait, every, session, user
	})
	setupOwnerIngress, setupSignedInUser = &CloudIngressClient{}, "owner-id"
	chatAppsReadyWait, chatAppsPollEvery = 5*time.Second, time.Millisecond
	setupLinkRemoteOrigin = func(runtimeConfig) string { return link }
	chatAppsCall = func(_ context.Context, client *CloudIngressClient, body map[string]any) (chatAppsState, error) {
		if client == nil {
			t.Error("chat apps called without the owner's session")
		}
		app.mu.Lock()
		defer app.mu.Unlock()
		if body == nil {
			return app.state, app.getErr
		}
		app.bodies = append(app.bodies, body)
		if _, phone := body["owner_phone"]; phone && app.phoneErr != nil {
			return chatAppsState{}, app.phoneErr
		}
		if app.setErr != nil {
			return chatAppsState{}, app.setErr
		}
		if enabled, set := body["enabled"].(bool); set {
			app.state.Enabled = enabled
		}
		answer := app.state
		answer.Restarting = app.restart && body["enabled"] == true
		return answer, nil
	}
	reads := 0
	chatAppsReadHA = func(runtimePaths) (chatAppsHA, error) {
		reads++
		if reads == 1 || ha.next == nil {
			return ha.first.ha, ha.first.err
		}
		r := <-ha.next
		return r.ha, r.err
	}
}

var chatHAReady = chatHARead{ha: chatAppsHA{version: "2026.10.2"}}

// chatPage is step 3 right after this run's sign-in pairing.
func chatPage(t *testing.T) *pageStepsTest {
	t.Helper()
	p := newPageStepsTest(t, chatPaths(t), "codex")
	p.flow.pairedThisRun = true
	return p
}

func chatPaths(t *testing.T) runtimePaths {
	t.Helper()
	resetServerProfileSelection(t)
	return writeTestConfigFile(t, `{"schema_version":1}`)
}

// The row shows only when the App has chat apps, Link remote access answers,
// and Home Assistant is 2026.10 or newer; nothing is sent before a click.
func TestSetupPageChatRowOnlyWhenItCanWork(t *testing.T) {
	available := chatAppsState{Available: true}
	for name, test := range map[string]struct {
		app         chatAppsState
		get         error
		ha          chatHARead
		link        string
		alone       bool // no owner session this run
		codePairing bool // paired with a code after another home's sign-in: no row
		want        string
	}{
		"offered":         {app: available, ha: chatHAReady, link: chatTestLink, want: "offer"},
		"already on":      {app: chatAppsState{Available: true, Enabled: true}, ha: chatHAReady, link: chatTestLink, want: "on"},
		"dormant App":     {get: errChatAppsUnsupported, ha: chatHAReady, link: chatTestLink},
		"App without":     {app: chatAppsState{Available: false}, ha: chatHAReady, link: chatTestLink},
		"no Link":         {app: available, ha: chatHAReady},
		"old HA":          {app: available, ha: chatHARead{ha: chatAppsHA{version: "2026.9.4"}}, link: chatTestLink},
		"HA unreadable":   {app: available, ha: chatHARead{err: errSetupExit}, link: chatTestLink},
		"no sign-in here": {app: available, ha: chatHAReady, link: chatTestLink, alone: true},
		"no pairing now":  {app: available, ha: chatHAReady, link: chatTestLink, codePairing: true},
	} {
		t.Run(name, func(t *testing.T) {
			app := &fakeChatApp{state: test.app, getErr: test.get}
			useChatApps(t, app, &fakeChatHA{first: test.ha}, test.link)
			if test.alone {
				setupOwnerIngress = nil
			}
			p := chatPage(t)
			p.flow.pairedThisRun = !test.codePairing
			done := p.runControl()
			s := p.waitFor("step 3", accepting("finish"))
			got := ""
			if s.Control.Chat != nil {
				got = s.Control.Chat.State
			}
			if got != test.want || slices.Contains(s.Accepts, "chat") != (test.want == "offer") {
				t.Fatalf("chat row %q accepts %v, want %q", got, s.Accepts, test.want)
			}
			if test.want == "on" && s.Control.Chat.URL != chatTestLink+"/api/ha_nova/mcp" {
				t.Fatalf("connector %q", s.Control.Chat.URL)
			}
			p.click("finish", "")
			<-done
			if len(app.posted()) != 0 {
				t.Fatalf("step 3 changed chat apps without a click: %v", app.posted())
			}
		})
	}
}

// The click on "Turn on and restart" is the consent: only then the App is
// asked, with allow_restart. The page shows the restart and Home Assistant
// loading the integration, then the connector address; step 4 waits.
func TestSetupPageChatTurnsOnThroughTheRestart(t *testing.T) {
	app := &fakeChatApp{state: chatAppsState{Available: true}, restart: true}
	ha := &fakeChatHA{first: chatHAReady, next: make(chan chatHARead)}
	useChatApps(t, app, ha, chatTestLink)
	p := chatPage(t)
	done := p.runControl()
	p.waitFor("step 3", accepting("chat"))
	p.click("chat", "off") // never sent by the page: nothing happens
	p.waitFor("step 3 again", accepting("chat"))
	if len(app.posted()) != 0 {
		t.Fatalf("a non-consent value turned chat apps on: %v", app.posted())
	}

	p.click("chat", "on")
	rows := func(s setupPageState) string {
		var out []string
		for _, r := range s.Control.Chat.Rows {
			out = append(out, r.ID+"="+r.State)
		}
		return strings.Join(out, ",")
	}
	s := p.waitFor("the restart", func(s setupPageState) bool {
		return s.Control.Chat != nil && rows(s) == "chat_on=ok,chat_restart=run,chat_ready=todo"
	})
	if s.Control.Chat.State != "run" || slices.Contains(s.Accepts, "finish") {
		t.Fatalf("during the restart: %+v accepts %v", s.Control.Chat, s.Accepts)
	}
	if want := []map[string]any{{"enabled": true, "allow_restart": true}}; !reflect.DeepEqual(app.posted(), want) {
		t.Fatalf("posted %v, want %v", app.posted(), want)
	}
	ha.next <- chatHARead{err: errSetupExit} // Home Assistant is down
	ha.next <- chatHARead{err: errSetupExit}
	ha.next <- chatHAReady // back, not loaded yet
	p.waitFor("Home Assistant back", func(s setupPageState) bool {
		return rows(s) == "chat_on=ok,chat_restart=ok,chat_ready=run"
	})
	ha.next <- chatHARead{ha: chatAppsHA{version: "2026.10.2", loaded: true}}
	s = p.waitFor("on", accepting("finish"))
	c := s.Control.Chat
	if c.State != "on" || c.URL != chatTestLink+"/api/ha_nova/mcp" || c.Problem != nil || rows(s) != "chat_on=ok,chat_restart=ok,chat_ready=ok" || slices.Contains(s.Accepts, "chat") {
		t.Fatalf("after the restart: %+v accepts %v", c, s.Accepts)
	}
	p.click("finish", "")
	<-done
	if n := p.flow.pageNotes; len(n) != 1 || n[0].Key != "setup.page.home.note_chat" || n[0].Args["url"] != c.URL {
		t.Fatalf("notes %+v", n)
	}
}

// A refusal keeps the switch with one plain line, and trying again runs the
// whole action; an expired sign-in sends the owner to the NOVA page; Home
// Assistant that never loads it says so and still shows the address.
func TestSetupPageChatFailures(t *testing.T) {
	t.Run("refused, then again", func(t *testing.T) {
		app := &fakeChatApp{state: chatAppsState{Available: true}, setErr: &chatAppsRefused{status: 409, code: "HA_CONFIG_INVALID"}}
		useChatApps(t, app, &fakeChatHA{first: chatHARead{ha: chatAppsHA{version: "2026.10.2", loaded: true}}}, chatTestLink)
		p := chatPage(t)
		done := p.runControl()
		p.waitFor("step 3", accepting("chat"))
		p.click("chat", "on")
		s := p.waitFor("the problem", func(s setupPageState) bool {
			return s.Control.Chat.Problem != nil && slices.Contains(s.Accepts, "chat")
		})
		if c := s.Control.Chat; c.State != "offer" || c.Problem.Key != "setup.chat.problem_config" || c.URL != "" || len(c.Rows) != 0 {
			t.Fatalf("after a refusal: %+v", c)
		}
		app.mu.Lock()
		app.setErr = nil
		app.mu.Unlock()
		p.click("chat", "on")
		if s = p.waitFor("on", func(s setupPageState) bool { return s.Control.Chat.State == "on" }); s.Control.Chat.Problem != nil {
			t.Fatalf("the retry: %+v", s.Control.Chat)
		}
		p.click("finish", "")
		<-done
	})
	t.Run("sign-in expired", func(t *testing.T) {
		app := &fakeChatApp{state: chatAppsState{Available: true}, setErr: newCloudHTTPError(CloudErrOuterSessionExpired, "authorize Cloud ingress session", 401, true)}
		useChatApps(t, app, &fakeChatHA{first: chatHAReady}, chatTestLink)
		p := chatPage(t)
		done := p.runControl()
		p.waitFor("step 3", accepting("chat"))
		p.click("chat", "on")
		s := p.waitFor("expired", func(s setupPageState) bool { return s.Control.Chat.State == "expired" })
		if s.Control.Chat.Problem.Key != "setup.chat.problem_expired" || slices.Contains(s.Accepts, "chat") {
			t.Fatalf("expired: %+v accepts %v", s.Control.Chat, s.Accepts)
		}
		p.click("finish", "")
		<-done
	})
	t.Run("never loads", func(t *testing.T) {
		app := &fakeChatApp{state: chatAppsState{Available: true}, restart: true}
		useChatApps(t, app, &fakeChatHA{first: chatHAReady}, chatTestLink)
		chatAppsReadyWait = 30 * time.Millisecond
		p := chatPage(t)
		done := p.runControl()
		p.waitFor("step 3", accepting("chat"))
		p.click("chat", "on")
		s := p.waitFor("on", accepting("finish"))
		if c := s.Control.Chat; c.State != "on" || c.Problem == nil || c.Problem.Key != "setup.chat.problem_slow" || c.URL == "" || c.Rows[len(c.Rows)-1].State != "fail" {
			t.Fatalf("slow: %+v", c)
		}
		p.click("finish", "")
		<-done
	})
}

// The owner's phone linked on this computer becomes chat apps' phone too,
// so the owner taps once (spec §11.4); never another user's link, and never
// over a phone the App has already.
func TestChatAppsSendsTheOwnersPhoneOnly(t *testing.T) {
	link := func(user string) string {
		return `{"schema_version":1,"confirm_phone":{"owner-id":{"service":"mobile_app_markus_iphone","device_id":"0123456789abcdef0123456789abcdef","user_id":"` + user + `","name":"Markus’ iPhone"}}}`
	}
	phone := &chatAppsPhone{Service: "mobile_app_markus_iphone", DeviceID: "0123456789abcdef0123456789abcdef", Name: "Markus’ iPhone"}
	named := "Old phone"
	for name, test := range map[string]struct {
		config string
		state  chatAppsState
		want   *chatAppsPhone
	}{
		"owner's own":       {link("owner-id"), chatAppsState{}, phone},
		"another user's":    {link("anna-id"), chatAppsState{}, nil},
		"App has one":       {link("owner-id"), chatAppsState{OwnerPhone: &named}, nil},
		"no link":           {`{"schema_version":1}`, chatAppsState{}, nil},
		"legacy on":         {`{"schema_version":1,"confirm_phone":{"owner-id":"on"}}`, chatAppsState{}, nil},
		"malformed service": {strings.Replace(link("owner-id"), "mobile_app_markus_iphone", "notify.x", 1), chatAppsState{}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			paths := writeTestConfigFile(t, test.config)
			if got := ownerChatPhone(paths, "owner-id", test.state); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("phone %+v, want %+v", got, test.want)
			}
		})
	}
	// And it goes with the turn-on.
	app := &fakeChatApp{state: chatAppsState{Available: true}}
	useChatApps(t, app, &fakeChatHA{first: chatHARead{ha: chatAppsHA{version: "2026.10.2", loaded: true}}}, chatTestLink)
	paths := writeTestConfigFile(t, link("owner-id"))
	if result, err := turnOnChatApps(context.Background(), setupOwnerIngress, paths, ownerChatPhone(paths, "owner-id", app.state), func(string) {}); !result.ready || !result.phoneStored || err != nil {
		t.Fatalf("turn on: %+v %v", result, err)
	}
	// The phone goes in its own request once Home Assistant is back.
	if got := app.posted(); len(got) != 2 || !reflect.DeepEqual(got[1], map[string]any{"owner_phone": phone}) || got[0]["owner_phone"] != nil {
		t.Fatalf("posted %v", got)
	}
	// The App didn't take it: on, and one line says the phone is missing.
	app.phoneErr = errSetupExit
	if result, err := turnOnChatApps(context.Background(), setupOwnerIngress, paths, phone, func(string) {}); !result.ready || result.phoneStored || !result.phoneFailed || err != nil {
		t.Fatalf("phone refused: %+v %v", result, err)
	}
}

// The ingress request: GET and POST /chat-apps/v1/state with the owner's
// ingress cookie, JSON, no device credential; an App without the route is
// "unsupported", an App refusal keeps its code, an expired session says so.
func TestChatAppsIngressRequest(t *testing.T) {
	type seen struct{ method, path, cookie, auth, contentType, body string }
	var got []seen
	answer := func(w http.ResponseWriter, status int, body string) {
		w.Header().Set(relayVersionHeader, "0.11.0")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
	mode := "ok"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		cookie, _ := r.Cookie("ingress_session")
		value := ""
		if cookie != nil {
			value = cookie.Value
		}
		got = append(got, seen{r.Method, r.URL.Path, value, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(raw)})
		switch mode {
		case "ok":
			answer(w, 200, `{"ok":true,"data":{"available":true,"enabled":true,"unfinished":false,"read_only":false,"chat_codes":true,"allowed_users":null,"owner_phone":null,"connector_path":"/api/ha_nova/mcp","restarting":true,"later_field":1}}`)
		case "missing":
			answer(w, 404, `{"ok":false,"error":{"code":"NOT_FOUND","message":"x"}}`)
		case "refused":
			answer(w, 409, `{"ok":false,"error":{"code":"RESTART_CONSENT_REQUIRED","message":"x"}}`)
		case "expired":
			w.WriteHeader(401) // Home Assistant itself: no Relay header
		}
	}))
	t.Cleanup(server.Close)
	origin, _ := url.Parse(server.URL)
	root, session := "/api/hassio_ingress/"+strings.Repeat("a", 43), strings.Repeat("b", 128)
	client, err := newCloudIngressClientForOrigin(origin, root, session, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	state, err := chatAppsCall(ctx, client, nil)
	if err != nil || !state.Available || !state.Enabled || !state.Restarting || state.OwnerPhone != nil {
		t.Fatalf("get: %+v %v", state, err)
	}
	if _, err := chatAppsCall(ctx, client, map[string]any{"enabled": true, "allow_restart": true}); err != nil {
		t.Fatal(err)
	}
	want := []seen{
		{"GET", root + "/chat-apps/v1/state", session, "", "", ""},
		{"POST", root + "/chat-apps/v1/state", session, "", "application/json", `{"allow_restart":true,"enabled":true}`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("requests\n got %+v\nwant %+v", got, want)
	}
	mode = "missing"
	if _, err := chatAppsCall(ctx, client, nil); err != errChatAppsUnsupported {
		t.Fatalf("404: %v", err)
	}
	mode = "refused"
	_, err = chatAppsCall(ctx, client, map[string]any{"enabled": true})
	if refused, ok := err.(*chatAppsRefused); !ok || refused.code != "RESTART_CONSENT_REQUIRED" || refused.status != 409 {
		t.Fatalf("409: %v", err)
	}
	mode = "expired"
	if _, err = chatAppsCall(ctx, client, nil); !IsCloudErrorCode(err, CloudErrOuterSessionExpired) {
		t.Fatalf("401: %v", err)
	}
	if chatAppsProblem(err).Key != "setup.chat.problem_expired" {
		t.Fatalf("expired problem %q", chatAppsProblem(err).Key)
	}
}

func TestChatAppsHAVersion(t *testing.T) {
	for version, ok := range map[string]bool{"2026.10.0": true, "2026.11.0.dev0": true, "2027.1.1": true, "2026.9.9": false, "2025.12.0": false, "": false, "dev": false} {
		if chatAppsHAVersionOK(version) != ok {
			t.Errorf("%q: want %v", version, ok)
		}
	}
}

// Setup's text path asks once, at step 3's place, default "Not now"; only a
// typed yes turns them on, and the address follows the outcome.
func TestSetupTextPathAsksChatAppsOnce(t *testing.T) {
	for _, answer := range []string{"\n", "yes\n"} {
		app := &fakeChatApp{state: chatAppsState{Available: true}}
		useChatApps(t, app, &fakeChatHA{first: chatHARead{ha: chatAppsHA{version: "2026.10.2", loaded: true}}}, chatTestLink)
		var out strings.Builder
		f := &setupFlow{paths: chatPaths(t), ui: setupTextUI{reader: bufio.NewReader(strings.NewReader(answer)), out: &out}}
		url, _ := f.offerChatApps()
		on := answer == "yes\n"
		if (url != "") != on || (len(app.posted()) == 1) != on {
			t.Fatalf("answer %q: url %q posted %v\n%s", answer, url, app.posted(), out.String())
		}
		if !strings.Contains(out.String(), "Home Assistant restarts once (about a minute).") {
			t.Fatalf("no restart consent on the question:\n%s", out.String())
		}
	}
}

// `ha-nova chat-apps`: status without a sign-in; on refuses what can't
// work before any sign-in, asks the restart consent, then signs in; off
// signs in and turns them off.
func TestChatAppsCommand(t *testing.T) {
	setup := func(t *testing.T, app *fakeChatApp, ha chatHARead, link, stdin string) (runtimePaths, *int) {
		t.Helper()
		paths := chatPaths(t)
		if err := saveConfig(paths, runtimeConfig{HAURL: "http://homeassistant.local:8123", RelayBaseURL: "http://homeassistant.local:8791"}); err != nil {
			t.Fatal(err)
		}
		useChatApps(t, app, &fakeChatHA{first: ha}, link)
		signIns := 0
		signIn, ok, in, resolve := chatAppsSignIn, chatAppsSignInOK, chatAppsStdin, resolveHASignInOriginForSetup
		t.Cleanup(func() {
			chatAppsSignIn, chatAppsSignInOK, chatAppsStdin, resolveHASignInOriginForSetup = signIn, ok, in, resolve
		})
		resolveHASignInOriginForSetup = func(context.Context, string) (*url.URL, error) { return url.Parse("http://homeassistant.local") }
		chatAppsSignInOK = func(io.Writer, runtimeConfig) bool { return true }
		chatAppsStdin = strings.NewReader(stdin)
		chatAppsSignIn = func(io.Writer, string) (haSignInSession, error) {
			signIns++
			return haSignInSession{ingress: setupOwnerIngress, user: HACurrentUser{ID: "owner-id", IsOwner: true}}, nil
		}
		return paths, &signIns
	}
	loaded := chatHARead{ha: chatAppsHA{version: "2026.10.2", loaded: true}}
	run := func(paths runtimePaths, args ...string) (int, string) {
		exit := 0
		out := captureStdout(t, func() { exit = runChatAppsCommand(paths, args) })
		return exit, out
	}

	t.Run("status", func(t *testing.T) {
		paths, signIns := setup(t, &fakeChatApp{}, loaded, chatTestLink, "")
		if exit, out := run(paths, "status"); exit != 0 || !strings.Contains(out, chatTestLink+"/api/ha_nova/mcp") || *signIns != 0 {
			t.Fatalf("status: %d %q", exit, out)
		}
	})
	t.Run("on refuses before a sign-in", func(t *testing.T) {
		for _, test := range []struct {
			ha   chatHARead
			link string
		}{{chatHARead{ha: chatAppsHA{version: "2026.9.1"}}, chatTestLink}, {chatHAReady, ""}} {
			app := &fakeChatApp{state: chatAppsState{Available: true}}
			paths, signIns := setup(t, app, test.ha, test.link, "yes\n")
			if exit, _ := run(paths, "on"); exit != 1 || *signIns != 0 || len(app.posted()) != 0 {
				t.Fatalf("%+v: exit %d sign-ins %d", test, exit, *signIns)
			}
		}
	})
	t.Run("on needs the consent", func(t *testing.T) {
		app := &fakeChatApp{state: chatAppsState{Available: true}}
		paths, signIns := setup(t, app, chatHAReady, chatTestLink, "\n")
		if exit, _ := run(paths, "on"); exit != 0 || *signIns != 1 || len(app.posted()) != 0 {
			t.Fatalf("no consent: exit %d sign-ins %d posted %v", exit, *signIns, app.posted())
		}
	})
	t.Run("on", func(t *testing.T) {
		app := &fakeChatApp{state: chatAppsState{Available: true}}
		paths, signIns := setup(t, app, chatHAReady, chatTestLink, "yes\n")
		chatAppsReadHA = func() func(runtimePaths) (chatAppsHA, error) {
			n := 0
			return func(runtimePaths) (chatAppsHA, error) {
				n++
				return chatAppsHA{version: "2026.10.2", loaded: n > 1}, nil
			}
		}()
		exit, out := run(paths, "on")
		if exit != 0 || *signIns != 1 || !reflect.DeepEqual(app.posted(), []map[string]any{{"enabled": true, "allow_restart": true}}) || !strings.Contains(out, chatTestLink+"/api/ha_nova/mcp") {
			t.Fatalf("on: exit %d sign-ins %d posted %v\n%s", exit, *signIns, app.posted(), out)
		}
	})
	t.Run("on with a dormant App", func(t *testing.T) {
		app := &fakeChatApp{getErr: errChatAppsUnsupported}
		paths, _ := setup(t, app, chatHAReady, chatTestLink, "yes\n")
		if exit, _ := run(paths, "on"); exit != 1 || len(app.posted()) != 0 {
			t.Fatalf("dormant: exit %d posted %v", exit, app.posted())
		}
	})
	t.Run("off", func(t *testing.T) {
		app := &fakeChatApp{state: chatAppsState{Available: true, Enabled: true}}
		paths, signIns := setup(t, app, chatHARead{err: errSetupExit}, "", "")
		if exit, _ := run(paths, "off"); exit != 0 || *signIns != 1 || !reflect.DeepEqual(app.posted(), []map[string]any{{"enabled": false}}) {
			t.Fatalf("off: exit %d sign-ins %d posted %v", exit, *signIns, app.posted())
		}
		app.setErr = errChatAppsUnsupported
		if exit, _ := run(paths, "off"); exit != 0 {
			t.Fatalf("off on a dormant App: exit %d", exit)
		}
		app.setErr = &chatAppsRefused{status: 500, code: "INTERNAL"}
		if exit, _ := run(paths, "off"); exit != 1 {
			t.Fatalf("a failed off: exit %d", exit)
		}
	})
	t.Run("usage", func(t *testing.T) {
		paths, _ := setup(t, &fakeChatApp{}, loaded, chatTestLink, "")
		if exit, _ := run(paths, "maybe"); exit != 1 {
			t.Fatalf("unknown action: exit %d", exit)
		}
	})
}

// A lost answer is asked again: when the App says on, the wait goes on;
// when nobody can tell, the line says so instead of "didn't turn on".
func TestChatAppsLostAnswer(t *testing.T) {
	lost := newCloudError(CloudErrNetwork, "send Cloud ingress request", nil)
	for _, test := range []struct {
		name   string
		getErr error
		want   string
	}{{"App says on", nil, ""}, {"unknown", lost, "setup.chat.problem_unknown"}} {
		t.Run(test.name, func(t *testing.T) {
			app := &fakeChatApp{state: chatAppsState{Available: true}, setErr: lost, getErr: test.getErr}
			useChatApps(t, app, &fakeChatHA{first: chatHARead{ha: chatAppsHA{version: "2026.10.2", loaded: true}}}, chatTestLink)
			chatAppsCall = func(previous func(context.Context, *CloudIngressClient, map[string]any) (chatAppsState, error)) func(context.Context, *CloudIngressClient, map[string]any) (chatAppsState, error) {
				return func(ctx context.Context, c *CloudIngressClient, body map[string]any) (chatAppsState, error) {
					state, err := previous(ctx, c, body)
					if body != nil {
						app.mu.Lock()
						app.state.Enabled = true // the App did turn on; only its answer was lost
						app.mu.Unlock()
					}
					return state, err
				}
			}(chatAppsCall)
			result, err := turnOnChatApps(context.Background(), setupOwnerIngress, chatPaths(t), nil, func(string) {})
			if test.want == "" && (err != nil || !result.ready) {
				t.Fatalf("reconciled: %+v %v", result, err)
			}
			if test.want != "" && (err == nil || chatAppsProblem(err).Key != test.want) {
				t.Fatalf("unknown: %v", err)
			}
		})
	}
}

// A phone linked on step 3 after chat apps went on still reaches them; while
// they are off, the consent names it.
func TestSetupPageChatTakesALaterPhone(t *testing.T) {
	app := &fakeChatApp{state: chatAppsState{Available: true, Enabled: true}}
	useChatApps(t, app, &fakeChatHA{first: chatHAReady}, chatTestLink)
	paths := writeTestConfigFile(t, `{"schema_version":1,"confirm_phone":{"owner-id":{"service":"mobile_app_markus_iphone","device_id":"0123456789abcdef0123456789abcdef","user_id":"owner-id","name":"Markus’ iPhone"}}}`)
	f := &setupFlow{paths: paths, ui: setupTextUI{out: io.Discard}}
	offer := &chatAppsOffer{state: app.state}
	on := &setupPageChat{State: "on"}
	f.pageChatPhone(on, offer)
	if got := app.posted(); len(got) != 1 || got[0]["owner_phone"] == nil || on.Phone != "Markus’ iPhone" || offer.state.OwnerPhone == nil {
		t.Fatalf("on: posted %v phone %q", got, on.Phone)
	}
	f.pageChatPhone(on, offer) // the App has it now: nothing more
	if len(app.posted()) != 1 {
		t.Fatalf("sent twice: %v", app.posted())
	}
	offerRow := &setupPageChat{State: "offer"}
	f.pageChatPhone(offerRow, &chatAppsOffer{state: chatAppsState{Available: true}})
	if len(app.posted()) != 1 || offerRow.Phone != "Markus’ iPhone" {
		t.Fatalf("off: posted %v phone %q", app.posted(), offerRow.Phone)
	}
}
