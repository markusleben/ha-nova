package main

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Linking (confirm_phone_link.go) against a fake family home: Markus (the
// owner), Anna (an administrator), and a child who is not; a shared iPad on
// Markus's person that the child registered. The Relay is the Supervisor.

type fakeHome struct {
	relay   string
	users   []map[string]any
	devices []phoneTarget // every mobile_app device; UserID "" = on no person
	notify  []string      // notify services that exist
	reads   int           // phone reads seen
	usersOK bool
}

func newFamilyHome() *fakeHome {
	return &fakeHome{relay: "supervisor-id", usersOK: true,
		users: []map[string]any{
			{"id": "supervisor-id", "name": "Supervisor", "system_generated": true, "is_active": true, "group_ids": []string{"system-admin"}},
			{"id": "owner-id", "name": "Markus", "is_owner": true, "is_active": true, "group_ids": []string{"system-admin"}},
			{"id": "anna-id", "name": "Anna", "is_active": true, "group_ids": []string{"system-admin"}},
			{"id": "kid-id", "name": "Kid", "is_active": true, "group_ids": []string{"system-users"}},
		},
		devices: []phoneTarget{
			{Service: "mobile_app_markuss_iphone", Name: "Markus’s iPhone", DeviceID: "dev-markus", UserID: "owner-id", Person: "Markus"},
			{Service: "mobile_app_annas_pixel", Name: "Anna’s Pixel", DeviceID: "dev-anna", UserID: "anna-id", Person: "Anna"},
			{Service: "mobile_app_kids_phone", Name: "Kid’s Phone", DeviceID: "dev-kid", UserID: "kid-id", Person: "Kid"},
			{Service: "mobile_app_family_ipad", Name: "Family iPad", DeviceID: "dev-ipad", UserID: "owner-id", Person: "Markus"},
		},
		notify: []string{"mobile_app_markuss_iphone", "mobile_app_annas_pixel", "mobile_app_kids_phone", "mobile_app_family_ipad"},
	}
}

// install answers the phone reads; phones must come from usePhones (notify
// and listening), and discovery runs for real.
func (home *fakeHome) install(t *testing.T, f *fakeHA) {
	t.Helper()
	notify := map[string]any{}
	for _, s := range home.notify {
		notify[s] = map[string]any{}
	}
	f.services = append(f.defaultServices(), map[string]any{"domain": "notify", "services": notify})
	f.extra = func(_ string, p map[string]any) (any, bool) {
		switch {
		case p["type"] == "auth/current_user":
			home.reads++
			return map[string]any{"id": home.relay}, true
		case p["type"] == "config/auth/list":
			if !home.usersOK {
				return fakeRelayFailure{"UPSTREAM_WS_TIMEOUT", "timed out"}, true
			}
			return home.users, true
		case p["type"] == "config/device_registry/list":
			var rows []map[string]any
			for _, d := range home.devices {
				rows = append(rows, map[string]any{"id": d.DeviceID, "name": d.Name, "identifiers": [][]string{{"mobile_app", "app-" + d.DeviceID}}})
			}
			return append(rows, map[string]any{"id": "hue-bridge", "name": "Anna’s Pixel", "identifiers": [][]string{{"hue", "x"}}}), true
		case p["path"] == "/api/template":
			vars := p["body"].(map[string]any)["variables"].(map[string]any)
			device := vars["device"]
			var phones []phoneTarget
			var all []string
			var linked any
			for _, n := range vars["names"].([]any) { // Home Assistant slugs each name
				for _, d := range home.devices {
					if d.Name == n {
						all = append(all, d.Service)
						break
					}
				}
			}
			for _, d := range home.devices {
				if d.UserID != "" {
					phones = append(phones, d)
				}
				if d.DeviceID == device {
					linked = map[string]any{"service": d.Service, "name": d.Name, "device_id": d.DeviceID}
				}
			}
			out, _ := json.Marshal(map[string]any{"phones": phones, "all": all, "linked": linked})
			return coreReply(200, string(out)), true
		}
		return nil, false
	}
}

// familyLink: a harness with the family home, real discovery, and fake phones.
func familyLink(t *testing.T) (*writeHarness, *fakePhones, *fakeHome) {
	t.Helper()
	f, home := newFakeHA(), newFamilyHome()
	home.install(t, f)
	h := newWriteHarness(t, f)
	p := usePhones(t)
	phoneDiscover = discoverPhone
	return h, p, home
}

func runLink(t *testing.T, h *writeHarness, input string, run phoneLinkRun) (int, string) {
	t.Helper()
	s, err := newRelaySession(h.paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	var out strings.Builder
	return linkPhone(s, bufio.NewReader(strings.NewReader(input)), &out, run), out.String()
}

func runUnlink(t *testing.T, h *writeHarness) (int, string) {
	t.Helper()
	s, err := newRelaySession(h.paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	var out strings.Builder
	return unlinkPhone(s, &out), out.String()
}

// tapFrom answers the notification on service with the user's button.
func tapFrom(service, user string, yes bool) func(string, string, string) []phoneEvent {
	return func(got, ok, no string) []phoneEvent {
		if got != service {
			return nil
		}
		if yes {
			return []phoneEvent{tap(ok, user, "REMOTE")}
		}
		return []phoneEvent{tap(no, user, "REMOTE")}
	}
}

// calls: the service calls sent (template reads are POSTs too).
func calls(h *writeHarness) int {
	n := 0
	for _, c := range h.posts() {
		if strings.HasPrefix(c.payload["path"].(string), "/api/services/") {
			n++
		}
	}
	return n
}

// seedLink stores a link as a finished linking would.
func seedLink(t *testing.T, h *writeHarness, link *phoneLink) {
	t.Helper()
	links, _ := phoneLinks(h.paths)
	if err := setPhoneLink(h.paths, "owner-id", links["owner-id"], link); err != nil {
		t.Fatal(err)
	}
}

var annaLink = phoneLink{Service: "mobile_app_annas_pixel", DeviceID: "dev-anna", UserID: "anna-id", Name: "Anna’s Pixel"}

func linked(t *testing.T, h *writeHarness) *phoneLink {
	t.Helper()
	links, ok := phoneLinks(h.paths)
	if !ok {
		t.Fatal("config unreadable")
	}
	return links["owner-id"].link
}

// The signed-in user's only phone is proposed; Enter sends ONE notification,
// to that phone only, and the tap there links it. Later confirmations go to
// that phone only, and count only from its user — not even the owner's.
func TestLinkPhoneByTapOnTheChosenPhoneOnly(t *testing.T) {
	h, p, _ := familyLink(t)
	p.tapBy = tapFrom("mobile_app_annas_pixel", "anna-id", true)
	exit, out := runLink(t, h, "\n", phoneLinkRun{interactive: true, signedIn: "anna-id", offer: true})
	if exit != 0 || !strings.Contains(out, "Use “Anna’s Pixel” (Anna) for HA NOVA confirmations?") || !strings.Contains(out, "Only this phone gets HA NOVA confirmations") {
		t.Fatalf("exit %d out %q", exit, out)
	}
	if strings.Join(p.services, ",") != "mobile_app_annas_pixel,mobile_app_annas_pixel" || p.sent[0]["title"] != phoneTitle+phoneLinkButton {
		t.Fatalf("notified %v %v", p.services, p.sent)
	}
	if l := linked(t, h); l == nil || l.DeviceID != "dev-anna" || l.UserID != "anna-id" || l.Service != "mobile_app_annas_pixel" || l.Name != "Anna’s Pixel" {
		t.Fatalf("link %+v", l)
	}
	// A typed plan now asks Anna's phone only; a tap from the owner's account is rejected.
	plan := h.plan("lock.unlock", "lock.front").Plan
	if plan.Channel != channelPhone {
		t.Fatalf("plan %+v", plan)
	}
	p.services, p.sent = nil, nil
	p.tapBy = tapFrom("mobile_app_annas_pixel", "owner-id", true)
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 4 || env.Error.Code != "CONFIRM_REJECTED" || calls(h) != 0 {
		t.Fatalf("owner's tap: exit %d %+v", exit, env.Error)
	}
	p.tapBy = tapFrom("mobile_app_annas_pixel", "anna-id", true)
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 0 || env.Status != "verified" || calls(h) != 1 {
		t.Fatalf("anna's tap: exit %d %+v", exit, env.Error)
	}
	for _, service := range p.services {
		if service != "mobile_app_annas_pixel" {
			t.Fatalf("another phone was notified: %v", p.services)
		}
	}
	// The wizard on a computer that already has a phone just says so.
	p.services = nil
	if exit, out := runLink(t, h, "", phoneLinkRun{interactive: true, signedIn: "anna-id", offer: true}); exit != 0 || !strings.Contains(out, "Linked now: “Anna’s Pixel” confirms") || len(p.services) != 0 {
		t.Fatalf("offer when linked: %d %q %v", exit, out, p.services)
	}
}

// Without a terminal nothing is sent: the administrators' phones are a list
// (never the child's, never the Relay user's), and no phone is notified.
func TestLinkPhoneWithoutTerminalOnlyLists(t *testing.T) {
	h, p, _ := familyLink(t)
	exit, out := runLink(t, h, "", phoneLinkRun{})
	if exit != 0 || !strings.Contains(out, "1) Anna’s Pixel (Anna)") || !strings.Contains(out, "Family iPad (Markus)") || !strings.Contains(out, "Markus’s iPhone (Markus)") || !strings.Contains(out, "Only Home Assistant administrators' phones") ||
		strings.Contains(out, "Kid") || !strings.Contains(out, "Nothing was sent") || len(p.sent) != 0 || linked(t, h) != nil {
		t.Fatalf("exit %d out %q sent %v", exit, out, p.services)
	}
	// Several phones in a terminal: a numbered list; Enter skips and sends nothing.
	if exit, out := runLink(t, h, "\n", phoneLinkRun{interactive: true}); exit != 0 || !strings.Contains(out, "Which phone should confirm") || len(p.sent) != 0 {
		t.Fatalf("skip: %d %q", exit, out)
	}
}

// A shared device listed under Markus but registered by the child: the tap
// arrives as the child, so nothing is linked. A non-admin's own phone is
// refused before anything is sent.
func TestLinkPhoneRefusesOtherUsersAndNonAdmins(t *testing.T) {
	h, p, _ := familyLink(t)
	p.tapBy = tapFrom("mobile_app_family_ipad", "kid-id", true)
	exit, out := runLink(t, h, "", phoneLinkRun{pick: "family ipad"})
	if exit != 1 || !strings.Contains(out, "someone other than Markus") || linked(t, h) != nil {
		t.Fatalf("kid's tap: %d %q", exit, out)
	}
	for _, service := range p.services {
		if service != "mobile_app_family_ipad" {
			t.Fatalf("another phone was notified: %v", p.services)
		}
	}
	p.services, p.sent = nil, nil
	exit, out = runLink(t, h, "", phoneLinkRun{pick: "Kid's Phone"})
	if exit != 1 || !strings.Contains(out, "isn't a Home Assistant administrator") || len(p.sent) != 0 || linked(t, h) != nil {
		t.Fatalf("non-admin: %d %q %v", exit, out, p.services)
	}
	// A Relay-user tap, a decline, and no answer link nothing either.
	for _, tapper := range []func(string, string, string) []phoneEvent{
		tapFrom("mobile_app_markuss_iphone", "supervisor-id", true), tapFrom("mobile_app_markuss_iphone", "owner-id", false), nil,
	} {
		p.tapBy = tapper
		if exit, _ := runLink(t, h, "", phoneLinkRun{pick: "Markus's iPhone"}); exit != 1 || linked(t, h) != nil {
			t.Fatalf("linked without the owner's yes: %d", exit)
		}
	}
}

// Changing or unlinking asks the phone linked now first; the new phone gets
// nothing until the old one confirmed.
func TestRelinkAndUnlinkNeedTheOldPhone(t *testing.T) {
	h, p, home := familyLink(t)
	seedLink(t, h, &phoneLink{Service: "mobile_app_annas_pixel", DeviceID: "dev-anna", UserID: "anna-id", Name: "Anna’s Pixel"})
	p.tapBy = tapFrom("mobile_app_annas_pixel", "anna-id", false)
	if exit, out := runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"}); exit != 1 || !strings.Contains(out, "First confirm on the phone linked now, “Anna’s Pixel”") ||
		linked(t, h).DeviceID != "dev-anna" || strings.Contains(strings.Join(p.services, ","), "markus") {
		t.Fatalf("old phone cancelled: %d %q %v", exit, out, p.services)
	}
	if exit, _ := runUnlink(t, h); exit != 1 || linked(t, h) == nil {
		t.Fatalf("unlinked without the old phone")
	}
	p.services, p.sent = nil, nil
	p.tapBy = func(service, ok, no string) []phoneEvent {
		user := map[string]string{"mobile_app_annas_pixel": "anna-id", "mobile_app_markuss_iphone": "owner-id"}[service]
		return []phoneEvent{tap(ok, user, "REMOTE")}
	}
	if exit, out := runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"}); exit != 0 || linked(t, h).DeviceID != "dev-markus" || linked(t, h).UserID != "owner-id" ||
		strings.Join(p.services, ",") != "mobile_app_annas_pixel,mobile_app_annas_pixel,mobile_app_markuss_iphone,mobile_app_markuss_iphone" {
		t.Fatalf("relink: %d %q %v", exit, out, p.services)
	}
	if exit, _ := runUnlink(t, h); exit != 0 || linked(t, h) != nil {
		t.Fatalf("unlink confirmed on the phone: %d", exit)
	}
	// A linked phone that can't be reached (its app's notify service is gone)
	// blocks a change; nothing is sent anywhere.
	seedLink(t, h, &phoneLink{Service: "mobile_app_annas_pixel", DeviceID: "dev-anna", UserID: "anna-id", Name: "Anna’s Pixel"})
	home.notify = home.notify[:1]
	home.install(t, h.f)
	p.services, p.sent = nil, nil
	if exit, out := runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"}); exit != 1 || !strings.Contains(out, "remove it in Home Assistant") || len(p.sent) != 0 {
		t.Fatalf("unreachable old phone: %d %q %v", exit, out, p.services)
	}
}

// A linked phone that can never answer again (removed from Home Assistant,
// or its user no longer an administrator; a model can cause both) is
// replaced or unlinked only in the window — never by a tap alone, never
// when the window can't open.
func TestReplacingAPhoneThatCantAnswerNeedsTheWindow(t *testing.T) {
	h, p, home := familyLink(t)
	seedLink(t, h, &phoneLink{Service: "mobile_app_old", DeviceID: "dev-gone", UserID: "owner-id", Name: "Old Phone"})
	if plan, env := h.run(runPlanCommand, "call", "lock.unlock", "lock.front"); plan != 4 || env.Error.Code != "CONFIRM_UNAVAILABLE" {
		t.Fatalf("plan with a removed phone: %d %+v", plan, env.Error)
	}
	p.tapBy = tapFrom("mobile_app_markuss_iphone", "owner-id", true)
	for _, run := range []func() (int, string){
		func() (int, string) { return runUnlink(t, h) },
		func() (int, string) { return runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"}) },
	} {
		if exit, out := run(); exit != 1 || !strings.Contains(out, "No confirmation window can open") || linked(t, h).DeviceID != "dev-gone" || len(p.sent) != 0 {
			t.Fatalf("without a window: %d %q %v", exit, out, p.services)
		}
	}
	useWindow(t, windowFailed)
	if exit, _ := runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"}); exit != 1 || linked(t, h).DeviceID != "dev-gone" || len(p.sent) != 0 {
		t.Fatalf("a window that can't open replaced the phone: %d", exit)
	}
	w := useWindow(t, windowCancelled, windowConfirmed)
	if exit, _ := runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"}); exit != 1 || len(p.sent) != 0 || linked(t, h).DeviceID != "dev-gone" {
		t.Fatalf("window cancelled: %d %v", exit, p.services)
	}
	if exit, out := runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"}); exit != 0 || linked(t, h).DeviceID != "dev-markus" || !strings.Contains(w.messages[1], "Link “Markus’s iPhone”") ||
		!strings.Contains(out, "no longer in Home Assistant") || !strings.Contains(out, "A confirmation window opens on this computer first") {
		t.Fatalf("window confirmed: %d %q %v", exit, out, w.messages)
	}
	// Markus demoted: his linked phone can't confirm, and the window may approve a change.
	home.users[1]["is_owner"], home.users[1]["group_ids"] = false, []string{"system-users"}
	home.users[2]["is_owner"] = true // Anna owns this home now
	h.f.mu.Lock()
	home.install(t, h.f)
	h.f.mu.Unlock()
	links, _ := phoneLinks(h.paths)
	if err := setPhoneLink(h.paths, "anna-id", links["anna-id"], links["owner-id"].link); err != nil {
		t.Fatal(err)
	}
	p.services, p.sent = nil, nil
	p.tapBy = tapFrom("mobile_app_annas_pixel", "anna-id", true)
	w = useWindow(t, windowConfirmed)
	s, _ := newRelaySession(h.paths, verbOptions{})
	defer s.close()
	var out strings.Builder
	if exit := linkPhone(s, bufio.NewReader(strings.NewReader("")), &out, phoneLinkRun{pick: "Anna’s Pixel"}); exit != 0 || !strings.Contains(out.String(), "Markus is no longer a Home Assistant administrator") ||
		strings.Contains(out.String(), "remove it in Home Assistant") || strings.Join(p.services, ",") != "mobile_app_annas_pixel,mobile_app_annas_pixel" {
		t.Fatalf("demoted: %d %q %v", exit, out.String(), p.services)
	}
}

// A link approved against one state never overwrites a change made
// meanwhile without that change's own approval.
func TestPhoneLinkWriteNeedsTheApprovedState(t *testing.T) {
	h, _, _ := familyLink(t)
	before, _ := phoneLinks(h.paths)
	seedLink(t, h, &annaLink)
	if err := setPhoneLink(h.paths, "owner-id", before["owner-id"], nil); err != errPhoneLinkChanged || linked(t, h) == nil {
		t.Fatalf("stale unlink: %v", err)
	}
}

// Two devices with one name share a notify service, so neither is offered,
// and the user is told how to fix it instead of "install the app"; a
// device whose entities are gone still counts (the device registry).
func TestSharedPhoneNamesAreNamed(t *testing.T) {
	h, p, home := familyLink(t)
	home.users = home.users[:2] // only Markus is an administrator
	home.devices = append(home.devices, phoneTarget{Service: "mobile_app_markuss_iphone", Name: "Markus’s iPhone", DeviceID: "dev-old"},
		phoneTarget{Service: "mobile_app_family_ipad", Name: "Family iPad", DeviceID: "dev-old-ipad"})
	exit, out := runLink(t, h, "", phoneLinkRun{})
	if exit != 1 || !strings.Contains(out, "Two devices in Home Assistant are called “Markus’s iPhone”") || strings.Contains(out, "install the Home Assistant app") || len(p.sent) != 0 {
		t.Fatalf("shared: %d %q", exit, out)
	}
}

// Without a terminal, old phone plus new phone stay within one budget.
func TestPhoneLinkWithoutTerminalStaysInItsBudget(t *testing.T) {
	h, p, _ := familyLink(t)
	saved := phoneLinkBudget
	phoneLinkBudget = 3 * time.Second
	t.Cleanup(func() { phoneLinkBudget = saved })
	seedLink(t, h, &annaLink)
	p.tapBy = func(service, ok, no string) []phoneEvent {
		if service == "mobile_app_annas_pixel" {
			return []phoneEvent{tap(ok, "anna-id", "REMOTE")}
		}
		return nil // the new phone never answers
	}
	start := time.Now()
	if exit, out := runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"}); exit != 1 || time.Since(start) > 4*time.Second || !strings.Contains(out, "was not tapped in time") {
		t.Fatalf("budget: %d after %v: %q", exit, time.Since(start), out)
	}
}

