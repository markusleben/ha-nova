package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeWebSocketCaller answers get_config and cloud/status (nil: unknown
// command, as Home Assistant without the cloud integration).
type fakeWebSocketCaller struct{ config, cloud map[string]any }

func (f fakeWebSocketCaller) Call(_ context.Context, commandType string, _ map[string]any, result any) error {
	value := map[string]map[string]any{"get_config": f.config, "cloud/status": f.cloud}[commandType]
	if value == nil {
		return errors.New("unknown_command")
	}
	data, _ := json.Marshal(value)
	return json.Unmarshal(data, result)
}

// Claude or ChatGPT in the browser: the owner's sign-in shows what chat apps
// miss, by the chat-app row's own address rule.
func TestPageWebCheckSaysWhatChatAppsNeed(t *testing.T) {
	ready := map[string]any{"logged_in": true, "active_subscription": true, "remote_connected": true, "remote_domain": "abc123.ui.nabu.casa",
		"remote_certificate_status": "ready", "remote_certificate": map[string]any{"common_name": "abc123.ui.nabu.casa"}, "prefs": map[string]any{"remote_enabled": true}}
	remoteOff := map[string]any{"logged_in": true, "active_subscription": true, "remote_domain": "abc123.ui.nabu.casa", "prefs": map[string]any{"remote_enabled": false}}
	config := func(version, external string, components ...string) map[string]any {
		return map[string]any{"version": version, "external_url": external, "components": append([]string{"http"}, components...)}
	}
	for _, c := range []struct {
		name   string
		socket fakeWebSocketCaller
		want   string // "": ready
	}{
		{"Container or Core", fakeWebSocketCaller{config: config("2026.10.2", "https://home.example.com")}, "setup.chat.problem_no_apps"},
		{"older than 2026.10", fakeWebSocketCaller{config: config("2026.9.4", "https://home.example.com", "hassio")}, "setup.chat.problem_old"},
		{"no Link, a local address", fakeWebSocketCaller{config: config("2026.10.2", "http://homeassistant.local:8123", "hassio")}, "setup.chat.need_address_web"},
		{"Link with remote access off", fakeWebSocketCaller{config: config("2026.10.2", "https://abc123.ui.nabu.casa", "hassio", "cloud"), cloud: remoteOff}, "setup.chat.need_address_web"},
		{"Link with remote access", fakeWebSocketCaller{config: config("2026.10.2", "", "hassio", "cloud"), cloud: ready}, ""},
		{"Link ready but its integration not loaded", fakeWebSocketCaller{config: config("2026.10.2", "", "hassio"), cloud: ready}, "setup.chat.need_address_web"},
		{"own HTTPS address", fakeWebSocketCaller{config: config("2026.11.0", "https://home.example.com", "hassio")}, ""},
	} {
		err := pageWebCheck(context.Background(), c.socket)
		var notReady *setupWebNotReady
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.want != "" && (!errors.As(err, &notReady) || notReady.msg.Key != c.want || notReady.never != (c.want == "setup.chat.problem_no_apps")):
			t.Errorf("%s: %v, want %s", c.name, err, c.want)
		}
	}
	// A read that fails fails the sign-in: never "no address".
	for _, socket := range []fakeWebSocketCaller{{}, {config: config("2026.10.2", "", "hassio", "cloud")}} {
		if err := pageWebCheck(context.Background(), socket); err == nil || errors.As(err, new(*setupWebNotReady)) {
			t.Fatalf("an unread %v passes or reads as not ready: %v", socket.config, err)
		}
	}
}

// webCheckHA: the fake Home Assistant answers the browser path's check with
// external as its external URL, and no Link.
func webCheckHA(ha *fakeSignInHA, external *atomic.Value) {
	ha.extra = func(data []byte) (string, bool) {
		var command struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(data, &command)
		switch command.Type {
		case "get_config":
			return fmt.Sprintf(`{"version":"2026.10.2","external_url":%q,"components":["http","hassio"]}`, external.Load().(string)), true
		case "cloud/status":
			return `{"logged_in":false}`, true
		}
		return "", false
	}
}

// The browser path never pairs into a dead end: with no internet address,
// the owner's sign-in stops before pairing and says so, with "Set it up,
// then check again" (a fresh sign-in checks again) and "Use HA NOVA on this
// computer instead" (back to the No-AI-app screen). Once the address is
// there, the same path pairs.
func TestSetupPageBrowserPathSaysWhatIsMissingBeforePairing(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{})
	t.Cleanup(clearSetupNextPromptSkipsStaleBlankInput)
	setupGoldenHome(t)
	ha := newFakeSignInHA(t)
	var external atomic.Value
	external.Store("http://homeassistant.local:8123")
	webCheckHA(ha, &external)
	pairs := stubSetupPageHome(t, ha.server.URL, true)
	page, stdout, done := startSetupPageFlow(t, "")
	signIn := func() {
		t.Helper()
		page.click("signin", "")
		waiting := page.waitFor("the sign-in URL", func(s setupPageState) bool { return s.SignInURL != "" })
		authorize, err := url.Parse(waiting.SignInURL)
		if err != nil {
			t.Fatal(err)
		}
		page.do(http.MethodGet, strings.TrimPrefix(authorize.Query().Get("redirect_uri")+"?"+signedInQuery(authorize.Query().Get("state")), page.base), "", nil)
	}
	notReady := func() {
		t.Helper()
		got := page.waitFor("the missing address", accepting("local"))
		if got.Screen != "connect" || got.Fix != "web" || got.Problem == nil || got.Problem.Key != "setup.chat.need_address_web" ||
			got.Links["chat"] != chatAppsGuideURL || !slices.Contains(got.Accepts, "signin") || *pairs != 0 {
			t.Fatalf("not ready = %+v, pairs %d", got, *pairs)
		}
	}

	none := page.waitFor("no AI app", accepting("web"))
	if none.Links["chat"] != chatAppsGuideURL {
		t.Fatalf("no AI app links = %v", none.Links)
	}
	page.click("web", "")
	page.waitFor("the found screen", accepting("signin"))
	signIn()
	notReady()
	signIn() // Set it up, then check again: still no address
	notReady()

	page.click("local", "") // Use HA NOVA on this computer instead
	back := page.waitFor("no AI app again", accepting("web"))
	if back.Screen != "noclient" || back.Web || back.Problem != nil {
		t.Fatalf("back = %+v", back)
	}
	page.click("web", "")
	found := page.waitFor("the found screen again", accepting("signin"))
	if !found.Web || found.Fix != "" {
		t.Fatalf("found = %+v", found)
	}
	external.Store("https://home.example.com")
	signIn()
	control := page.waitFor("step 3", accepting("finish"))
	if control.Screen != "control" || *pairs != 1 {
		t.Fatalf("step 3 = %+v, pairs %d", control, *pairs)
	}
	page.click("finish", "")
	page.waitFor("step 4", accepting("done"))
	page.click("done", "")
	if code := setupPageExit(t, done, stdout); code != 0 || *pairs != 1 {
		t.Fatalf("exit %d pairs %d\n%s", code, *pairs, stdout)
	}
}
