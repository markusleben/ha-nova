package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"
	"slices"
	"sort"
)

// Who confirms on a phone, and where (spec §7.2). One phone per computer and
// Home Assistant, linked by the user (confirm_phone_link.go): config.json
// stores its device id and the Home Assistant user its link tap arrived as.
// Only that phone is ever sent anything, and a tap counts only from that
// user while they are an administrator other than the Relay's own user.

// phoneTemplate lists every person's Companion phones (device id, name, and
// legacy notify service, slugged by Home Assistant itself), the service of
// every Companion device named in names (from the device registry, so a
// device whose entities are disabled still counts: two devices with one
// name share a service), and the linked device by id. names and the id are
// template variables, never template source.
const phoneTemplate = `{%- set ns = namespace(phones=[], all=[]) -%}
{%- set apps = integration_entities('mobile_app') -%}
{%- for n in names -%}{%- set ns.all = ns.all + [('mobile_app_' ~ n) | slugify] -%}{%- endfor -%}
{%- for p in states.person if p.attributes.user_id -%}
{%- for t in p.attributes.device_trackers | default([], true) if t in apps -%}
{%- set d = device_id(t) -%}
{%- set n = device_attr(d, 'name') if d else none -%}
{%- if n -%}{%- set ns.phones = ns.phones + [{'service': ('mobile_app_' ~ n) | slugify, 'name': n, 'device_id': d, 'user_id': p.attributes.user_id, 'person': p.name}] -%}{%- endif -%}
{%- endfor -%}
{%- endfor -%}
{%- set n = device_attr(device, 'name') if device else none -%}
{{ {'phones': ns.phones, 'all': ns.all, 'linked': ({'service': ('mobile_app_' ~ n) | slugify, 'name': n, 'device_id': device} if n else none)} | to_json }}`

var phoneServicePattern = regexp.MustCompile(`^mobile_app_[a-z0-9_]+$`)

// checkPhoneRead is the fixed allowlist of the discovery reads.
func checkPhoneRead(endpoint string, payload map[string]any) error {
	switch endpoint {
	case "ws":
		if t := payload["type"]; len(payload) == 1 && (t == "auth/current_user" || t == "config/auth/list" || t == "config/device_registry/list") {
			return nil
		}
	case "core":
		body, _ := payload["body"].(map[string]any)
		vars, _ := body["variables"].(map[string]any)
		_, isText := vars["device"].(string)
		isList := false
		switch names := vars["names"].(type) { // []any once decoded
		case []string:
			isList = !slices.Contains(names, "")
		case []any:
			isList = true
			for _, n := range names {
				text, ok := n.(string)
				isList = isList && ok && text != ""
			}
		}
		if len(payload) == 3 && payload["method"] == http.MethodPost && payload["path"] == "/api/template" &&
			len(body) == 2 && body["template"] == phoneTemplate && len(vars) == 2 && isText && isList {
			return nil
		}
	}
	return errors.New("not a phone-confirmation read; nothing was sent")
}

func (s *relaySession) phoneRead(endpoint string, payload map[string]any, into any) error {
	if err := checkPhoneRead(endpoint, payload); err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	data, err := s.send(endpoint, body)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, into)
}

type haUser struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	IsOwner         bool     `json:"is_owner"`
	IsActive        bool     `json:"is_active"`
	SystemGenerated bool     `json:"system_generated"`
	GroupIDs        []string `json:"group_ids"`
}

// phoneAdmin: confirming unlocks and deletes needs an active person who
// administers Home Assistant (never a system user such as the Supervisor).
func phoneAdmin(u haUser) bool {
	return u.ID != "" && u.IsActive && !u.SystemGenerated && (u.IsOwner || slices.Contains(u.GroupIDs, "system-admin"))
}

// phoneHome is one Home Assistant as phone confirmation sees it.
type phoneHome struct {
	owner, relay string
	users        map[string]haUser
	phones       []phoneTarget // on a person with a user, reachable, name not shared
	shared       []string      // names of person phones left out because another device has the same name
	linked       *phoneTarget  // the linked device while it exists; Service "" when it can't be reached
	linkedWhy    phoneWhy      // why linked can't be reached
}

