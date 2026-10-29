package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The bridge's control channel (mcp_serve_control.go) against the family
// home with Home Assistant's own id shapes: Markus (the owner) with an
// iPhone and the family iPad, Anna (an administrator), and a kid who is not.

const (
	ownerHex  = "0000000000000000000000000000000a"
	annaHex   = "0000000000000000000000000000000b"
	kidHex    = "0000000000000000000000000000000c"
	devMarkus = "d000000000000000000000000000000a"
	devAnna   = "d000000000000000000000000000000b"
	devKid    = "d000000000000000000000000000000c"
	devIpad   = "d000000000000000000000000000000d"
)

func hexFamily() *fakeHome {
	home := newFamilyHome()
	ids := map[string]string{"owner-id": ownerHex, "anna-id": annaHex, "kid-id": kidHex,
		"dev-markus": devMarkus, "dev-anna": devAnna, "dev-kid": devKid, "dev-ipad": devIpad}
	for _, u := range home.users {
		if id, ok := ids[u["id"].(string)]; ok {
			u["id"] = id
		}
	}
	for i, d := range home.devices {
		home.devices[i].DeviceID, home.devices[i].UserID = ids[d.DeviceID], ids[d.UserID]
	}
	return home
}

type controlHarness struct {
	*serveHarness
	phones *fakePhones
	home   *fakeHome
	curl   string
	secret string
}

// newControl: the bridge with its control channel; settings is
// chat-apps.json ("" = the owner only).
func newControl(t *testing.T, settings string) *controlHarness {
	t.Helper()
	f, home := newFakeHA(), hexFamily()
	home.install(t, f)
	s := newServe(t, f, settings)
	p := usePhones(t)
	phoneDiscover = discoverPhone
	saved := controlAskWait
	controlAskWait = 2 * time.Second
	t.Cleanup(func() { controlAskWait = saved })
	secret := randomHex(t, 32)
	file := filepath.Join(t.TempDir(), "control-secret")
	if err := os.WriteFile(file, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.b.enableControl(file); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(s.b.serveControl))
	t.Cleanup(srv.Close)
	return &controlHarness{serveHarness: s, phones: p, home: home, curl: srv.URL + controlPrefix, secret: secret}
}

func (c *controlHarness) call(method, path string, body any, secret string) (int, map[string]any) {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = strings.NewReader(string(raw))
	}
	req, _ := http.NewRequest(method, c.curl+path, reader)
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (c *controlHarness) post(body map[string]any) (int, map[string]any) {
	c.t.Helper()
	return c.call(http.MethodPost, "phone", body, c.secret)
}

// finish waits for a job's end and returns its status.
func (c *controlHarness) finish(id any) string {
	c.t.Helper()
	var status string
	waitFor(c.t, "the phone change to end", func() bool {
		_, state := c.call(http.MethodGet, "phone/"+id.(string), nil, c.secret)
		status, _ = state["status"].(string)
		return status != "running"
	})
	return status
}

func (c *controlHarness) links() map[string]*phoneLink {
	c.t.Helper()
	byUser, ok := readBridgeLinks(c.b.linksFile())
	if !ok {
		c.t.Fatal("phone-links.json unreadable")
	}
	return byUser
}

func (c *controlHarness) seed(user string, link *phoneLink) {
	c.t.Helper()
	raw, _ := json.Marshal(map[string]*phoneLink{user: link})
	if err := os.WriteFile(c.b.linksFile(), raw, 0o600); err != nil {
		c.t.Fatal(err)
	}
}

func controlCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

var markusIPhone = &phoneLink{Service: "mobile_app_markuss_iphone", DeviceID: devMarkus, UserID: ownerHex, Name: "Markus’s iPhone"}

