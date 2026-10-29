package main

import (
	"bufio"
	"strings"
	"testing"
)

// The terminal's phone question: the user's own phones first (the owner's
// when no one signed in), and without one, why, before anyone else's.
func TestChoosePhoneOwnFirstAndWhyItIsMissing(t *testing.T) {
	admin := func(id string, owner bool) haUser {
		return haUser{ID: id, IsActive: true, IsOwner: owner, GroupIDs: []string{"system-admin"}}
	}
	anna := phoneTarget{Service: "mobile_app_annas_pixel", Name: "Anna’s Pixel", DeviceID: "dev-anna", UserID: "anna-id", Person: "Anna"}
	mine := phoneTarget{Service: "mobile_app_markuss_iphone", Name: "Markus’s iPhone", DeviceID: "dev-markus", UserID: "owner-id", Person: "Markus"}
	home := phoneHome{owner: "owner-id", relay: "relay-id",
		users:  map[string]haUser{"owner-id": admin("owner-id", true), "anna-id": admin("anna-id", false)},
		phones: []phoneTarget{anna, mine}}
	if all, own := home.ownFirst(""); own != 1 || all[0] != mine || all[1] != anna {
		t.Fatalf("owner fallback: %v own=%d", all, own)
	}
	if all, own := home.ownFirst("anna-id"); own != 1 || all[0] != anna {
		t.Fatalf("signed in: %v own=%d", all, own)
	}

	// No own phone; a child's two devices share a name: that isn't the reason.
	home.phones = []phoneTarget{anna}
	home.shared, home.sharedOf = []string{"Kid’s Phone"}, map[string][]string{"kid-id": {"Kid’s Phone"}}
	var out strings.Builder
	if got, ok := choosePhone(bufio.NewReader(strings.NewReader("\n")), &out, home, ""); ok {
		t.Fatalf("Enter linked %+v", got)
	}
	text := out.String()
	if !strings.Contains(text, phoneOwnMissing) || strings.Contains(text, phoneListHeader) || !strings.Contains(text, "Anna’s Pixel") {
		t.Fatalf("no own phone:\n%s", text)
	}

	// The owner's own phone shares its name: that is the reason.
	home.sharedOf["owner-id"] = []string{"Markus’s iPhone"}
	home.shared = append(home.shared, "Markus’s iPhone")
	out.Reset()
	choosePhone(bufio.NewReader(strings.NewReader("\n")), &out, home, "")
	if text := out.String(); strings.Contains(text, phoneOwnMissing) || !strings.Contains(text, phoneSharedLine("Markus’s iPhone")) {
		t.Fatalf("shared own phone:\n%s", text)
	}
}
