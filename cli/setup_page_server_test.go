package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// setupPageTest is a started setup page with nothing behind it but a UI that
// tests can make wait for actions.
type setupPageTest struct {
	t      *testing.T
	ui     *setupWebUI
	server *setupPageServer
	base   string
	cookie *http.Cookie
}

func startSetupPageTest(t *testing.T) *setupPageTest {
	t.Helper()
	ui := newSetupWebUI(strings.NewReader(""), io.Discard)
	server, err := startSetupPageServer(ui)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.close)
	return &setupPageTest{t: t, ui: ui, server: server, base: "http://127.0.0.1:" + server.port}
}

var setupPageNoRedirect = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	Timeout:       5 * time.Second,
}

func (p *setupPageTest) do(method, path string, body string, header map[string]string) *http.Response {
	p.t.Helper()
	request, err := http.NewRequest(method, p.base+path, strings.NewReader(body))
	if err != nil {
		p.t.Fatal(err)
	}
	for name, value := range header {
		if name == "Host" {
			request.Host = value
			continue
		}
		request.Header.Set(name, value)
	}
	response, err := setupPageNoRedirect.Do(request)
	if err != nil {
		p.t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(data))
	return response
}

// bounceTarget is where a bounce page sends the browser next, or "".
func bounceTarget(response *http.Response) string {
	body, _ := io.ReadAll(response.Body)
	match := regexp.MustCompile(`<meta http-equiv="refresh" content="0;url=([^"]*)">`).FindSubmatch(body)
	if response.StatusCode != http.StatusOK || match == nil {
		return ""
	}
	return string(match[1])
}

// launch opens the launch URL once and keeps the session cookie.
func (p *setupPageTest) launch() {
	p.t.Helper()
	response := p.do(http.MethodGet, "/s/"+p.server.token, "", nil)
	if target := bounceTarget(response); target != "/" {
		p.t.Fatalf("launch = %d, bounces to %q", response.StatusCode, target)
	}
	cookies := response.Cookies()
	if len(cookies) != 1 {
		p.t.Fatalf("launch cookies = %v", cookies)
	}
	p.cookie = cookies[0]
}

func (p *setupPageTest) post(body string, header map[string]string) int {
	p.t.Helper()
	all := map[string]string{"Content-Type": "application/json", "Origin": p.base, "Cookie": p.cookie.Name + "=" + p.cookie.Value}
	for name, value := range header {
		all[name] = value
	}
	for name, value := range all {
		if value == "" {
			delete(all, name)
		}
	}
	return p.do(http.MethodPost, "/api/action", body, all).StatusCode
}

func TestSetupPageBindsLoopbackOnly(t *testing.T) {
	page := startSetupPageTest(t)
	if host := page.server.callbackHost(); host != "127.0.0.1:"+page.server.port {
		t.Fatalf("callback host = %q", host)
	}
	if !strings.HasPrefix(page.server.launchURL(), "http://127.0.0.1:"+page.server.port+"/s/") {
		t.Fatalf("launch URL = %q", page.server.launchURL())
	}
	if len(page.server.token) != 43 { // 256 bits, base64url
		t.Fatalf("launch token has %d characters", len(page.server.token))
	}
}

// DNS rebinding: a foreign name that resolves to 127.0.0.1 never reaches a
// handler.
func TestSetupPageRefusesForeignHostHeaders(t *testing.T) {
	page := startSetupPageTest(t)
	for _, host := range []string{"evil.example:" + page.server.port, "evil.example", "127.0.0.1", "127.0.0.1:1", "0.0.0.0:" + page.server.port, "localhost:" + page.server.port, "LOCALHOST:" + page.server.port} {
		for _, path := range []string{"/", "/s/" + page.server.token, "/api/state", oauthLoopbackCallbackPath} {
			if response := page.do(http.MethodGet, path, "", map[string]string{"Host": host}); response.StatusCode != http.StatusMisdirectedRequest {
				t.Fatalf("Host %q %s = %d, want 421", host, path, response.StatusCode)
			}
		}
	}
	if page.server.used.Load() {
		t.Fatal("a foreign Host used the launch token")
	}
	if response := page.do(http.MethodGet, "/", "", map[string]string{"Host": "127.0.0.1:" + page.server.port}); response.StatusCode != http.StatusOK {
		t.Fatalf("own host = %d", response.StatusCode)
	}
}

