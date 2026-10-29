package main

import (
	"bufio"
	"strings"
	"testing"
)

// The terminal's phone question: setup can't tell whose computer this is
// (only Home Assistant's owner signs in, also on a partner's computer), so
// it asks whose phone. Every administrator's phone shows its person in name
// order, nothing is proposed or first, and Enter skips.
func TestChoosePhoneAsksWhosePhone(t *testing.T) {
	admin := func(id string, owner bool) haUser {
		return haUser{ID: id, IsActive: true, IsOwner: owner, GroupIDs: []string{"system-admin"}}
	}
	anna := phoneTarget{Service: "mobile_app_annas_pixel", Name: "Anna’s Pixel", DeviceID: "dev-anna", UserID: "anna-id", Person: "Anna"}
	mine := phoneTarget{Service: "mobile_app_markuss_iphone", Name: "Markus’s iPhone", DeviceID: "dev-markus", UserID: "owner-id", Person: "Markus"}
	home := phoneHome{owner: "owner-id", relay: "relay-id", confirms: phoneAdmin,
		users:  map[string]haUser{"owner-id": admin("owner-id", true), "anna-id": admin("anna-id", false)},
		phones: []phoneTarget{anna, mine}, shared: []string{"Kid’s Phone"}, sharedOf: map[string][]string{"kid-id": {"Kid’s Phone"}}}
	var out strings.Builder
	if got, ok := choosePhone(bufio.NewReader(strings.NewReader("\n")), &out, home); ok {
		t.Fatalf("Enter linked %+v", got)
	}
	text := out.String()
	for _, want := range []string{phoneWhoseHeader, phoneWhoseQuestion, phoneWhoseMissing, phoneSharedLine("Kid’s Phone"), "1) Anna’s Pixel (Anna)", "2) Markus’s iPhone (Markus)", "Type the number of the phone"} {
		if !strings.Contains(text, want) {
			t.Fatalf("no %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Use “") || strings.Contains(text, "your") {
		t.Fatalf("a phone was offered as the user's own:\n%s", text)
	}
	out.Reset()
	if got, ok := choosePhone(bufio.NewReader(strings.NewReader("2\n")), &out, home); !ok || got != mine {
		t.Fatalf("picked %+v %v", got, ok)
	}
}
