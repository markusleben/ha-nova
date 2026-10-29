package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// The control channel's guards (mcp_serve_control.go): each re-check after
// a tap, each read that fails closed, and the links that can never work.

const bothAllowed = `{"allowed_users":["` + ownerHex + `","` + annaHex + `"]}`

var annaPixel = &phoneLink{Service: "mobile_app_annas_pixel", DeviceID: devAnna, UserID: annaHex, Name: "Anna’s Pixel"}

// settings rewrites chat-apps.json; t.Error, as a phone's tap calls it too.
func (c *controlHarness) settings(t *testing.T, settings string) {
	if err := os.WriteFile(filepath.Join(c.data, "chat-apps.json"), []byte(settings), 0o600); err != nil {
		t.Error(err)
	}
}

func (c *controlHarness) lastSent() map[string]any {
	c.phones.mu.Lock()
	defer c.phones.mu.Unlock()
	return c.phones.sent[len(c.phones.sent)-1]
}

func (c *controlHarness) sentTo() string {
	c.phones.mu.Lock()
	defer c.phones.mu.Unlock()
	return strings.Join(c.phones.services, ",")
}

// A Relay signed in as the owner can't tell a tap from HA NOVA itself: no
// phone is listed, and no link is asked for or written (as on a computer).
func TestServeControlRefusesAnOwnerTokenHome(t *testing.T) {
	c := newControl(t, bothAllowed)
	c.f.mu.Lock()
	c.home.relay = ownerHex
	c.home.install(t, c.f)
	c.f.mu.Unlock()
	if code, body := c.call(http.MethodGet, "phones?user="+annaHex, nil, c.secret); code != http.StatusConflict || controlCode(body) != "OWNER_TOKEN" {
		t.Fatalf("phones: %d %v", code, body)
	}
	for _, req := range []map[string]any{
		{"user": annaHex, "action": "move", "phone": map[string]any{"device_id": devAnna}},
		{"user": annaHex, "action": "adopt", "phone": map[string]any{"device_id": devAnna}},
		{"user": ownerHex, "action": "stop"},
	} {
		if code, body := c.post(req); code != http.StatusConflict || controlCode(body) != "OWNER_TOKEN" {
			t.Fatalf("%s: %d %v", req["action"], code, body)
		}
	}
	if len(c.links()) != 0 || c.phones.sentCount() != 0 {
		t.Fatalf("written or sent: %v %d", c.links(), c.phones.sentCount())
	}
}

// adopt takes the phone from Home Assistant: only one of the user's own
// phones that can confirm (the kid is at Ask only), stored as Home
// Assistant lists it, whatever the request says.
func TestServeControlAdoptChecksThePhone(t *testing.T) {
	c := newControl(t, levels([]string{ownerHex, annaHex, kidHex}, map[string]string{kidHex: "ask"}))
	for user, device := range map[string]string{annaHex: devMarkus, kidHex: devKid, ownerHex: "d00000000000000000000000000000ff"} {
		if code, body := c.post(map[string]any{"user": user, "action": "adopt", "phone": map[string]any{"device_id": device}}); code != http.StatusConflict || controlCode(body) != "PHONE_NOT_ELIGIBLE" {
			t.Fatalf("adopt %s for %s: %d %v", device, user, code, body)
		}
	}
	adopt := map[string]any{"user": ownerHex, "action": "adopt", "phone": map[string]any{"service": "mobile_app_annas_pixel", "device_id": devMarkus, "name": "Anna’s Pixel"}}
	if code, body := c.post(adopt); code != http.StatusOK || body["status"] != "done" {
		t.Fatalf("adopt: %d %v", code, body)
	}
	want := phoneLink{Service: "mobile_app_markuss_iphone", DeviceID: devMarkus, UserID: ownerHex, Name: "Markus’s iPhone", Platform: phoneIOS}
	if links := c.links(); len(links) != 1 || links[ownerHex] == nil || *links[ownerHex] != want || c.phones.sentCount() != 0 {
		t.Fatalf("adopted: %v", links)
	}
}