// A move asks the phone linked now first; only after its OK does the new
// phone get the link question, and only a tap from the user it is listed
// under links it. The list holds that user's own phones, never anyone else's.
func TestServeControlMovesOnlyWithTheOldPhonesOK(t *testing.T) {
	c := newControl(t, "")
	c.seed(ownerHex, markusIPhone)
	code, list := c.call(http.MethodGet, "phones?user="+ownerHex, nil, c.secret)
	phones, _ := list["phones"].([]any)
	link, _ := list["link"].(map[string]any)
	if code != http.StatusOK || len(phones) != 1 || phones[0].(map[string]any)["device_id"] != devIpad || link["state"] != "ready" || link["name"] != "Markus’s iPhone" {
		t.Fatalf("phones: %d %v", code, list)
	}
	if code, body := c.post(map[string]any{"user": ownerHex, "action": "move", "phone": map[string]any{"device_id": devAnna}}); code != http.StatusConflict || controlCode(body) != "PHONE_NOT_ELIGIBLE" {
		t.Fatalf("someone else's phone: %d %v", code, body)
	}
	c.phones.tapBy = tapFrom("mobile_app_markuss_iphone", ownerHex, false)
	_, body := c.post(map[string]any{"user": ownerHex, "action": "move", "phone": map[string]any{"device_id": devIpad}})
	if status := c.finish(body["job"]); status != "declined" || *c.links()[ownerHex] != *markusIPhone || strings.Join(c.phones.services, ",") != "mobile_app_markuss_iphone,mobile_app_markuss_iphone" {
		t.Fatalf("declined on the old phone: %s %v %v", status, c.links(), c.phones.services)
	}
	c.phones.mu.Lock()
	title, message := c.phones.sent[0]["title"], c.phones.sent[0]["message"].(string)
	c.phones.services, c.phones.sent = nil, nil
	c.phones.tapBy = func(service, ok, no string) []phoneEvent {
		if service == "mobile_app_family_ipad" {
			return []phoneEvent{tap(ok, annaHex, "REMOTE")} // the iPad's app signed in as Anna
		}
		return tapFrom("mobile_app_markuss_iphone", ownerHex, true)(service, ok, no)
	}
	c.phones.mu.Unlock()
	if title != "Move chat-app confirmations?" || !strings.Contains(message, "will ask “Family iPad” instead.") || !strings.Contains(message, "Requested by the Home Assistant owner.") {
		t.Fatalf("move ask: %q %q", title, message)
	}
	_, body = c.post(map[string]any{"user": ownerHex, "action": "move", "phone": map[string]any{"device_id": devIpad}})
	if status := c.finish(body["job"]); status != "rejected" || *c.links()[ownerHex] != *markusIPhone {
		t.Fatalf("a tap from another user: %s %v", status, c.links())
	}
	c.phones.mu.Lock()
	c.phones.services, c.phones.sent = nil, nil
	c.phones.tapBy = func(service, ok, no string) []phoneEvent { return []phoneEvent{tap(ok, ownerHex, "REMOTE")} }
	c.phones.mu.Unlock()
	_, body = c.post(map[string]any{"user": ownerHex, "action": "move", "phone": map[string]any{"device_id": devIpad}})
	got := c.links()[ownerHex]
	if status := c.finish(body["job"]); status != "done" {
		t.Fatalf("move: %s", status)
	}
	if got = c.links()[ownerHex]; got == nil || got.DeviceID != devIpad || got.UserID != ownerHex || got.Service != "mobile_app_family_ipad" {
		t.Fatalf("link after the move: %+v", got)
	}
	c.phones.mu.Lock()
	defer c.phones.mu.Unlock()
	if strings.Join(c.phones.services, ",") != "mobile_app_markuss_iphone,mobile_app_markuss_iphone,mobile_app_family_ipad,mobile_app_family_ipad" ||
		c.phones.sent[2]["title"] != "Get chat-app confirmations?" || c.phones.sent[3]["title"] != "Linked to HA NOVA" {
		t.Fatalf("asks: %v %v", c.phones.services, c.phones.sent)
	}
	if strings.Contains(c.logs.String(), "HANOVA_") || strings.Contains(c.logs.String(), c.secret) {
		t.Fatal("an id or the secret reached the log")
	}
}