// #719's pin ("on", owner's phones) refuses typed plans instead of moving
// them to chat, and `off` is refused; linking replaces it.
func TestLegacyPinRefusesUntilLinked(t *testing.T) {
	h, p, _ := familyLink(t)
	doc, err := loadConfigDocument(h.paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	doc.top[phoneSettingKey] = json.RawMessage(`{"owner-id":"on","another-owner":"off"}`)
	if err := writeJSONFileIfUnchanged(h.paths.ConfigFile, doc.top, 0o600, doc.source); err != nil {
		t.Fatal(err)
	}
	if exit, env := h.run(runPlanCommand, "call", "lock.unlock", "lock.front"); exit != 4 || env.Error.Code != "CONFIRM_UNAVAILABLE" || !strings.Contains(env.Error.Message, "none is chosen") {
		t.Fatalf("legacy pin: %d %+v", exit, env.Error)
	}
	if exit, out := runUnlink(t, h); exit != 1 || !strings.Contains(out, "Link yours first") {
		t.Fatalf("legacy off: %d %q", exit, out)
	}
	p.tapBy = tapFrom("mobile_app_markuss_iphone", "owner-id", true)
	if exit, out := runLink(t, h, "", phoneLinkRun{pick: "1"}); exit != 1 || linked(t, h) != nil {
		t.Fatalf("number 1 is Anna's phone, tapped by nobody: %d %q", exit, out)
	}
	if exit, out := runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"}); exit != 0 || linked(t, h) == nil {
		t.Fatalf("link from legacy: %d %q", exit, out)
	}
	if plan := h.plan("lock.unlock", "lock.front").Plan; plan.Channel != channelPhone {
		t.Fatalf("after linking: %+v", plan)
	}
}