func TestSetupPageLaunchTokenIsSingleUse(t *testing.T) {
	page := startSetupPageTest(t)
	if response := page.do(http.MethodGet, "/s/"+strings.Repeat("A", 43), "", nil); response.StatusCode != http.StatusNotFound {
		t.Fatalf("wrong token = %d", response.StatusCode)
	}
	if response := page.do(http.MethodPost, "/s/"+page.server.token, "", nil); response.StatusCode != http.StatusMethodNotAllowed || page.server.used.Load() {
		t.Fatalf("POST launch = %d used=%v", response.StatusCode, page.server.used.Load())
	}
	page.launch()
	cookie := page.cookie
	if cookie.Name != setupPageCookie || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" ||
		cookie.Domain != "" || len(cookie.Value) != 43 || cookie.Value == page.server.token {
		t.Fatalf("session cookie = %+v", cookie)
	}
	// Reuse (another tab, a leaked URL) gets no session.
	again := page.do(http.MethodGet, "/s/"+page.server.token, "", nil)
	if target := bounceTarget(again); target != "/#elsewhere" || len(again.Cookies()) != 0 {
		t.Fatalf("reused token bounces to %q, cookies %v", target, again.Cookies())
	}
	state := func(cookieHeader string) int {
		return page.do(http.MethodGet, "/api/state", "", map[string]string{"Cookie": cookieHeader}).StatusCode
	}
	if code := state(cookie.Name + "=" + cookie.Value); code != http.StatusOK {
		t.Fatalf("state with the session = %d", code)
	}
	for _, header := range []string{"", cookie.Name + "=" + page.server.token, cookie.Name + "=" + strings.Repeat("A", 43), cookie.Name + "="} {
		if code := state(header); code != http.StatusUnauthorized {
			t.Fatalf("state with cookie %q = %d", header, code)
		}
	}
	cross := page.do(http.MethodGet, "/api/state", "", map[string]string{"Cookie": cookie.Name + "=" + cookie.Value, "Sec-Fetch-Site": "cross-site"})
	if cross.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site state read = %d", cross.StatusCode)
	}
}

