package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const signInTestIngressRoot = "/api/hassio_ingress/abcdefghijklmnopqrstuvwxyz0123456789ABCD"

// fakeSignInHA is a plain-HTTP Home Assistant with just the sign-in surface:
// /auth/token, /auth/revoke, the WebSocket commands, and one ingress route.
type fakeSignInHA struct {
	t           *testing.T
	server      *httptest.Server
	owner       bool
	noSuper     bool
	silentWS    bool
	onSession   func()
	extra       func(data []byte) (string, bool) // more WebSocket commands
	mu          sync.Mutex
	revoked     bool
	events      []string
	ingressAuth []string
}

func newFakeSignInHA(t *testing.T) *fakeSignInHA {
	t.Helper()
	ha := &fakeSignInHA{t: t, owner: true}
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/token", ha.token)
	mux.HandleFunc("/auth/revoke", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("token") == "refresh-secret" {
			ha.record("revoke")
			ha.mu.Lock()
			ha.revoked = true
			ha.mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/websocket", ha.websocket)
	mux.HandleFunc(signInTestIngressRoot+CloudPathPairInfo, func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("ingress_session")
		ha.mu.Lock()
		ha.ingressAuth = append(ha.ingressAuth, r.Header.Get("Authorization"))
		ha.mu.Unlock()
		if err != nil || cookie.Value != strings.Repeat("b", 128) {
			http.Error(w, "no session", http.StatusUnauthorized)
			return
		}
		w.Header().Set(relayVersionHeader, "0.9.2")
		_, _ = io.WriteString(w, `{"ok":true,"data":{"relay_version":"0.9.2","relay_instance_id":"relay-1","protocol_version":"v2","available":true}}`)
	})
	ha.server = httptest.NewServer(mux)
	t.Cleanup(ha.server.Close)
	return ha
}

func (ha *fakeSignInHA) slug() string {
	slug, err := selectedCloudNOVAAppSlug()
	if err != nil {
		ha.t.Fatal(err)
	}
	return slug
}

func (ha *fakeSignInHA) record(event string) {
	ha.mu.Lock()
	defer ha.mu.Unlock()
	ha.events = append(ha.events, event)
}

func (ha *fakeSignInHA) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	w.Header().Set("Content-Type", "application/json")
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		client, _ := url.Parse(r.Form.Get("client_id"))
		redirect, _ := url.Parse(r.Form.Get("redirect_uri"))
		if r.Form.Get("code") != "good-code" || client == nil || redirect == nil ||
			client.Host != redirect.Host || client.Path != oauthLoopbackClientPath {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_request"}`)
			return
		}
		ha.record("exchange")
		_, _ = io.WriteString(w, `{"access_token":"access-secret","refresh_token":"refresh-secret","token_type":"Bearer","expires_in":1800}`)
	case "refresh_token":
		ha.mu.Lock()
		revoked := ha.revoked
		ha.mu.Unlock()
		if revoked {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"access-2","token_type":"Bearer","expires_in":1800}`)
	default:
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"unsupported_grant_type"}`)
	}
}

