package main

import "testing"

// Step 3's phone row with no phone of the user's own (Home Assistant's
// owner on a re-run): someone else's phone is never the one-click offer, the
// row says why, and Check again reads once more.
func TestSetupPagePhoneSaysWhyTheOwnPhoneIsMissing(t *testing.T) {
	h, phones, home := familyLink(t)
	h.f.mu.Lock()
	for i := range home.devices {
		if home.devices[i].UserID == "owner-id" {
			home.devices[i].UserID = "" // not on Markus's person
		}
	}
	home.devices = append(home.devices, phoneTarget{Service: "mobile_app_kids_phone", Name: "Kid’s Phone", DeviceID: "dev-kid-old"}) // someone else's clash isn't the reason
	h.f.mu.Unlock()
	p := newPageStepsTest(t, h.paths, "codex")
	p.flow.pairedThisRun = true
	done := p.runControl()
	ctl := p.waitFor("step 3", accepting("recheck")).Control
	if ctl.Phone != "pick" || ctl.Own != 0 || ctl.PhoneWhy == nil || ctl.PhoneWhy.Key != "setup.page.control.phone_missing" || len(ctl.Phones) != 1 || ctl.Phones[0].ID != "dev-anna" {
		t.Fatalf("control = %+v", ctl)
	}
	// An old registration with the same name: that is the reason given.
	h.f.mu.Lock()
	home.devices = append(home.devices, phoneTarget{Service: "mobile_app_markuss_iphone", Name: "Markus’s iPhone", DeviceID: "dev-old", Maker: "Apple"})
	home.devices[0].UserID = "owner-id"
	h.f.mu.Unlock()
	p.click("recheck", "")
	ctl = p.waitFor("the reason", func(s setupPageState) bool {
		return s.Control.PhoneWhy != nil && s.Control.PhoneWhy.Key == "setup.page.control.phone_shared"
	}).Control
	if ctl.Own != 0 || ctl.PhoneWhy.Args["phone"] != "Markus’s iPhone" || len(phones.sent) != 0 {
		t.Fatalf("control = %+v sent %v", ctl, phones.services)
	}
	p.click("finish", "")
	<-done
}
