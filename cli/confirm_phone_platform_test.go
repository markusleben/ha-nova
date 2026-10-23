package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The notification per platform. Android's push relay copies each action's
// authenticationRequired into FCM data, which takes strings only: a bool
// there failed the whole push (Home Assistant only logs it), so the link
// question never reached an Android phone while the button-less result
// did. iOS reads a bool; an unknown phone gets the bool too (no
// notification beats one without the unlock).
func TestPhonePayloadPerPlatform(t *testing.T) {
	deadline := time.Now().Add(90 * time.Second)
	for _, c := range []struct {
		maker, platform, hint string
		unlock                any
	}{
		{"Apple", phoneIOS, "Press and hold to answer.", true},
		{"Google", phoneAndroid, "Pull down to answer.", "true"},
		{"samsung", phoneAndroid, "Pull down to answer.", "true"},
		{" apple ", phoneIOS, "Press and hold to answer.", true}, // any doubt keeps the unlock
		{"", "", "Press and hold (iPhone) or pull down (Android) to answer.", true},
	} {
		target := phoneTarget{Maker: c.maker}
		if target.platform() != c.platform {
			t.Fatalf("%q: platform %q", c.maker, target.platform())
		}
		link := phonePayload(phoneLinkAsk(), target.platform(), "tag-1", "OK", "NO", deadline)
		data := link["data"].(map[string]any)
		actions := data["actions"].([]any)
		confirm := actions[0].(map[string]any)
		want := phoneRequested() + " " + c.hint
		if c.platform == phoneAndroid { // a collapsed Android notification shows one line: the hint first
			want = c.hint + " " + phoneRequested()
		}
		if link["message"] != want || confirm["authenticationRequired"] != c.unlock || len(actions) != 2 ||
			actions[1].(map[string]any)["authenticationRequired"] != nil || data["priority"] != "high" || data["tag"] != "tag-1" {
			t.Fatalf("%q link: %v", c.maker, link)
		}
		// Android fixes a channel's importance when its first notification
		// arrives: the question and the result both create it as high (pop-ups on).
		result := phoneResultPayload("Done", "Home Assistant confirms it.", "tag-1")["data"].(map[string]any)
		for _, d := range []map[string]any{data, result} {
			if d["channel"] != "HA NOVA" || d["importance"] != "high" {
				t.Fatalf("%q channel: %v", c.maker, d)
			}
		}
		if data["push"].(map[string]any)["interruption-level"] != "active" || result["push"].(map[string]any)["interruption-level"] != "active" {
			t.Fatalf("%q iOS level: %v %v", c.maker, data["push"], result["push"])
		}
		// The ids die with the wait: FCM's ttl in milliseconds (the relay drops 0), Android's timeout in seconds.
		if ttl := data["ttl"].(int64); ttl < 85_000 || ttl > 90_000 || data["timeout"].(int) < 85 || data["timeout"].(int) > 90 {
			t.Fatalf("%q ttl %v timeout %v", c.maker, data["ttl"], data["timeout"])
		}
		// A plan's Confirm needs no unlock on any platform (an Apple Watch must answer it).
		plan := phonePayload(phonePlanAsk([3]string{"Unlock", "“Front Door”"}, false), target.platform(), "tag-2", "OK", "NO", deadline)
		if _, set := plan["data"].(map[string]any)["actions"].([]any)[0].(map[string]any)["authenticationRequired"]; set {
			t.Fatalf("%q plan asks for the unlock: %v", c.maker, plan)
		}
		// Android data: everything the relay copies from an action is a string.
		if c.platform == phoneAndroid {
			for _, a := range actions {
				for key, v := range a.(map[string]any) {
					if _, ok := v.(string); !ok && key != "destructive" { // the relay drops destructive (iOS only)
						t.Fatalf("Android action %s is %T", key, v)
					}
				}
			}
		}
	}
}

// How to show the buttons, said on the computer and in the chat.
func TestPhoneTapLinePerPlatform(t *testing.T) {
	for platform, want := range map[string]string{
		phoneIOS:     "Press and hold the notification, then tap “Allow”.",
		phoneAndroid: "Pull the notification down, or tap its arrow, then tap “Allow”.",
		"":           "Tap “Allow” in the notification. To see the buttons, press and hold the notification on an iPhone, or pull it down on Android.",
	} {
		if got := phoneTapLine(platform, "Allow"); got != want {
			t.Errorf("%q: %q", platform, got)
		}
	}
	if got := sayPhoneAsk("Unlock", phoneIOS); got != "Confirm on your phone. Press and hold the notification, then tap “Unlock”." {
		t.Errorf("say: %q", got)
	}
	if got := sayWindowPhoneAsk("Unlock", phoneAndroid); got != "Confirm in the window on this computer or on your phone. On the phone: Pull the notification down, or tap its arrow, then tap “Unlock”. The first answer counts." {
		t.Errorf("say both: %q", got)
	}
	if got := fmtSent("Bene 14", phoneAndroid); got != "Sent to “Bene 14”. Pull the notification down, or tap its arrow, then tap “Allow”. Do it only if you started this." {
		t.Errorf("sent: %q", got)
	}
}

