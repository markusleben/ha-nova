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
		if link["message"] != phoneRequested()+" "+c.hint || confirm["authenticationRequired"] != c.unlock || len(actions) != 2 ||
			actions[1].(map[string]any)["authenticationRequired"] != nil || data["priority"] != "high" || data["tag"] != "tag-1" {
			t.Fatalf("%q link: %v", c.maker, link)
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
		"":           "Tap “Allow” in the notification. To see it, press and hold the notification on an iPhone, or pull it down on Android.",
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
