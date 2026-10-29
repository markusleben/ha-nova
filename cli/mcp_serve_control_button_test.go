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