// CSRF: only this page's own JSON fetch can change anything.
func TestSetupPageActionsNeedSameOriginJSON(t *testing.T) {
	page := startSetupPageTest(t)
	page.launch()
	page.ui.update(func(v *setupPageState) { v.Accepts = []string{"signin"} })
	for name, test := range map[string]struct {
		body   string
		header map[string]string
		want   int
	}{
		"no Origin":           {body: `{"action":"signin"}`, header: map[string]string{"Origin": ""}, want: http.StatusForbidden},
		"other origin":        {body: `{"action":"signin"}`, header: map[string]string{"Origin": "http://evil.example"}, want: http.StatusForbidden},
		"null origin":         {body: `{"action":"signin"}`, header: map[string]string{"Origin": "null"}, want: http.StatusForbidden},
		"other port":          {body: `{"action":"signin"}`, header: map[string]string{"Origin": "http://127.0.0.1:1"}, want: http.StatusForbidden},
		"cross-site fetch":    {body: `{"action":"signin"}`, header: map[string]string{"Sec-Fetch-Site": "cross-site"}, want: http.StatusForbidden},
		"same-site fetch":     {body: `{"action":"signin"}`, header: map[string]string{"Sec-Fetch-Site": "same-site"}, want: http.StatusForbidden},
		"form post":           {body: `action=signin`, header: map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, want: http.StatusUnsupportedMediaType},
		"text/plain post":     {body: `{"action":"signin"}`, header: map[string]string{"Content-Type": "text/plain"}, want: http.StatusUnsupportedMediaType},
		"multipart post":      {body: `x`, header: map[string]string{"Content-Type": "multipart/form-data; boundary=x"}, want: http.StatusUnsupportedMediaType},
		"no cookie":           {body: `{"action":"signin"}`, header: map[string]string{"Cookie": ""}, want: http.StatusUnauthorized},
		"unknown action":      {body: `{"action":"pair"}`, want: http.StatusBadRequest},
		"unknown field":       {body: `{"action":"signin","url":"http://x"}`, want: http.StatusBadRequest},
		"two values":          {body: `{"action":"signin"}{"action":"signin"}`, want: http.StatusBadRequest},
		"too long a value":    {body: `{"action":"address","value":"` + strings.Repeat("a", 300) + `"}`, want: http.StatusBadRequest},
		"too big a body":      {body: `{"action":"signin","value":"` + strings.Repeat("a", 5000) + `"}`, want: http.StatusBadRequest},
		"not waited for":      {body: `{"action":"address","value":"10.0.0.2"}`, want: http.StatusConflict},
		"stop without a task": {body: `{"action":"stop"}`, want: http.StatusConflict},
	} {
		if got := page.post(test.body, test.header); got != test.want {
			t.Errorf("%s = %d, want %d", name, got, test.want)
		}
	}
	select {
	case action := <-page.ui.actions:
		t.Fatalf("a refused request reached the engine: %+v", action)
	default:
	}
	if got := page.do(http.MethodGet, "/api/action", "", nil).StatusCode; got != http.StatusMethodNotAllowed {
		t.Fatalf("GET action = %d", got)
	}
	if got := page.post(`{"action":"signin"}`, map[string]string{"Content-Type": "application/json; charset=utf-8"}); got != http.StatusNoContent {
		t.Fatalf("own request = %d", got)
	}
	if action := <-page.ui.actions; action.Action != "signin" {
		t.Fatalf("action = %+v", action)
	}
	if got := page.post(`{"action":"signin"}`, nil); got != http.StatusConflict {
		t.Fatalf("a double click = %d, want 409", got)
	}
}

func TestSetupPageHeadersOnEveryResponse(t *testing.T) {
	page := startSetupPageTest(t)
	for _, test := range []struct {
		path   string
		header map[string]string
	}{
		{path: "/"}, {path: "/assets/app.js"}, {path: "/assets/steps.js"}, {path: "/assets/style.css"}, {path: "/assets/logo.svg"},
		{path: "/api/state"}, {path: "/nope"}, {path: "/assets/../setup_page_server.go"},
		{path: "/", header: map[string]string{"Host": "evil.example"}},
	} {
		response := page.do(http.MethodGet, test.path, "", test.header)
		csp := response.Header.Get("Content-Security-Policy")
		for _, part := range []string{"default-src 'none'", "script-src 'self'", "style-src 'self'", "frame-ancestors 'none'", "form-action 'self'", "base-uri 'none'"} {
			if !strings.Contains(csp, part) {
				t.Errorf("%s: CSP %q lacks %q", test.path, csp, part)
			}
		}
		if strings.Contains(csp, "unsafe") {
			t.Errorf("%s: CSP %q allows unsafe sources", test.path, csp)
		}
		for name, want := range map[string]string{"X-Frame-Options": "DENY", "Referrer-Policy": "no-referrer", "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff"} {
			if got := response.Header.Get(name); got != want {
				t.Errorf("%s: %s = %q, want %q", test.path, name, got, want)
			}
		}
	}
	if got := page.do(http.MethodGet, "/assets/../setup_page_server.go", "", nil).StatusCode; got != http.StatusNotFound {
		t.Fatalf("path escape = %d", got)
	}
	if got := page.do(http.MethodGet, "/assets/index.html", "", nil).StatusCode; got != http.StatusNotFound {
		t.Fatalf("unlisted asset = %d", got)
	}
}