// A drop waits for a change of the same user that still runs, and a change
// whose user was removed meanwhile writes nothing: a dropped link never
// comes back.
func TestServeControlDropWaitsForARunningMove(t *testing.T) {
	c := newControl(t, bothAllowed)
	_, body := c.post(map[string]any{"user": annaHex, "action": "move", "phone": map[string]any{"device_id": devAnna}}) // no link yet: only her Pixel asks
	waitFor(t, "the Pixel's question", func() bool { return c.phones.sentCount() == 1 })
	c.settings(t, `{"allowed_users":["`+ownerHex+`"]}`)
	if code, drop := c.post(map[string]any{"user": annaHex, "action": "drop"}); code != http.StatusConflict || controlCode(drop) != "PHONE_CHANGE_RUNNING" {
		t.Fatalf("drop beside the move: %d %v", code, drop)
	}
	ok, _ := c.phones.buttons(t)
	c.phones.events <- tap(ok, annaHex, "REMOTE")
	if status := c.finish(body["job"]); status != "changed" || len(c.links()) != 0 || c.lastSent()["message"] != "The chat-app settings changed meanwhile. Nothing changed." {
		t.Fatalf("removed meanwhile: %s %v %v", status, c.links(), c.lastSent())
	}
	if code, drop := c.post(map[string]any{"user": annaHex, "action": "drop"}); code != http.StatusOK || drop["status"] != "done" {
		t.Fatalf("drop: %d %v", code, drop)
	}
}

// After the old phone's OK the bridge reads the settings again: a stop
// removes the link only while the user is still allowed and chat codes are
// as its question said; a move asks its new phone only while the user is
// still allowed.
func TestServeControlChecksTheSettingsAfterTheOldTap(t *testing.T) {
	for name, after := range map[string]string{"removed": `{"allowed_users":["` + ownerHex + `"]}`, "codes off": `{"allowed_users":["` + ownerHex + `","` + annaHex + `"],"chat_codes":false}`} {
		t.Run("stop, "+name, func(t *testing.T) {
			c := newControl(t, bothAllowed)
			c.seed(annaHex, annaPixel)
			c.phones.tapBy = func(service, ok, no string) []phoneEvent {
				c.settings(t, after)
				return []phoneEvent{tap(ok, annaHex, "REMOTE")}
			}
			_, body := c.post(map[string]any{"user": annaHex, "action": "stop"})
			if status := c.finish(body["job"]); status != "changed" || c.links()[annaHex] == nil || *c.links()[annaHex] != *annaPixel || c.lastSent()["message"] != "The chat-app settings changed meanwhile. Nothing changed." {
				t.Fatalf("%s: %v %v", status, c.links(), c.lastSent())
			}
		})
	}
	t.Run("move, removed", func(t *testing.T) {
		c := newControl(t, bothAllowed)
		c.seed(ownerHex, markusIPhone)
		c.phones.tapBy = func(service, ok, no string) []phoneEvent {
			c.settings(t, `{"allowed_users":["`+annaHex+`"]}`)
			return []phoneEvent{tap(ok, ownerHex, "REMOTE")}
		}
		_, body := c.post(map[string]any{"user": ownerHex, "action": "move", "phone": map[string]any{"device_id": devIpad}})
		if status := c.finish(body["job"]); status != "changed" || *c.links()[ownerHex] != *markusIPhone || c.sentTo() != "mobile_app_markuss_iphone,mobile_app_markuss_iphone" {
			t.Fatalf("%s: %v %s", status, c.links(), c.sentTo())
		}
	})
}

// After the new phone's tap the bridge reads again: an entry changed
// meanwhile writes nothing, and neither does a read that fails.
func TestServeControlChecksTheLinkAfterTheNewTap(t *testing.T) {
	for name, meanwhile := range map[string]string{"changed": `{"` + ownerHex + `":{"service":"mobile_app_markuss_iphone","device_id":"` + devMarkus + `","user_id":"` + ownerHex + `","name":"Markus’s iPhone"}}`, "unreadable": "{not json"} {
		t.Run(name, func(t *testing.T) {
			c := newControl(t, "")
			c.phones.tapBy = func(service, ok, no string) []phoneEvent {
				if err := os.WriteFile(c.b.linksFile(), []byte(meanwhile), 0o600); err != nil {
					t.Error(err)
				}
				return []phoneEvent{tap(ok, ownerHex, "REMOTE")}
			}
			_, body := c.post(map[string]any{"user": ownerHex, "action": "move", "phone": map[string]any{"device_id": devIpad}}) // no link yet: only the iPad asks
			status := c.finish(body["job"])
			want := map[string][2]string{"changed": {"changed", "The phone or its Home Assistant user changed meanwhile. Nothing changed."}, "unreadable": {"failed", "HA NOVA couldn't finish this. Nothing changed."}}[name]
			if raw, _ := os.ReadFile(c.b.linksFile()); status != want[0] || string(raw) != meanwhile || c.lastSent()["message"] != want[1] {
				t.Fatalf("%s %s %v", status, raw, c.lastSent())
			}
		})
	}
}

