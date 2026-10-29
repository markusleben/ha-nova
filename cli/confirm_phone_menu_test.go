package main

import (
	"bufio"
	"slices"
	"strings"
	"testing"
)

// The terminal's phone question when a user other than Home Assistant's
// owner signed in: their own phones first, and without one, why, before
// anyone else's.
func TestChoosePhoneOwnFirstAndWhyItIsMissing(t *testing.T) {
	admin := func(id string, owner bool) haUser {
		return haUser{ID: id, IsActive: true, IsOwner: owner, GroupIDs: []string{"system-admin"}}
	}
	anna := phoneTarget{Service: "mobile_app_annas_pixel", Name: "Anna’s Pixel", DeviceID: "dev-anna", UserID: "anna-id", Person: "Anna"}
	mine := phoneTarget{Service: "mobile_app_markuss_iphone", Name: "Markus’s iPhone", DeviceID: "dev-markus", UserID: "owner-id", Person: "Markus"}
	home := phoneHome{owner: "owner-id", relay: "relay-id",
		users:  map[string]haUser{"owner-id": admin("owner-id", true), "anna-id": admin("anna-id", false), "ben-id": admin("ben-id", false)},
		phones: []phoneTarget{mine, anna}}
	if all, own := home.ownFirst("anna-id"); own != 1 || all[0] != anna || all[1] != mine {
		t.Fatalf("signed in: %v own=%d", all, own)
	}

	// Ben signed in and has no phone; a child's two devices share a name: that isn't the reason.
	home.shared, home.sharedOf = []string{"Kid’s Phone"}, map[string][]string{"kid-id": {"Kid’s Phone"}}
	var out strings.Builder
	if got, ok := choosePhone(bufio.NewReader(strings.NewReader("\n")), &out, home, "ben-id"); ok {
		t.Fatalf("Enter linked %+v", got)
	}
	text := out.String()
	if !strings.Contains(text, phoneOwnMissing) || strings.Contains(text, phoneListHeader) || strings.Contains(text, phoneWhoseQuestion) || !strings.Contains(text, "Anna’s Pixel") {
		t.Fatalf("no own phone:\n%s", text)
	}

	// Ben's own phone shares its name: that is the reason.
	home.sharedOf["ben-id"] = []string{"Ben’s Phone"}
	home.shared = append(home.shared, "Ben’s Phone")
	out.Reset()
	choosePhone(bufio.NewReader(strings.NewReader("\n")), &out, home, "ben-id")
	if text := out.String(); strings.Contains(text, phoneOwnMissing) || !strings.Contains(text, phoneSharedLine("Ben’s Phone")) {
		t.Fatalf("shared own phone:\n%s", text)
	}
}

// The owner signed in, or nobody did (`ha-nova setup confirm-phone`): the
// owner may be setting up someone else's computer, so no phone is "own".
// The question is whose phone, every phone shows its person in name order,
// nothing is offered first, and Enter skips.
func TestChoosePhoneAsksWhoseWhenTheOwnerSetsUp(t *testing.T) {
	admin := func(id string, owner bool) haUser {
		return haUser{ID: id, IsActive: true, IsOwner: owner, GroupIDs: []string{"system-admin"}}
	}
	anna := phoneTarget{Service: "mobile_app_annas_pixel", Name: "Anna’s Pixel", DeviceID: "dev-anna", UserID: "anna-id", Person: "Anna"}
	mine := phoneTarget{Service: "mobile_app_markuss_iphone", Name: "Markus’s iPhone", DeviceID: "dev-markus", UserID: "owner-id", Person: "Markus"}
	home := phoneHome{owner: "owner-id", relay: "relay-id",
		users:  map[string]haUser{"owner-id": admin("owner-id", true), "anna-id": admin("anna-id", false)},
		phones: []phoneTarget{anna, mine}, shared: []string{"Kid’s Phone"}, sharedOf: map[string][]string{"kid-id": {"Kid’s Phone"}}}
	for _, signedIn := range []string{"owner-id", ""} {
		if !home.whose(signedIn) {
			t.Fatalf("%q: not whose", signedIn)
		}
		if all, own := home.ownFirst(signedIn); own != 0 || !slices.Equal(all, []phoneTarget{anna, mine}) {
			t.Fatalf("%q: %v own=%d", signedIn, all, own)
		}
		var out strings.Builder
		if got, ok := choosePhone(bufio.NewReader(strings.NewReader("\n")), &out, home, signedIn); ok {
			t.Fatalf("%q: Enter linked %+v", signedIn, got)
		}
		text := out.String()
		for _, want := range []string{phoneWhoseHeader, phoneWhoseQuestion, phoneWhoseMissing, phoneSharedLine("Kid’s Phone"), "1) Anna’s Pixel (Anna)", "2) Markus’s iPhone (Markus)", "Type the number of the phone"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%q: no %q in:\n%s", signedIn, want, text)
			}
		}
		if strings.Contains(text, "Use “") || strings.Contains(text, phoneListHeader) || strings.Contains(text, phoneOwnMissing) || strings.Contains(text, "your") {
			t.Fatalf("%q: an own phone was offered:\n%s", signedIn, text)
		}
		out.Reset()
		if got, ok := choosePhone(bufio.NewReader(strings.NewReader("2\n")), &out, home, signedIn); !ok || got != mine {
			t.Fatalf("%q: picked %+v %v", signedIn, got, ok)
		}
	}
	if home.whose("anna-id") {
		t.Fatal("a user other than the owner: own phones first")
	}
}