// A stop needs the old phone's OK; with no phone there is nothing to stop.
func TestServeControlStopNeedsTheOldPhone(t *testing.T) {
	c := newControl(t, "")
	c.seed(ownerHex, markusIPhone)
	c.phones.tapBy = tapFrom("mobile_app_markuss_iphone", ownerHex, true)
	_, body := c.post(map[string]any{"user": ownerHex, "action": "stop"})
	if status := c.finish(body["job"]); status != "done" || len(c.links()) != 0 {
		t.Fatalf("stop: %s %v", status, c.links())
	}
	c.phones.mu.Lock()
	message := c.phones.sent[0]["message"].(string)
	c.phones.mu.Unlock()
	if !strings.Contains(message, "four-digit code instead") {
		t.Fatalf("stop ask with chat codes on: %q", message)
	}
	if code, body := c.post(map[string]any{"user": ownerHex, "action": "stop"}); code != http.StatusConflict || controlCode(body) != "NO_PHONE" {
		t.Fatalf("nothing to stop: %d %v", code, body)
	}
}

// A linked phone that can never answer again (removed from Home Assistant)
// is replaced or stopped only with override, the NOVA page's click; one
// that is only unreachable now is never replaced.
func TestServeControlGonePhoneOnlyWithOverride(t *testing.T) {
	c := newControl(t, "")
	gone := &phoneLink{Service: "mobile_app_old_phone", DeviceID: "d00000000000000000000000000000ff", UserID: ownerHex, Name: "Old Phone"}
	c.seed(ownerHex, gone)
	move := map[string]any{"user": ownerHex, "action": "move", "phone": map[string]any{"device_id": devIpad}}
	if code, body := c.post(move); code != http.StatusConflict || controlCode(body) != "OLD_PHONE_GONE" || c.phones.sentCount() != 0 {
		t.Fatalf("without override: %d %v", code, body)
	}
	if code, list := c.call(http.MethodGet, "phones?user="+ownerHex, nil, c.secret); code != http.StatusOK || list["link"].(map[string]any)["state"] != "gone" || list["link"].(map[string]any)["why"] != "removed" {
		t.Fatalf("gone link: %v", list)
	}
	c.phones.tapBy = tapFrom("mobile_app_family_ipad", ownerHex, true)
	move["override"] = true
	_, body := c.post(move)
	if status := c.finish(body["job"]); status != "done" || c.links()[ownerHex].DeviceID != devIpad || c.phones.services[0] != "mobile_app_family_ipad" {
		t.Fatalf("override: %s %v %v", status, c.links(), c.phones.services)
	}
	c.seed(ownerHex, gone)
	if code, body := c.post(map[string]any{"user": ownerHex, "action": "stop", "override": true}); code != http.StatusOK || body["status"] != "done" || len(c.links()) != 0 {
		t.Fatalf("stop a gone phone: %d %v", code, body)
	}
	c.seed(ownerHex, markusIPhone)
	c.f.mu.Lock()
	c.home.notify = []string{"mobile_app_family_ipad"} // the iPhone's app signed out: its notify service is gone
	c.home.install(t, c.f)
	c.f.mu.Unlock()
	if code, body := c.post(map[string]any{"user": ownerHex, "action": "stop", "override": true}); code != http.StatusConflict || controlCode(body) != "OLD_PHONE_UNAVAILABLE" || len(c.links()) != 1 {
		t.Fatalf("unreachable is not gone: %d %v", code, body)
	}
}