// A null entry, or one whose user_id is another user's, can never answer
// for this user: it shows as gone, and only override replaces it.
func TestServeControlForeignOrNullLinkIsGone(t *testing.T) {
	for name, link := range map[string]*phoneLink{"foreign": annaPixel, "null": nil} {
		t.Run(name, func(t *testing.T) {
			c := newControl(t, "")
			c.seed(ownerHex, link)
			_, list := c.call(http.MethodGet, "phones?user="+ownerHex, nil, c.secret)
			if l, _ := list["link"].(map[string]any); l["state"] != "gone" || l["why"] != "removed" {
				t.Fatalf("link: %v", list)
			}
			move := map[string]any{"user": ownerHex, "action": "move", "phone": map[string]any{"device_id": devIpad}}
			if code, body := c.post(move); code != http.StatusConflict || controlCode(body) != "OLD_PHONE_GONE" || c.phones.sentCount() != 0 {
				t.Fatalf("without override: %d %v", code, body)
			}
			c.phones.tapBy = tapFrom("mobile_app_family_ipad", ownerHex, true)
			move["override"] = true
			_, body := c.post(move)
			if status := c.finish(body["job"]); status != "done" || c.links()[ownerHex].DeviceID != devIpad || c.sentTo() != "mobile_app_family_ipad,mobile_app_family_ipad" {
				t.Fatalf("override: %s %v %s", status, c.links(), c.sentTo())
			}
		})
	}
}

