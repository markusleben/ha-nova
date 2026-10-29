package main

import (
	"strings"
	"testing"
)

// A job names the button its phone shows, from the confirmation catalog in
// Home Assistant's language, so the NOVA page quotes the phone's own word.
func TestServeControlJobNamesItsButton(t *testing.T) {
	title := func(c *controlHarness) string {
		actions := c.lastSent()["data"].(map[string]any)["actions"].([]any)
		return actions[0].(map[string]any)["title"].(string)
	}
	t.Run("German move", func(t *testing.T) {
		c := newControl(t, "")
		c.f.mu.Lock()
		c.f.config = map[string]any{"language": "de"}
		c.f.mu.Unlock()
		c.seed(ownerHex, markusIPhone)
		_, body := c.post(map[string]any{"user": ownerHex, "action": "move", "phone": map[string]any{"device_id": devIpad}})
		button := func() any { return c.job(body["job"])["button"] }
		if got := button(); got != "Wechseln" {
			t.Fatalf("the old phone's step: %v", got)
		}
		waitFor(t, "the iPhone's question", func() bool { return c.phones.sentCount() == 1 })
		if !strings.Contains(title(c), "Wechseln") {
			t.Fatalf("the iPhone shows %q", title(c))
		}
		ok, _ := c.phones.buttons(t)
		c.phones.events <- tap(ok, ownerHex, "REMOTE")
		waitFor(t, "the iPad's question", func() bool { return c.phones.sentCount() == 3 })
		if got := button(); got != "Erlauben" || !strings.Contains(title(c), "Erlauben") {
			t.Fatalf("the new phone's step: %v, the iPad shows %q", got, title(c))
		}
		if status := c.finish(body["job"]); status != "timeout" || button() != "Erlauben" {
			t.Fatalf("at the end: %s %v", status, button())
		}
	})
	t.Run("English stop", func(t *testing.T) {
		c := newControl(t, "")
		c.seed(ownerHex, markusIPhone)
		_, body := c.post(map[string]any{"user": ownerHex, "action": "stop"})
		if got := c.job(body["job"])["button"]; got != "Stop" {
			t.Fatalf("a stop: %v", got)
		}
		c.finish(body["job"])
	})
}

// Below Manage everything a chat app deletes nothing: the questions and
// replies on that user's phone name what asks it there; the owner's keep
// "Unlocks and deletes".
func TestServeControlPhoneTextsFollowTheLevel(t *testing.T) {
	sent := func(c *controlHarness) string {
		c.phones.mu.Lock()
		defer c.phones.mu.Unlock()
		var all []string
		for _, s := range c.phones.sent {
			message, _ := s["message"].(string)
			all = append(all, message)
		}
		return strings.Join(all, "\n")
	}
	c := newControl(t, levels([]string{ownerHex, kidHex}, map[string]string{kidHex: "use"}))
	c.phones.tapBy = tapFrom("mobile_app_kids_phone", kidHex, true)
	_, body := c.post(map[string]any{"user": kidHex, "action": "move", "phone": map[string]any{"device_id": devKid}})
	if status := c.finish(body["job"]); status != "done" {
		t.Fatalf("link: %s", status)
	}
	_, body = c.post(map[string]any{"user": kidHex, "action": "stop"})
	if status := c.finish(body["job"]); status != "done" {
		t.Fatalf("stop: %s", status)
	}
	kid := sent(c)
	for _, want := range []string{
		"Unlocking, opening a garage door or gate, and turning off the alarm from Claude and ChatGPT will then ask you first.",
		"Unlocking, opening a garage door or gate, and turning off the alarm from Claude and ChatGPT now ask you.",
		"Unlocking, opening a garage door or gate, and turning off the alarm from Claude and ChatGPT will stop working until a phone is linked again.",
	} {
		if !strings.Contains(kid, want) {
			t.Errorf("the kid's phone never read %q:\n%s", want, kid)
		}
	}
	if strings.Contains(kid, "delete") {
		t.Errorf("deletes named at Use the home:\n%s", kid)
	}
	move := chatPhoneChangeAsk("de", &phoneTarget{Name: "Kids Tablet"}, false, true).body
	if !strings.HasPrefix(move, "Wenn Claude oder ChatGPT etwas entriegeln, ein Garagentor oder Tor öffnen oder die Alarmanlage ausschalten wollen, wird dann stattdessen „Kids Tablet“ gefragt.") || strings.Contains(move, "löschen") {
		t.Errorf("a German move at Use the home: %q", move)
	}
	owner := newControl(t, "")
	owner.phones.tapBy = tapFrom("mobile_app_markuss_iphone", ownerHex, true)
	_, body = owner.post(map[string]any{"user": ownerHex, "action": "move", "phone": map[string]any{"device_id": devMarkus}})
	owner.finish(body["job"])
	if got := sent(owner); !strings.Contains(got, "Unlocks and deletes from Claude and ChatGPT will then ask you first.") || strings.Contains(got, "garage door") {
		t.Errorf("the owner's phone read:\n%s", got)
	}
}