func fmtSent(name, platform string) string {
	return strings.TrimSpace(strings.Replace(strings.Replace(phoneSentText, "%s", name, 1), "%s", phoneTapLine(platform, "Allow"), 1))
}

// A plan's say comes before any lookup: the platform stored with the link
// (one for every linked home, else the line true on both).
func TestLinkedPlatformFromTheStoredLinks(t *testing.T) {
	for _, c := range []struct {
		links map[string]phoneLink
		want  string
	}{
		{map[string]phoneLink{"o1": {Service: "mobile_app_a", Platform: phoneAndroid}}, phoneAndroid},
		{map[string]phoneLink{"o1": {Service: "mobile_app_a", Platform: phoneIOS}, "o2": {Service: "mobile_app_b", Platform: phoneIOS}}, phoneIOS},
		{map[string]phoneLink{"o1": {Service: "mobile_app_a", Platform: phoneIOS}, "o2": {Service: "mobile_app_b", Platform: phoneAndroid}}, ""},
		{map[string]phoneLink{"o1": {Service: "mobile_app_a"}, "o2": {Service: "mobile_app_b", Platform: phoneAndroid}}, ""}, // a link from before 0.27.2
		{map[string]phoneLink{"o1": {Service: "mobile_app_a"}}, ""},
	} {
		raw, _ := json.Marshal(map[string]any{"schema_version": 1, "confirm_phone": c.links})
		if got := linkedPlatform(writeTestConfigFile(t, string(raw))); got != c.want {
			t.Errorf("%v: %q, want %q", c.links, got, c.want)
		}
	}
}

// A phone linked as an iPhone stays one for Move and Stop: a maker changed
// later in Home Assistant never turns its unlock into Android's string.
func TestLinkedIPhoneKeepsItsUnlock(t *testing.T) {
	home := phoneHome{owner: "o", relay: "r", users: map[string]haUser{"u": {ID: "u", IsActive: true, GroupIDs: []string{"system-admin"}}},
		linked: &phoneTarget{Service: "mobile_app_x", DeviceID: "d", Maker: "samsung"}}
	for stored, want := range map[string]string{phoneIOS: phoneIOS, phoneAndroid: phoneAndroid, "": phoneAndroid} {
		setup, status := home.status(map[string]phoneEntry{"o": {link: &phoneLink{Service: "mobile_app_x", DeviceID: "d", UserID: "u", Platform: stored}}}, nil)
		if status != phoneReady || setup.targets[0].platform() != want {
			t.Errorf("stored %q: %v %q", stored, status, setup.targets[0].platform())
		}
	}
}

// A chat app's waiting say names the asked phone's gesture once known.
func TestWaitingForPhoneSaysHowToTap(t *testing.T) {
	tap := phoneTapLine(phoneAndroid, "Unlock")
	for want, line := range map[string]*string{tap: &tap, "press and hold it on an iPhone, or pull it down on Android": nil} {
		if say := waitingForPhone("p_1", false, line)["structuredContent"].(map[string]any)["say"].(string); !strings.Contains(strings.ToLower(say), strings.ToLower(want)) {
			t.Errorf("say %q lacks %q", say, want)
		}
	}
}

// Nothing showed on the phone: one line on where its notifications turn on,
// per platform, in the terminal (after phoneNothingAfter and at the
// timeout) and in a timed-out plan's next step.
func TestPhoneNothingArrivedHint(t *testing.T) {
	for platform, want := range map[string]string{
		phoneIOS:     "Nothing on your phone? Open Settings → Notifications → Home Assistant, and turn on Allow Notifications.",
		phoneAndroid: "Nothing on your phone? Open your phone's settings → Apps → Home Assistant → Notifications → HA NOVA, and turn on notifications and pop-ups.",
		"":           "Nothing on your phone? Open your phone's settings, find the Home Assistant app, and turn on its notifications.",
	} {
		if got := phoneNothingHint(platform); got != want {
			t.Errorf("%q: %q", platform, got)
		}
	}
	if got := (&phoneAsking{setup: phoneSetup{targets: []phoneTarget{{Maker: "Google"}}}}).nothingLine(); !strings.HasSuffix(got, ": "+phoneNothingSteps(phoneAndroid)) {
		t.Errorf("plan next: %q", got)
	}

	h, p, _ := familyLink(t)
	savedBudget, savedAfter := phoneLinkBudget, phoneNothingAfter
	phoneLinkBudget, phoneNothingAfter = 3*time.Second, 200*time.Millisecond
	t.Cleanup(func() { phoneLinkBudget, phoneNothingAfter = savedBudget, savedAfter })
	seedLink(t, h, &annaLink)
	p.tapBy = func(service, ok, no string) []phoneEvent {
		if service == "mobile_app_annas_pixel" {
			return []phoneEvent{tap(ok, "anna-id", "REMOTE")}
		}
		return nil // the new iPhone never answers
	}
	var exit int
	var out string
	stderr := captureStderr(t, func() { exit, out = runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"}) })
	hint := phoneNothingHint(phoneIOS)
	if exit != 1 || !strings.Contains(out, "was not tapped in time") || !strings.Contains(out, "  "+hint+" Run ha-nova setup confirm-phone again to retry.\n") || strings.Count(stderr, hint) != 1 {
		t.Fatalf("link timeout: %d %q / stderr %q", exit, out, stderr)
	}
}