func (ha *fakeSignInHA) websocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	ctx := r.Context()
	if ha.silentWS {
		<-ctx.Done()
		return
	}
	_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"auth_required"}`))
	_, auth, err := conn.Read(ctx)
	if err != nil || !strings.Contains(string(auth), `"access_token":"access-secret"`) {
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"auth_invalid"}`))
		return
	}
	_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"auth_ok"}`))
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var command struct {
			ID       int64  `json:"id"`
			Type     string `json:"type"`
			Endpoint string `json:"endpoint"`
		}
		_ = json.Unmarshal(data, &command)
		ha.record(command.Type + " " + command.Endpoint)
		result, handled := "", false
		if ha.extra != nil {
			result, handled = ha.extra(data)
		}
		switch {
		case handled:
		case command.Type == "auth/current_user":
			result = fmt.Sprintf(`{"id":"user-1","name":"Markus","is_owner":%t,"is_admin":true}`, ha.owner)
		case command.Type == "supervisor/api" && ha.noSuper:
			_ = conn.Write(ctx, websocket.MessageText, []byte(fmt.Sprintf(
				`{"id":%d,"type":"result","success":false,"error":{"code":"unknown_command","message":"Unknown command."}}`, command.ID)))
			continue
		case command.Endpoint == "/addons/"+ha.slug()+"/info":
			result = `{"slug":"` + ha.slug() + `","state":"started","version":"0.9.2","ingress":true,"ingress_entry":"` +
				signInTestIngressRoot + `","ingress_url":"` + signInTestIngressRoot + `/home"}`
		case command.Endpoint == "/ingress/session":
			result = `{"session":"` + strings.Repeat("b", 128) + `"}`
			if ha.onSession != nil {
				ha.onSession()
			}
		default:
			ha.t.Errorf("unexpected WebSocket command %s", data)
			return
		}
		_ = conn.Write(ctx, websocket.MessageText, []byte(fmt.Sprintf(
			`{"id":%d,"type":"result","success":true,"result":%s}`, command.ID, result)))
	}
}

// fakeBrowser stands in for the user's browser: it follows the authorize URL
// back to the loopback callback with the given query.
func useFakeBrowser(t *testing.T, ha *fakeSignInHA, query func(state string) string) *[]string {
	t.Helper()
	t.Setenv("HA_NOVA_NO_BROWSER", "")
	opened := []string{}
	previous := openBrowserForSetup
	openBrowserForSetup = func(target string) error {
		opened = append(opened, target)
		authorize, err := url.Parse(target)
		if err != nil || !strings.HasPrefix(target, ha.server.URL+"/auth/authorize?") {
			t.Errorf("authorize URL = %q", target)
			return nil
		}
		redirect := authorize.Query().Get("redirect_uri")
		go func() {
			response, err := http.Get(redirect + "?" + query(authorize.Query().Get("state")))
			if err == nil {
				response.Body.Close()
			}
		}()
		return nil
	}
	t.Cleanup(func() {
		openBrowserForSetup = previous
		clearSetupNextPromptSkipsStaleBlankInput()
	})
	return &opened
}

func signedInQuery(state string) string {
	return url.Values{"code": {"good-code"}, "state": {state}}.Encode()
}

func TestHASignInOpensIngressAndRevokesTheHomeAssistantSessionFirst(t *testing.T) {
	ha := newFakeSignInHA(t)
	opened := useFakeBrowser(t, ha, signedInQuery)
	var out bytes.Buffer

	session, err := openHASignInIngress(context.Background(), &out, ha.server.URL)
	if err != nil {
		t.Fatalf("openHASignInIngress: %v (output %q)", err, out.String())
	}
	if len(*opened) != 1 || session.user.Name != "Markus" || !session.user.IsOwner {
		t.Fatalf("opened=%v user=%+v", *opened, session.user)
	}
	ha.mu.Lock()
	revoked, events := ha.revoked, strings.Join(ha.events, ",")
	ha.mu.Unlock()
	if !revoked || !strings.HasSuffix(events, "supervisor/api /ingress/session,revoke") {
		t.Fatalf("the refresh token must be revoked before pairing starts; events=%s", events)
	}

	response, err := session.ingress.Do(context.Background(), CloudEndpointPairInfo, "", nil)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("ingress call after revocation = %v, %v", response, err)
	}
	response.Body.Close()
	if ha.ingressAuth[0] != "" {
		t.Fatalf("an HA token reached ingress: %q", ha.ingressAuth[0])
	}
	if strings.Contains(out.String(), "access-secret") || strings.Contains(out.String(), "refresh-secret") {
		t.Fatalf("token in output: %q", out.String())
	}
}