// lookupPhones reads the home. owner is set whenever it is known, also on
// error: links are keyed by it.
func lookupPhones(s *relaySession, links map[string]phoneEntry) (phoneHome, error) {
	var current haUser
	var users []haUser
	if err := s.phoneRead("ws", map[string]any{"type": "auth/current_user"}, &current); err != nil {
		return phoneHome{}, err
	}
	if err := s.phoneRead("ws", map[string]any{"type": "config/auth/list"}, &users); err != nil {
		return phoneHome{}, err
	}
	home := phoneHome{relay: current.ID, users: map[string]haUser{}}
	for _, u := range users {
		home.users[u.ID] = u
		if u.IsOwner && u.IsActive && !u.SystemGenerated {
			home.owner = u.ID
		}
	}
	if home.owner == "" || current.ID == "" {
		return home, errors.New("Home Assistant's owner or the Relay's user could not be read")
	}
	device := ""
	if link := linkFor(links, home.owner).link; link != nil {
		device = link.DeviceID
	}
	var devices []struct {
		Name        string     `json:"name"`
		Identifiers [][]string `json:"identifiers"`
	}
	if err := s.phoneRead("ws", map[string]any{"type": "config/device_registry/list"}, &devices); err != nil {
		return home, err
	}
	names := []string{}
	for _, d := range devices {
		for _, id := range d.Identifiers {
			if len(id) > 0 && id[0] == "mobile_app" && d.Name != "" {
				names = append(names, d.Name)
				break
			}
		}
	}
	var core struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}
	payload := map[string]any{"method": http.MethodPost, "path": "/api/template",
		"body": map[string]any{"template": phoneTemplate, "variables": map[string]any{"device": device, "names": names}}}
	if err := s.phoneRead("core", payload, &core); err != nil {
		return home, err
	}
	status, services, err := s.coreGET("/api/services")
	if err != nil || core.Status != http.StatusOK || status != http.StatusOK {
		return home, errors.New("the Companion phones could not be read")
	}
	return home, home.fill(core.Body, services, device)
}

// fill keeps the rendered phones whose notify service exists and is theirs
// alone. rendered is the template's text (a JSON string), or the object.
func (h *phoneHome) fill(rendered, services json.RawMessage, device string) error {
	var text string
	if json.Unmarshal(rendered, &text) == nil {
		rendered = json.RawMessage(text)
	}
	var found struct {
		Phones []phoneTarget `json:"phones"`
		All    []string      `json:"all"`
		Linked *phoneTarget  `json:"linked"`
	}
	var domains []struct {
		Domain   string                     `json:"domain"`
		Services map[string]json.RawMessage `json:"services"`
	}
	if json.Unmarshal(rendered, &found) != nil || json.Unmarshal(services, &domains) != nil {
		return errors.New("the Companion phones could not be read")
	}
	notify := map[string]bool{}
	for _, d := range domains {
		if d.Domain == "notify" {
			for name := range d.Services {
				notify[name] = true
			}
		}
	}
	shared := map[string]int{}
	for _, service := range found.All {
		shared[service]++
	}
	usable := func(t phoneTarget) bool {
		return phoneServicePattern.MatchString(t.Service) && notify[t.Service] && shared[t.Service] <= 1 && t.DeviceID != ""
	}
	seen := map[string]bool{}
	for _, t := range found.Phones {
		switch {
		case seen[t.DeviceID] || t.UserID == "":
		case shared[t.Service] > 1:
			if !slices.Contains(h.shared, t.Name) {
				h.shared = append(h.shared, t.Name)
			}
		case usable(t):
			seen[t.DeviceID] = true
			h.phones = append(h.phones, t)
		}
	}
	sort.SliceStable(h.phones, func(i, j int) bool { return h.phones[i].Name < h.phones[j].Name })
	switch {
	case found.Linked != nil && found.Linked.DeviceID == device:
		linked := *found.Linked
		if !usable(linked) {
			linked.Service, h.linkedWhy = "", whyUnreachable
			if shared[found.Linked.Service] > 1 {
				h.linkedWhy = whyShared
			}
		}
		h.linked = &linked
	}
	return nil
}

type phoneStatus int

// phoneWhy: why a linked phone can't be asked (phoneMissing).
type phoneWhy int

const (
	whyNone        phoneWhy = iota
	whyRead                 // a read failed
	whyOwnerToken           // the Relay signs in as the owner
	whyRelayUser            // the Relay now signs in as the linked user
	whyDemoted              // the linked user is no longer an active administrator
	whyRemoved              // the linked device is no longer in Home Assistant
	whyShared               // another device has the linked phone's name
	whyUnreachable          // its notify service is gone (app signed out or disabled)
)

// replaceable: the linked phone can never answer again as it is, so the
// window (where one opens) approves changing it, as when none is linked.
func (w phoneWhy) replaceable() bool { return w == whyRemoved || w == whyDemoted || w == whyRelayUser }

const (
	phoneNone    phoneStatus = iota // no phone linked for this home on this computer: the next channel
	phoneReady                      // the linked phone can be asked
	phoneMissing                    // linked, but it can't be asked now: refuse, never downgrade
	phoneLegacy                     // #719's pin ("on"), no phone chosen yet: refuse until linked
)