// adopt hands over a first phone without a question and never replaces
// one; drop only removes a user no longer allowed; one change per user at
// a time; an unknown job is lost.
func TestServeControlAdoptDropAndOneAtATime(t *testing.T) {
	c := newControl(t, `{"allowed_users":["`+ownerHex+`","`+annaHex+`"]}`)
	adopt := map[string]any{"user": annaHex, "action": "adopt", "phone": map[string]any{"service": "mobile_app_annas_pixel", "device_id": devAnna, "name": "Anna’s Pixel"}}
	if code, body := c.post(adopt); code != http.StatusOK || body["status"] != "done" || c.links()[annaHex].UserID != annaHex || c.phones.sentCount() != 0 {
		t.Fatalf("adopt: %d %v %v", code, body, c.links())
	}
	if code, body := c.post(adopt); code != http.StatusConflict || controlCode(body) != "PHONE_EXISTS" {
		t.Fatalf("adopt over a link: %d %v", code, body)
	}
	if code, body := c.post(map[string]any{"user": kidHex, "action": "adopt", "phone": adopt["phone"]}); code != http.StatusForbidden || controlCode(body) != "USER_NOT_ALLOWED" {
		t.Fatalf("a user not allowed: %d %v", code, body)
	}
	if code, body := c.post(map[string]any{"user": annaHex, "action": "drop"}); code != http.StatusConflict || controlCode(body) != "USER_STILL_ALLOWED" || c.links()[annaHex] == nil {
		t.Fatalf("drop an allowed user: %d %v", code, body)
	}
	if err := os.WriteFile(filepath.Join(c.data, "chat-apps.json"), []byte(`{"allowed_users":["`+ownerHex+`"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, body := c.post(map[string]any{"user": annaHex, "action": "drop"}); code != http.StatusOK || len(c.links()) != 0 {
		t.Fatalf("drop: %d %v %v", code, body, c.links())
	}
	move := map[string]any{"user": ownerHex, "action": "move", "phone": map[string]any{"device_id": devIpad}}
	code, first := c.post(move) // no link yet: only the new phone asks, and nobody taps
	if code != http.StatusAccepted {
		t.Fatalf("move: %d %v", code, first)
	}
	if code, body := c.post(move); code != http.StatusConflict || controlCode(body) != "PHONE_CHANGE_RUNNING" {
		t.Fatalf("a second change: %d %v", code, body)
	}
	if _, state := c.call(http.MethodGet, "phone/"+strings.Repeat("e", 32), nil, c.secret); state["status"] != "lost" {
		t.Fatalf("unknown job: %v", state)
	}
	if status := c.finish(first["job"]); status != "timeout" || len(c.links()) != 0 || c.phones.services[0] != "mobile_app_family_ipad" {
		t.Fatalf("no tap: %s %v", status, c.phones.services)
	}
}

// Both listeners bind before either serves: a busy control port stops the
// start and frees the /mcp port again; cancelling stops both.
func TestServeListensOnBothAndStopsTogether(t *testing.T) {
	c := newControl(t, "")
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := free.Addr().String()
	free.Close()
	if err := c.b.listen(context.Background(), addr, busy.Addr().String()); err == nil {
		t.Fatal("a busy control port started serve")
	}
	again, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the /mcp port stayed bound after the failed start: %v", err)
	}
	again.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.b.listen(ctx, "127.0.0.1:0", "127.0.0.1:0") }()
	waitFor(t, "both listeners", func() bool { return strings.Count(c.logs.String(), "listening on") == 2 })
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stopped with %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("serve did not stop")
	}
}

// The control channel answers only its own secret, never the integration's,
// and is never served on the /mcp listener; it binds loopback only.
func TestServeControlNeedsItsOwnSecretAndLoopback(t *testing.T) {
	c := newControl(t, "")
	for _, secret := range []string{"", c.serveHarness.secret, c.secret + "0"} {
		if code, _ := c.call(http.MethodGet, "phones?user="+ownerHex, nil, secret); code != http.StatusUnauthorized {
			t.Fatalf("secret %q: %d", secret, code)
		}
	}
	if status, _ := c.raw(http.MethodGet, nil, ""); status == http.StatusOK {
		t.Fatal("unexpected /mcp GET")
	}
	req, _ := http.NewRequest(http.MethodGet, strings.TrimSuffix(c.url, "/mcp")+controlPrefix+"phones?user="+ownerHex, nil)
	req.Header.Set("Authorization", "Bearer "+c.secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("control on the /mcp listener: %d", resp.StatusCode)
	}
	same := filepath.Join(t.TempDir(), "same")
	if err := os.WriteFile(same, []byte(c.serveHarness.secret), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.b.enableControl(same); err == nil {
		t.Fatal("the integration's secret was accepted for control")
	}
	for addr, ok := range map[string]bool{"127.0.0.1:8795": true, "[::1]:8795": true, "0.0.0.0:8795": false, "localhost:8795": false, "192.168.1.9:8795": false, ":8795": false} {
		if (controlAddr(addr) == nil) != ok {
			t.Errorf("controlAddr(%q) ok=%v", addr, !ok)
		}
	}
}