func TestHASignInStopsForNonOwnersAndHomesWithoutApps(t *testing.T) {
	for name, setup := range map[string]func(*fakeSignInHA){
		"not owner":     func(ha *fakeSignInHA) { ha.owner = false },
		"no Supervisor": func(ha *fakeSignInHA) { ha.noSuper = true },
	} {
		t.Run(name, func(t *testing.T) {
			ha := newFakeSignInHA(t)
			setup(ha)
			useFakeBrowser(t, ha, signedInQuery)
			session, err := openHASignInIngress(context.Background(), io.Discard, ha.server.URL)
			want := errSignInNotOwner
			if ha.noSuper {
				want = errSignInAppMissing
			}
			if !errors.Is(err, want) || session.ingress != nil {
				t.Fatalf("err = %v, want %v", err, want)
			}
			ha.mu.Lock()
			defer ha.mu.Unlock()
			if !ha.revoked {
				t.Fatal("the refresh token must be revoked on every path")
			}
		})
	}
}

func TestHASignInBoundsAnUnresponsiveHomeAssistantAndStillRevokes(t *testing.T) {
	previous := haSignInStepTimeout
	haSignInStepTimeout = 200 * time.Millisecond
	t.Cleanup(func() { haSignInStepTimeout = previous })
	ha := newFakeSignInHA(t)
	ha.silentWS = true
	useFakeBrowser(t, ha, signedInQuery)
	started := time.Now()
	_, err := openHASignInIngress(context.Background(), io.Discard, ha.server.URL)
	if err == nil || time.Since(started) > 10*time.Second {
		t.Fatalf("err = %v after %s", err, time.Since(started))
	}
	ha.mu.Lock()
	defer ha.mu.Unlock()
	if !ha.revoked {
		t.Fatal("a stalled Home Assistant must still get the refresh token revoked")
	}
}

func TestHASignInCtrlCAfterTheSessionExistsStillStopsAndRevokes(t *testing.T) {
	ha := newFakeSignInHA(t)
	useFakeBrowser(t, ha, signedInQuery)
	previous := haSignInInterrupt
	t.Cleanup(func() { haSignInInterrupt = previous })
	var stop context.CancelFunc
	haSignInInterrupt = func(ctx context.Context) (context.Context, context.CancelFunc) {
		ctx, stop = context.WithCancel(ctx)
		return ctx, stop
	}
	ha.onSession = func() { stop() } // Ctrl+C right as the ingress session is created
	session, err := openHASignInIngress(context.Background(), io.Discard, ha.server.URL)
	if !errors.Is(err, errSignInStopped) || session.ingress != nil {
		t.Fatalf("session=%+v err=%v", session, err)
	}
	ha.mu.Lock()
	defer ha.mu.Unlock()
	if !ha.revoked {
		t.Fatal("the refresh token must be revoked after a late Ctrl+C")
	}
}

func TestHASignInNamesNeverCarryTerminalControls(t *testing.T) {
	user := HACurrentUser{Name: "Ev\x1b[2Jil\x07"}
	if got := haSignInUserName(user); got != "Ev[2Jil" {
		t.Fatalf("name = %q", got)
	}
	if line := haSignInFallbackLine(errSignInNotOwner, user); strings.ContainsAny(line, "\x1b\x07") {
		t.Fatalf("line = %q", line)
	}
	if got := haSignInUserName(HACurrentUser{Name: "\x1b"}); got != "the owner" {
		t.Fatalf("control-only name = %q", got)
	}
}

func TestHASignInCanceledInTheBrowserNeverExchangesACode(t *testing.T) {
	ha := newFakeSignInHA(t)
	useFakeBrowser(t, ha, func(state string) string {
		return url.Values{"error": {"access_denied"}, "state": {state}}.Encode()
	})
	_, err := openHASignInIngress(context.Background(), io.Discard, ha.server.URL)
	if !errors.Is(err, errSignInBrowser) || haSignInFallbackLine(err, HACurrentUser{}) != "Sign-in was canceled." {
		t.Fatalf("err = %v", err)
	}
	ha.mu.Lock()
	defer ha.mu.Unlock()
	if len(ha.events) != 0 {
		t.Fatalf("events after a canceled sign-in: %v", ha.events)
	}
}