// No inline script or style (the CSP would block it anyway), no HTML sinks,
// and nothing secret in what the page reads.
func TestSetupPageServesNoInlineCodeAndNoSecrets(t *testing.T) {
	index, err := os.ReadFile(filepath.Join("setupweb", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{`<script(?:\s[^>]*)?>[^<]`, `\sstyle=`, `\son[a-z]+=`, `<style`} {
		if regexp.MustCompile(pattern).Match(index) {
			t.Errorf("index.html matches %s", pattern)
		}
	}
	for _, script := range regexp.MustCompile(`<script[^>]*>`).FindAll(index, -1) {
		if !strings.Contains(string(script), `src="/assets/`) {
			t.Errorf("inline script tag %s", script)
		}
	}
	for _, name := range []string{"app.js", "steps.js"} {
		script, err := os.ReadFile(filepath.Join("setupweb", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, sink := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "new Function", "setAttribute('on", "srcdoc", "location.assign", "href:"} {
			if strings.Contains(string(script), sink) && (name == "steps.js" || (sink != "location.assign" && sink != "href:")) {
				t.Errorf("%s uses %s", name, sink)
			}
		}
	}

	page := startSetupPageTest(t)
	page.launch()
	page.ui.update(func(v *setupPageState) { v.HA = `<img src=x onerror=alert(1)>` })
	request, _ := http.NewRequest(http.MethodGet, page.base+"/api/state", nil)
	request.AddCookie(page.cookie)
	response, err := setupPageNoRedirect.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if strings.Contains(string(body), page.server.token) || strings.Contains(string(body), page.cookie.Value) || strings.Contains(string(body), "<img") {
		t.Fatalf("state leaks a secret or raw markup: %s", body)
	}
	var state setupPageState
	if err := json.Unmarshal(body, &state); err != nil || state.HA != `<img src=x onerror=alert(1)>` {
		t.Fatalf("state = %s (%v)", body, err)
	}
}

// The OAuth callback route exists only while a sign-in waits, needs no
// session cookie (Home Assistant's redirect is cross-site), and returns to
// the page.
func TestSetupPageRoutesTheSignInCallbackOnlyWhileItWaits(t *testing.T) {
	page := startSetupPageTest(t)
	if got := page.do(http.MethodGet, oauthLoopbackCallbackPath+"?code=x&state=y", "", nil).StatusCode; got != http.StatusNotFound {
		t.Fatalf("idle callback = %d", got)
	}
	called := 0
	stop := page.ui.routeCallback(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		page.ui.callbackDone(w, r)
	}))
	page.ui.navigate("http://ha.local:8123/auth/authorize?state=s")
	response := page.do(http.MethodGet, oauthLoopbackCallbackPath+"?"+url.Values{"code": {"c"}, "state": {"s"}}.Encode(), "", nil)
	if target := bounceTarget(response); called != 1 || target != "/" || page.ui.snapshot().SignInURL != "" {
		t.Fatalf("callback bounces to %q called=%d url=%q", target, called, page.ui.snapshot().SignInURL)
	}
	stop()
	if got := page.do(http.MethodGet, oauthLoopbackCallbackPath, "", nil).StatusCode; got != http.StatusNotFound {
		t.Fatalf("callback after the sign-in = %d", got)
	}
}

// The poll that reads how setup ended gets its answer, though setup closes
// the server the moment that poll is served (0.27 upgrade lane D5: "This
// page lost touch with setup." after Done).
func TestSetupPageEndScreenReachesThePageBeforeClose(t *testing.T) {
	for i := 0; i < 300; i++ {
		ui := newSetupWebUI(strings.NewReader(""), io.Discard)
		server, err := startSetupPageServer(ui)
		if err != nil {
			t.Fatal(err)
		}
		p := &setupPageTest{t: t, ui: ui, server: server, base: "http://127.0.0.1:" + server.port}
		p.launch()
		ui.end("saved")
		closed := make(chan struct{})
		go func() { <-server.shownEnd; server.close(); close(closed) }()
		request, _ := http.NewRequest(http.MethodGet, p.base+"/api/state", nil)
		request.AddCookie(p.cookie)
		response, err := setupPageNoRedirect.Do(request)
		if err != nil {
			t.Fatalf("run %d: the end screen never arrived: %v", i, err)
		}
		var state struct{ Screen string }
		err = json.NewDecoder(response.Body).Decode(&state)
		response.Body.Close()
		if err != nil || state.Screen != "saved" {
			t.Fatalf("run %d: state %+v, %v", i, state, err)
		}
		<-closed
	}
}
