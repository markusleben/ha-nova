package main

import (
	"strings"
	"testing"
)

// Step 3's phone row for a user other than Home Assistant's owner with no
// phone of their own: someone else's phone is never the one-click offer,
// the row says why, and Check again reads once more.
func TestSetupPagePhoneSaysWhyTheOwnPhoneIsMissing(t *testing.T) {
	h, phones, home := familyLink(t)
	previous := setupSignedInUser
	setupSignedInUser = "anna-id"
	t.Cleanup(func() { setupSignedInUser = previous })
	h.f.mu.Lock()
	home.devices[1].UserID = "" // not on Anna's person
	// Someone else's clash isn't the reason.
	home.devices = append(home.devices, phoneTarget{Service: "mobile_app_kids_phone", Name: "Kid’s Phone", DeviceID: "dev-kid-old"})
	h.f.mu.Unlock()
	p := newPageStepsTest(t, h.paths, "codex")
	p.flow.pairedThisRun = true
	done := p.runControl()
	ctl := p.waitFor("step 3", accepting("recheck")).Control
	if ctl.Phone != "pick" || ctl.Whose || ctl.Own != 0 || ctl.PhoneWhy == nil || ctl.PhoneWhy.Key != "setup.page.control.phone_missing" || len(ctl.Phones) != 2 || ctl.Phones[0].ID != "dev-ipad" {
		t.Fatalf("control = %+v", ctl)
	}
	// An old registration with the same name: that is the reason given.
	h.f.mu.Lock()
	home.devices = append(home.devices, phoneTarget{Service: "mobile_app_annas_pixel", Name: "Anna’s Pixel", DeviceID: "dev-anna-old", Maker: "Google"})
	home.devices[1].UserID = "anna-id"
	h.f.mu.Unlock()
	p.click("recheck", "")
	ctl = p.waitFor("the reason", func(s setupPageState) bool {
		return s.Control.PhoneWhy != nil && s.Control.PhoneWhy.Key == "setup.page.control.phone_shared"
	}).Control
	if ctl.Own != 0 || ctl.PhoneWhy.Args["phone"] != "Anna’s Pixel" || len(phones.sent) != 0 {
		t.Fatalf("control = %+v sent %v", ctl, phones.services)
	}
	p.click("finish", "")
	<-done
}

// Home Assistant's owner signed in, maybe at a family member's computer:
// the row asks whose phone should confirm here, lists every
// administrator's phone with its person and none first, and Check again
// stays (the page says how to add a phone). An administrator's phone left
// out for its name is named; a non-administrator's isn't. The phone picked
// gets the one notification; a tap from another account names whose
// account it must be and keeps the question; that phone's own tap links it.
func TestSetupPageAsksWhosePhoneWhenTheOwnerSignedIn(t *testing.T) {
	h, phones, home := familyLink(t)
	phones.tapBy = tapFrom("mobile_app_annas_pixel", "owner-id", true) // Anna's app signed in as Markus
	previous := setupSignedInUser
	setupSignedInUser = "owner-id"
	t.Cleanup(func() { setupSignedInUser = previous })
	h.f.mu.Lock()
	home.devices = append(home.devices, phoneTarget{Service: "mobile_app_kids_phone", Name: "Kid’s Phone", DeviceID: "dev-kid-old"})
	h.f.mu.Unlock()
	p := newPageStepsTest(t, h.paths, "codex")
	p.flow.pairedThisRun = true
	done := p.runControl()
	ctl := p.waitFor("step 3", accepting("recheck")).Control
	var listed []string
	for _, phone := range ctl.Phones {
		listed = append(listed, phone.ID+" "+phone.Person)
	}
	if ctl.Phone != "pick" || !ctl.Whose || ctl.Own != 0 || ctl.PhoneWhy != nil ||
		strings.Join(listed, ",") != "dev-anna Anna,dev-ipad Markus,dev-markus Markus" || len(phones.sent) != 0 {
		t.Fatalf("control = %+v", ctl)
	}
	// An administrator's phone left out for its name: that is the reason given.
	h.f.mu.Lock()
	home.devices = append(home.devices, phoneTarget{Service: "mobile_app_family_ipad", Name: "Family iPad", DeviceID: "dev-ipad-old"})
	h.f.mu.Unlock()
	p.click("recheck", "")
	ctl = p.waitFor("the reason", func(s setupPageState) bool {
		return s.Control.PhoneWhy != nil && s.Control.PhoneWhy.Key == "setup.page.control.phone_shared"
	}).Control
	if !ctl.Whose || ctl.PhoneWhy.Args["phone"] != "Family iPad" || len(ctl.Phones) != 2 || len(phones.sent) != 0 {
		t.Fatalf("control = %+v sent %v", ctl, phones.services)
	}
	p.click("phone", "dev-anna")
	ctl = p.waitFor("the rejected tap", func(s setupPageState) bool { return s.Control.PhoneProblem != nil }).Control
	if ctl.Phone != "pick" || !ctl.Whose || ctl.PhoneProblem.Key != "setup.page.control.phone_rejected" ||
		ctl.PhoneProblem.Args["phone"] != "Anna’s Pixel" || ctl.PhoneProblem.Args["person"] != "Anna" || linked(t, h) != nil {
		t.Fatalf("control = %+v", ctl)
	}
	phones.mu.Lock()
	phones.tapBy = tapFrom("mobile_app_annas_pixel", "anna-id", true)
	phones.mu.Unlock()
	p.waitFor("the question again", accepting("phone"))
	p.click("phone", "dev-anna")
	ctl = p.waitFor("linked", func(s setupPageState) bool { return s.Control.Phone == "linked" }).Control
	if ctl.PhoneName != "Anna’s Pixel" || ctl.Whose || ctl.PhoneProblem != nil || phones.services[0] != "mobile_app_annas_pixel" {
		t.Fatalf("control = %+v notified %v", ctl, phones.services)
	}
	if l := linked(t, h); l == nil || l.DeviceID != "dev-anna" || l.UserID != "anna-id" {
		t.Fatalf("link %+v", l)
	}
	p.waitFor("finish", accepting("finish"))
	p.click("finish", "")
	<-done
}