func TestParseLocalHAOrigin(t *testing.T) {
	for raw, want := range map[string]string{
		"http://homeassistant.local:8123":   "http://homeassistant.local:8123",
		"https://HA.example.com/":           "https://ha.example.com",
		"http://192.168.1.10:8123":          "http://192.168.1.10:8123",
		"http://[fd00::10]:8123":            "http://[fd00::10]:8123",
		"http://homeassistant.local:8123/x": "",
		"http://user:pw@ha.local:8123":      "",
		"http://ha.local:8123?x=1":          "",
		"http://ha.local:8123#frag":         "",
		"ws://ha.local:8123":                "",
		"ha.local:8123":                     "",
		"http://ha.local:0":                 "",
		"":                                  "",
	} {
		origin, err := parseLocalHAOrigin(raw)
		got := ""
		if err == nil {
			got = origin.String()
		}
		if got != want {
			t.Errorf("parseLocalHAOrigin(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
}

func TestHASignInFallbackLinesStayPlain(t *testing.T) {
	user := HACurrentUser{Name: "Alex"}
	cases := map[string]error{
		"Sign-in stopped.":                        errors.Join(errSignInStopped, newCloudError(CloudErrTimeout, "wait", nil)),
		"No sign-in arrived within five minutes.": errors.Join(errSignInBrowser, newCloudError(CloudErrTimeout, "wait", nil)),
		"Signed in as Alex, who is not the Home Assistant owner. Only the owner can connect a computer.": errSignInNotOwner,
		"did not find a running NOVA App":                    errors.Join(errSignInAppMissing, newCloudError(CloudErrHAProtocol, "x", nil)),
		"Your NOVA App does not support sign-in yet":         &haSignInPairingError{err: errSignInRelayUnsupported},
		"The NOVA App is not ready to connect computers yet": &haSignInPairingError{err: errSignInRelayNotReady},
		"Connecting through Home Assistant did not finish.":  &haSignInPairingError{err: newCloudError(CloudErrPairingRejected, "x", nil)},
		"answered from a different address":                  newCloudError(CloudErrRedirectRejected, "x", nil),
		"Home Assistant did not answer.":                     newCloudError(CloudErrNetwork, "x", nil),
		"Could not finish the sign-in with Home Assistant.":  newCloudError(CloudErrHAProtocol, "x", nil),
	}
	for want, err := range cases {
		if got := haSignInFallbackLine(err, user); !strings.Contains(got, want) {
			t.Errorf("line for %v = %q, want it to contain %q", err, got, want)
		}
	}
	if got := haSignInFallbackLine(errSignInNotOwner, HACurrentUser{}); !strings.HasPrefix(got, "The account you signed in with") {
		t.Errorf("nameless non-owner line = %q", got)
	}
}

func TestHASignInStepPairsRetriesOrFallsBackInTheSameStep(t *testing.T) {
	previousOpen, previousPair, previousResolve := openHASignInIngressForSetup, signInPairForSetup, resolveHASignInOriginForSetup
	t.Cleanup(func() {
		openHASignInIngressForSetup, signInPairForSetup, resolveHASignInOriginForSetup = previousOpen, previousPair, previousResolve
	})
	resolveHASignInOriginForSetup = func(context.Context, string) (*url.URL, error) {
		return &url.URL{Scheme: "http", Host: "ha.local"}, nil
	}
	owner := haSignInSession{user: HACurrentUser{Name: "Markus", IsOwner: true}}
	cfg := &runtimeConfig{HAURL: "http://ha.local:8123", RelayBaseURL: "http://ha.local:8791"}
	paths := runtimePaths{ConfigDir: t.TempDir()}

	for name, test := range map[string]struct {
		input   string
		signIns []error
		pairErr error
		paired  bool
		code    string
		wantErr error
		output  string
		pairs   int
	}{
		"paired":                  {input: "\n", signIns: []error{nil}, paired: true, output: "Signed in to Home Assistant as Markus (owner)", pairs: 1},
		"yes":                     {input: "yes\n", signIns: []error{nil}, paired: true, pairs: 1},
		"retry after stop":        {input: "\n\n", signIns: []error{errors.Join(errSignInStopped, context.Canceled), nil}, paired: true, output: "Press Enter to sign in again", pairs: 1},
		"wrong account then code": {input: "\ncode\n", signIns: []error{errSignInNotOwner}, output: "Only the owner can connect a computer."},
		"no App":                  {input: "\n", signIns: []error{errSignInAppMissing}, output: "Use a six-digit code from NOVA instead."},
		"pairing failed":          {input: "\n", signIns: []error{nil}, pairErr: &haSignInPairingError{err: newCloudError(CloudErrPairingRejected, "x", nil)}, output: "Use a six-digit code from NOVA instead.", pairs: 1},
		"pin mismatch":            {input: "\n", signIns: []error{nil}, pairErr: errPinMismatch, wantErr: errPinMismatch, output: "secure identity did not match", pairs: 1},
		"code chosen":             {input: "code\n"},
		"six digits":              {input: "473 921\n", code: "473921"},
		"typo then code":          {input: "maybe\ncode\n", output: "Press Enter to sign in, or type 'code'."},
		"manual":                  {input: "manual\n", wantErr: errSetupRelayTokenStep},
		"back":                    {input: "back\n", wantErr: errSetupBack},
	} {
		t.Run(name, func(t *testing.T) {
			signIns := append([]error(nil), test.signIns...)
			openHASignInIngressForSetup = func(_ context.Context, _ io.Writer, origin string) (haSignInSession, error) {
				if origin != "http://ha.local" {
					t.Fatalf("sign-in must use the resolved origin, got %q", origin)
				}
				err := signIns[0]
				signIns = signIns[1:]
				return owner, err
			}
			pairs := 0
			signInPairForSetup = func(string, pairingProvisioner, *runtimeConfig, func(*runtimeConfig) error, pairingClientInfo, SecretStoreUIPolicy) (string, error) {
				pairs++
				return "device-1", test.pairErr
			}
			var out bytes.Buffer
			paired, code, err := runHASignInPairingStep(bufio.NewReader(strings.NewReader(test.input)), &out, paths, cfg)
			if paired != test.paired || code != test.code || !errors.Is(err, test.wantErr) || (test.wantErr == nil && err != nil) {
				t.Fatalf("paired=%v code=%q err=%v; output %q", paired, code, err, out.String())
			}
			if !strings.Contains(out.String(), test.output) || pairs != test.pairs || len(signIns) != 0 {
				t.Fatalf("output %q (want %q), pairs=%d (want %d), unused sign-ins=%d", out.String(), test.output, pairs, test.pairs, len(signIns))
			}
		})
	}
}

func TestHASignInStepSkipsTheBrowserWhenHomeAssistantIsUnreachable(t *testing.T) {
	previousOpen, previousResolve := openHASignInIngressForSetup, resolveHASignInOriginForSetup
	t.Cleanup(func() { openHASignInIngressForSetup, resolveHASignInOriginForSetup = previousOpen, previousResolve })
	resolveHASignInOriginForSetup = func(context.Context, string) (*url.URL, error) {
		return nil, newCloudError(CloudErrRedirectRejected, "reach Home Assistant", nil)
	}
	openHASignInIngressForSetup = func(context.Context, io.Writer, string) (haSignInSession, error) {
		t.Fatal("no browser may open when Home Assistant is unreachable")
		return haSignInSession{}, nil
	}
	var out bytes.Buffer
	paired, code, err := runHASignInPairingStep(bufio.NewReader(strings.NewReader("")), &out,
		runtimePaths{ConfigDir: t.TempDir()}, &runtimeConfig{HAURL: "http://ha.local:8123"})
	if paired || code != "" || err != nil || !strings.Contains(out.String(), "Use a six-digit code from NOVA instead.") {
		t.Fatalf("paired=%v code=%q err=%v output %q", paired, code, err, out.String())
	}
}

func TestResolveHASignInOriginFollowsOnlyTheSameHostPortMove(t *testing.T) {
	providers := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/providers" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"providers":[]}`)
	}
	moved := httptest.NewServer(http.HandlerFunc(providers))
	defer moved.Close()
	redirectTo := func(target string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target+r.URL.Path, http.StatusTemporaryRedirect)
		}))
	}
	plain := httptest.NewServer(http.HandlerFunc(providers))
	defer plain.Close()
	legacy := redirectTo(moved.URL) // Home Assistant 2026.10: 8123 -> 80 on the same host
	defer legacy.Close()
	elsewhere := redirectTo(strings.Replace(moved.URL, "127.0.0.1", "localhost", 1))
	defer elsewhere.Close()
	chained := redirectTo(legacy.URL)
	defer chained.Close()
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadRequest) }))
	defer broken.Close()

	for name, test := range map[string]struct {
		url  string
		want string
		code CloudErrorCode
	}{
		"plain answer":     {url: plain.URL, want: plain.URL},
		"legacy port move": {url: legacy.URL, want: moved.URL},
		"other host":       {url: elsewhere.URL, code: CloudErrRedirectRejected},
		"two redirects":    {url: chained.URL, code: CloudErrRedirectRejected},
		"unexpected":       {url: broken.URL, code: CloudErrHAProtocol},
	} {
		origin, err := resolveHASignInOrigin(context.Background(), test.url)
		switch {
		case test.code != "" && !IsCloudErrorCode(err, test.code):
			t.Errorf("%s: err = %v, want %s", name, err, test.code)
		case test.code == "" && (err != nil || origin.String() != test.want):
			t.Errorf("%s: origin = %v, err = %v, want %s", name, origin, err, test.want)
		}
	}
}