// status: the linked phone's setup, or why it can't be asked. A failed
// read is phoneMissing wherever any link exists.
func (h phoneHome) status(links map[string]phoneEntry, err error) (phoneSetup, phoneStatus) {
	if h.owner == "" {
		return phoneSetup{why: whyRead}, phoneMissing // a link exists on this computer; whether it is this home's can't be checked
	}
	entry, ok := links[h.owner]
	if chatCall != nil {
		entry, ok = links[chatOwner]
	}
	switch {
	case !ok:
		return phoneSetup{owner: h.owner, relayUser: h.relay}, phoneNone
	case entry.legacy:
		return phoneSetup{owner: h.owner, relayUser: h.relay}, phoneLegacy
	}
	setup := phoneSetup{owner: h.owner, relayUser: h.relay, confirmer: entry.link.UserID}
	switch {
	case err != nil:
		setup.why = whyRead
	case h.relay == h.owner:
		setup.why = whyOwnerToken
	case setup.confirmer == h.relay:
		setup.why = whyRelayUser
	case !phoneAdmin(h.users[setup.confirmer]):
		setup.why = whyDemoted
	case h.linked == nil:
		setup.why = whyRemoved
	case h.linked.Service == "":
		setup.why = h.linkedWhy
	}
	if setup.why != whyNone {
		return setup, phoneMissing
	}
	target := *h.linked
	target.UserID = setup.confirmer
	setup.targets = []phoneTarget{target}
	return setup, phoneReady
}

// discoverPhone: the linked phone, read only when this computer has a link.
func discoverPhone(s *relaySession) (phoneSetup, phoneStatus) {
	if activeServerIsDemo() {
		return phoneSetup{}, phoneNone // links belong to real homes
	}
	links, readable := linksForCall(s.paths)
	if !readable {
		return phoneSetup{why: whyRead}, phoneMissing
	}
	if len(links) == 0 {
		return phoneSetup{}, phoneNone
	}
	home, err := lookupPhones(s, links)
	return home.status(links, err)
}

// The link store (config.json top level, keyed by the Home Assistant owner's
// user id, unique per Home Assistant): a phoneLink, or #719's "on" pin.
const phoneSettingKey = "confirm_phone"

type phoneLink struct {
	Service  string `json:"service"`
	DeviceID string `json:"device_id"`
	UserID   string `json:"user_id"` // the tap's context.user_id
	Name     string `json:"name"`
}

type phoneEntry struct {
	legacy bool
	link   *phoneLink
}

// phoneLinks: ok=false when config.json can't be read now (mid-transaction,
// unreadable): the links are unknown, never "none". #719's "off" is no link.
func phoneLinks(paths runtimePaths) (map[string]phoneEntry, bool) {
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		return nil, os.IsNotExist(err)
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(doc.top[phoneSettingKey], &raw) // unreadable: as if none (setPhoneLink starts over the same way)
	links := map[string]phoneEntry{}
	for owner, value := range raw {
		var pin string
		if json.Unmarshal(value, &pin) == nil {
			if pin == "on" {
				links[owner] = phoneEntry{legacy: true}
			}
			continue
		}
		link := &phoneLink{}
		_ = json.Unmarshal(value, link) // malformed: an empty link, which is never usable
		links[owner] = phoneEntry{link: link}
	}
	return links, true
}

var errPhoneLinkChanged = errors.New("the phone setting for this Home Assistant changed while HA NOVA was asking; run it again")

// setPhoneLink stores this home's link (nil removes it) only while the
// home's entry is still prev, the one the confirmation approved changing: a
// concurrent change is never overwritten without its own approval.
func setPhoneLink(paths runtimePaths, owner string, prev phoneEntry, link *phoneLink) error {
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		return err
	}
	if now, ok := phoneLinks(paths); !ok || !samePhoneEntry(now[owner], prev) {
		return errPhoneLinkChanged
	}
	links := map[string]json.RawMessage{}
	_ = json.Unmarshal(doc.top[phoneSettingKey], &links) // unreadable: start over
	top := make(map[string]json.RawMessage, len(doc.top))
	for key, v := range doc.top {
		top[key] = v
	}
	delete(links, owner)
	if link != nil {
		links[owner], _ = json.Marshal(link)
	}
	delete(top, phoneSettingKey)
	if len(links) > 0 {
		top[phoneSettingKey], _ = json.Marshal(links)
	}
	return writeJSONFileIfUnchanged(paths.ConfigFile, top, 0o600, doc.source)
}

func samePhoneEntry(a, b phoneEntry) bool {
	return a.legacy == b.legacy && (a.link == nil) == (b.link == nil) && (a.link == nil || *a.link == *b.link)
}

// detectAfterWindow picks the channel where no window opens: the linked
// phone, else a form, else chat. A linked phone that can't be asked now
// refuses (channelPhoneMissing), so breaking its setup in Home Assistant
// can't move a plan to a channel the model can answer.
func detectAfterWindow(s *relaySession) string {
	switch _, status := discoverConfirmer(s); status {
	case phoneReady:
		return channelPhone
	case phoneMissing:
		return channelPhoneMissing
	case phoneLegacy:
		return channelPhoneUnlinked
	}
	return codeChannel()
}