// Discovery: no link means no phone reads; a link refuses (never moves)
// when the owner is unreadable, the Relay acts as the owner, or the linked
// user is no longer an administrator; a name two devices share is never
// used or listed.
func TestDiscoverLinkedPhone(t *testing.T) {
	h, p, home := familyLink(t)
	if plan := h.plan("lock.unlock", "lock.front").Plan; plan.Channel != channelChat || home.reads != 0 {
		t.Fatalf("no link: %+v reads %d", plan, home.reads)
	}
	seedLink(t, h, &annaLink)
	refused := func(why string) {
		t.Helper()
		if exit, env := h.run(runPlanCommand, "call", "lock.unlock", "lock.front"); exit != 4 || env.Error.Code != "CONFIRM_UNAVAILABLE" {
			t.Fatalf("%s: exit %d %+v", why, exit, env.Error)
		}
	}
	home.usersOK = false
	refused("owner unreadable")
	home.usersOK = true
	home.relay = "owner-id"
	refused("relay is the owner")
	if exit, out := runLink(t, h, "", phoneLinkRun{pick: "Anna’s Pixel"}); exit != 1 || !strings.Contains(out, "signs in to Home Assistant as the owner") {
		t.Fatalf("link on the owner's token: %d %q", exit, out)
	}
	home.relay = "anna-id"
	refused("relay is the linked user")
	home.relay = "supervisor-id"
	home.users[2]["group_ids"] = []string{"system-users"}
	refused("linked user demoted")
	home.users[2]["group_ids"] = []string{"system-admin"}
	home.devices = append(home.devices, phoneTarget{Service: "mobile_app_annas_pixel", Name: "Anna’s Pixel", DeviceID: "dev-anna-2"})
	refused("name shared with another device")
	if exit, out := runLink(t, h, "", phoneLinkRun{}); exit != 0 || strings.Contains(out, ") Anna") || !strings.Contains(out, "Two devices in Home Assistant are called “Anna’s Pixel”") {
		t.Fatalf("shared name listed: %q", out)
	}
	home.devices = home.devices[:len(home.devices)-1]
	if plan := h.plan("lock.unlock", "lock.front").Plan; plan.Channel != channelPhone || len(p.sent) != 0 {
		t.Fatalf("usable again: %+v", plan)
	}
}