func TestHASignInCtrlCStopsTheWaitWithoutExchanging(t *testing.T) {
	ha := newFakeSignInHA(t)
	previous := haSignInInterrupt
	t.Cleanup(func() { haSignInInterrupt = previous })
	var stop context.CancelFunc
	haSignInInterrupt = func(ctx context.Context) (context.Context, context.CancelFunc) {
		ctx, stop = context.WithCancel(ctx)
		return ctx, stop
	}
	t.Setenv("HA_NOVA_NO_BROWSER", "")
	previousOpen := openBrowserForSetup
	openBrowserForSetup = func(string) error { stop(); return nil } // the user presses Ctrl+C
	t.Cleanup(func() { openBrowserForSetup = previousOpen; clearSetupNextPromptSkipsStaleBlankInput() })
	_, err := openHASignInIngress(context.Background(), io.Discard, ha.server.URL)
	if !errors.Is(err, errSignInStopped) || !haSignInRetryable(err) {
		t.Fatalf("err = %v", err)
	}
	ha.mu.Lock()
	defer ha.mu.Unlock()
	if len(ha.events) != 0 {
		t.Fatalf("events after Ctrl+C: %v", ha.events)
	}
}

func TestRelayAdvertisesSignInOnlyWithTheCapabilityFlag(t *testing.T) {
	for body, want := range map[string]bool{
		`{"ok":true,"data":{"protocol_version":"v1","sign_in":true}}`:  true,
		`{"ok":true,"data":{"protocol_version":"v1","sign_in":false}}`: false,
		`{"ok":true,"data":{"protocol_version":"v1"}}`:                 false,
		`not json`: false,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/pair/v1/info" {
				http.NotFound(w, r)
				return
			}
			_, _ = io.WriteString(w, body)
		}))
		if got := relayAdvertisesSignIn(server.URL); got != want {
			t.Errorf("relayAdvertisesSignIn(%s) = %v, want %v", body, got, want)
		}
		server.Close()
	}
	if relayAdvertisesSignIn("http://127.0.0.1:1") {
		t.Error("an unreachable relay must not advertise sign-in")
	}
}