// Unreadable phone links refuse every action and change nothing.
func TestServeControlFailsClosedOnUnreadableLinks(t *testing.T) {
	c := newControl(t, `{"allowed_users":["`+annaHex+`"]}`)
	if err := os.WriteFile(c.b.linksFile(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, body := c.call(http.MethodGet, "phones?user="+annaHex, nil, c.secret); code != http.StatusServiceUnavailable || controlCode(body) != "PHONES_UNREADABLE" {
		t.Fatalf("phones: %d %v", code, body)
	}
	for _, req := range []map[string]any{
		{"user": annaHex, "action": "adopt", "phone": map[string]any{"device_id": devAnna}},
		{"user": annaHex, "action": "move", "phone": map[string]any{"device_id": devAnna}},
		{"user": annaHex, "action": "stop"},
		{"user": ownerHex, "action": "drop"},
	} {
		want := map[bool]string{true: "LINKS_UNWRITABLE", false: "PHONES_UNREADABLE"}[req["action"] == "drop"]
		if code, body := c.post(req); code != http.StatusServiceUnavailable || controlCode(body) != want {
			t.Fatalf("%s: %d %v", req["action"], code, body)
		}
	}
	if raw, _ := os.ReadFile(c.b.linksFile()); string(raw) != "{not json" || c.phones.sentCount() != 0 {
		t.Fatalf("changed: %s %d", raw, c.phones.sentCount())
	}
}

// The list holds only the asking user's own phones that can confirm: never
// another user's, those of a user at Ask only, or the Relay user's.
func TestServeControlListsOnlyTheUsersOwnPhones(t *testing.T) {
	c := newControl(t, levels([]string{ownerHex, annaHex, kidHex}, map[string]string{kidHex: "ask"}))
	listed := func(user string) string {
		code, list := c.call(http.MethodGet, "phones?user="+user, nil, c.secret)
		if code != http.StatusOK {
			t.Fatalf("phones for %s: %d %v", user, code, list)
		}
		var ids []string
		phones, _ := list["phones"].([]any)
		for _, p := range phones {
			ids = append(ids, p.(map[string]any)["device_id"].(string))
		}
		return strings.Join(ids, ",")
	}
	for user, want := range map[string]string{ownerHex: devIpad + "," + devMarkus, annaHex: devAnna, kidHex: ""} {
		if got := listed(user); got != want {
			t.Errorf("%s: %q, want %q", user, got, want)
		}
	}
	c.f.mu.Lock()
	c.home.relay = annaHex // the Relay signs in as Anna: her phones never confirm
	c.home.install(t, c.f)
	c.f.mu.Unlock()
	if got := listed(annaHex); got != "" {
		t.Errorf("the Relay user's phones: %q", got)
	}
}

// A job names the phone it asks, that phone's platform, and a move's new
// phone, from the 202 on, and at the end the phone that ended it.
func TestServeControlJobNamesItsPhones(t *testing.T) {
	c := newControl(t, "")
	c.seed(ownerHex, markusIPhone)
	_, body := c.post(map[string]any{"user": ownerHex, "action": "move", "phone": map[string]any{"device_id": devIpad}})
	state := func() string {
		s := c.job(body["job"])
		return fmt.Sprint(s["status"], " ", s["step"], " ", s["phone"], " ", s["platform"], " ", s["to"])
	}
	if got := state(); got != "running old Markus’s iPhone ios Family iPad" {
		t.Fatalf("at the start: %q", got)
	}
	waitFor(t, "the iPhone's question", func() bool { return c.phones.sentCount() == 1 })
	ok, _ := c.phones.buttons(t)
	c.phones.events <- tap(ok, ownerHex, "REMOTE")
	waitFor(t, "the iPad's question", func() bool { return state() == "running new Family iPad  Family iPad" })
	if status := c.finish(body["job"]); status != "timeout" || state() != "timeout new Family iPad  Family iPad" {
		t.Fatalf("no tap on the iPad: %s %q", status, state())
	}
}

// A change runs beside the chat: while its phones are asked, the same
// user's chat calls answer, and neither side reads the other's verb state
// (the race detector watches).
func TestServeControlJobRunsBesideChatCalls(t *testing.T) {
	c := newControl(t, "")
	c.seed(ownerHex, markusIPhone)
	chatted := make(chan struct{})
	c.phones.tapBy = func(service, ok, no string) []phoneEvent {
		<-chatted // each phone answers only after chat calls ran beside its question
		return []phoneEvent{tap(ok, ownerHex, "REMOTE")}
	}
	call, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "ha_plan",
		"arguments": map[string]any{"kind": "call", "service": "lock.unlock", "entity_ids": []string{"lock.front"}}}})
	stop, done := make(chan struct{}), make(chan []int)
	go func() {
		var codes []int
		defer func() { done <- codes }()
		for {
			select {
			case <-stop:
				return
			default:
			}
			req, _ := http.NewRequest(http.MethodPost, c.url, bytes.NewReader(call))
			for k, values := range c.as(ownerHex) {
				for _, v := range values {
					req.Header.Add(k, v)
				}
			}
			code := 0
			if resp, err := http.DefaultClient.Do(req); err == nil {
				code = resp.StatusCode
				resp.Body.Close()
			}
			if codes = append(codes, code); len(codes) == 3 {
				close(chatted)
			}
		}
	}()
	_, body := c.post(map[string]any{"user": ownerHex, "action": "move", "phone": map[string]any{"device_id": devIpad}})
	status := c.finish(body["job"])
	close(stop)
	codes := <-done
	if status != "done" || c.links()[ownerHex].DeviceID != devIpad {
		t.Fatalf("move: %s %v", status, c.links())
	}
	if len(codes) < 3 || slices.ContainsFunc(codes, func(code int) bool { return code != http.StatusOK }) {
		t.Fatalf("chat calls: %v", codes)
	}
}

// Every chat-app phone title fits Android's pop-up, which shows about 30
// characters.
func TestChatPhoneTitlesFitThePopUp(t *testing.T) {
	for lang, catalog := range confirmCatalogs {
		for key, entry := range catalog {
			for _, text := range setupEntryTexts(entry) {
				if strings.HasPrefix(key, "chat_phone.") && strings.HasSuffix(key, "title") && utf8.RuneCountInString(text) > 28 {
					t.Errorf("%s %s: %q has %d characters", lang, key, text, utf8.RuneCountInString(text))
				}
			}
		}
	}
}
