package main

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// A link ask: the phone linked now confirms the move first, then the new
// phone confirms the link, both worded by the worker; anything but two
// approvals links nothing.
func TestGateWorkerPhoneLink(t *testing.T) {
	h, p, _ := familyLink(t)
	post := gateTestServer(t, h.paths)
	anna := map[string]any{"service": "mobile_app_annas_pixel", "device_id": "dev-anna", "user_id": "anna-id"}
	markus := map[string]any{"service": "mobile_app_markuss_iphone", "device_id": "dev-markus", "user_id": "owner-id"}
	kid := map[string]any{"service": "mobile_app_kids_phone", "device_id": "dev-kid", "user_id": "kid-id"}
	both := func(markusYes bool) func(string, string, string) []phoneEvent {
		return func(service, ok, no string) []phoneEvent {
			if service == "mobile_app_markuss_iphone" {
				return tapFrom(service, "owner-id", markusYes)(service, ok, no)
			}
			return tapFrom("mobile_app_annas_pixel", "anna-id", true)(service, ok, no)
		}
	}
	link := func(current map[string]any, to map[string]any) (int, map[string]any) {
		body := map[string]any{"words": "link", "device": "Markus’ MacBook", "link": to, "wait_ms": 3000}
		if current != nil {
			body["current"] = current
		}
		return post("/phone", gateTestSecret, body)
	}
	titles := func() []string {
		var out []string
		for _, sent := range p.sent {
			out = append(out, sent["title"].(string))
		}
		return out
	}

	p.tapBy, p.sent = both(true), nil
	if status, out := link(nil, anna); status != http.StatusOK || out["answer"] != "approved" || out["user_id"] != "anna-id" {
		t.Errorf("a first link: HTTP %d %v", status, out)
	}
	if got := titles(); !slices.Equal(got, []string{"Get HA NOVA confirmations?", "Approved: Get HA NOVA confirmations"}) || !strings.Contains(p.sent[0]["message"].(string), "MacBook") {
		t.Errorf("a first link asks only the new phone, naming the device, then says it was approved: %v %v", got, p.sent)
	}

	p.sent = nil
	if status, out := link(markus, anna); status != http.StatusOK || out["answer"] != "approved" || out["user_id"] != "anna-id" {
		t.Errorf("a move: HTTP %d %v", status, out)
	}
	if got := titles(); !slices.Equal(got, []string{"Move HA NOVA confirmations?", "Approved: Move HA NOVA confirmations", "Get HA NOVA confirmations?", "Approved: Get HA NOVA confirmations"}) ||
		!strings.Contains(p.sent[0]["message"].(string), "Pixel") {
		t.Errorf("a move asks the phone linked now first, naming the new one: %v %v", got, p.sent)
	}

	p.tapBy, p.sent = both(false), nil
	if status, out := link(markus, anna); status != http.StatusOK || out["answer"] != "declined" || !slices.Equal(titles(), []string{"Move HA NOVA confirmations?", "Cancelled: Move HA NOVA confirmations"}) {
		t.Errorf("the phone linked now cancels: HTTP %d %v, sent %v", status, out, titles())
	}

	p.tapBy, p.sent = both(true), nil
	for name, tc := range map[string][2]map[string]any{"the phone linked now is no administrator's": {kid, anna}, "the new phone is no administrator's": {nil, kid}} {
		p.sent = nil
		if status, out := link(tc[0], tc[1]); status != http.StatusOK || out["answer"] != "unavailable" || len(p.sent) != 0 {
			t.Errorf("%s: HTTP %d %v, sent %d", name, status, out, len(p.sent))
		}
	}

	p.sent = nil
	if status, out := link(nil, map[string]any{"service": "mobile_app_annas_pixel", "device_id": "dev-anna", "user_id": "owner-id"}); status != http.StatusOK ||
		out["answer"] != "unavailable" || out["why"] != "not_eligible" || len(p.sent) != 0 {
		t.Errorf("another administrator's phone under the owner's user: HTTP %d %v, sent %d", status, out, len(p.sent))
	}

	for name, body := range map[string]map[string]any{
		"words and text":             {"words": "link", "device": "Mac", "lang": "en", "title": "T", "body": "B", "button": "OK", "link": anna, "wait_ms": 3000},
		"no device":                  {"words": "link", "link": anna, "wait_ms": 3000},
		"other words":                {"words": "unlink", "device": "Mac", "link": anna, "wait_ms": 3000},
		"a current that is no phone": {"words": "link", "device": "Mac", "link": anna, "current": map[string]any{"service": "persistent_notification", "device_id": "d", "user_id": "u"}, "wait_ms": 3000},
	} {
		if status, _ := post("/phone", gateTestSecret, body); status != http.StatusBadRequest {
			t.Errorf("%s: HTTP %d, want 400", name, status)
		}
	}
}
