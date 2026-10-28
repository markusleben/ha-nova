package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Some allowlisted raw writes can lower the tier of a later `ha-nova call`
// without acting on anything themselves: deleting an integration entry or a
// registry row, or reconfiguring an entry, can remove a lock, alarm panel,
// or access cover (or change its class) that marks a sibling switch, button,
// or switch_as_x source as typed; renaming a factory-reset button drops the
// word that makes its press typed; disabling a device, an integration entry,
// or the switch a switch_as_x lock or cover runs on, takes that lock, alarm
// panel, or access cover out of service with it. Before sending one, the CLI
// reads the registry and refuses it when the target is such an entity. An
// access change to the owner's, a system, or HA NOVA's own account is
// refused the same way (rawAccountKept).

var rawEntryDeletePath = regexp.MustCompile(`^/api/config/config_entries/entry/([A-Za-z0-9_-]+)$`)

const rawNextAccessTarget = "it controls a lock, alarm panel, or access cover, and removing or reconfiguring it would change which actions need the one-time code; do this in the Home Assistant UI"

// rawTarget: the entry or entity a raw request would remove or reconfigure,
// the button it would rename, the device it would disable or enable, or the
// switch it would disable or enable (a switch_as_x lock or cover runs on it),
// the device it would delete, the integration entry it would disable or
// enable, the account whose access it would change, or a remote backup
// location it would add (zero = none of these).
type rawTarget struct{ entry, entity, button, device, source, deleted, toggled, account, backup string }

func rawAccessTarget(endpoint string, body []byte) rawTarget {
	payload, ok := decodeObject(body)
	if !ok {
		return rawTarget{}
	}
	switch endpoint {
	case "core":
		method, _ := payload["method"].(string)
		method = strings.ToUpper(strings.TrimSpace(method))
		raw, _ := payload["path"].(string)
		pathPart, _, _ := strings.Cut(raw, "#")
		pathPart, _, _ = strings.Cut(pathPart, "?")
		if m := rawEntryDeletePath.FindStringSubmatch(pathPart); m != nil && method == "DELETE" {
			return rawTarget{entry: m[1]}
		}
		inner, _ := payload["body"].(map[string]any)
		switch {
		case method != "POST":
		case pathPart == "/api/config/config_entries/options/flow": // handler is the entry id
			handler, _ := inner["handler"].(string)
			return rawTarget{entry: handler}
		case pathPart == "/api/config/config_entries/subentries/flow":
			handler, _ := inner["handler"].([]any)
			if len(handler) == 2 {
				entry, _ := handler[0].(string)
				return rawTarget{entry: cmp.Or(entry, "(none)")}
			}
		case pathPart == "/api/config/config_entries/flow": // a reconfigure flow names its entry
			entry, _ := inner["entry_id"].(string)
			return rawTarget{entry: entry}
		}
	case "ws":
		for _, message := range []any{payload, payload["message"]} {
			m, _ := message.(map[string]any)
			id, _ := m["entity_id"].(string)
			id = strings.ToLower(id)
			_, renames := m["name"]
			_, newID := m["new_entity_id"]
			_, disables := m["disabled_by"]
			_, polling := m["pref_disable_polling"]
			device, _ := m["device_id"].(string)
			entry, _ := m["entry_id"].(string)
			user, _ := m["user_id"].(string)
			access := false
			for _, key := range []string{"group_ids", "is_active", "local_only"} {
				_, set := m[key]
				access = access || set
			}
			switch {
			case m["type"] == "config_entries/subentries/delete":
				return rawTarget{entry: cmp.Or(entry, "(none)")}
			case m["type"] == "config_entries/disable" || m["type"] == "config_entries/update" && polling:
				return rawTarget{toggled: cmp.Or(entry, "(none)")}
			case m["type"] == "config/auth/update" && access:
				return rawTarget{account: cmp.Or(user, "(none)")}
			case m["type"] == "backup/config/update" && rawRemoteBackupAgents(m):
				return rawTarget{backup: "remote"}
			case m["type"] == "config/entity_registry/remove":
				return rawTarget{entity: id}
			case m["type"] == "config/entity_registry/update" && strings.HasPrefix(id, "button.") && (renames || newID):
				return rawTarget{button: id}
			case m["type"] == "config/device_registry/update" && disables:
				return rawTarget{device: device}
			case m["type"] == "config/device_registry/remove":
				return rawTarget{deleted: device}
			case m["type"] == "config/entity_registry/update" && disables && strings.HasPrefix(id, "switch."):
				return rawTarget{source: id}
			}
		}
	}
	return rawTarget{}
}

// checkRawAccessTarget reads the registry for a request rawAccessTarget
// names: refusal is the safety rule (exit 4), err a failed read (exit 1) —
// either way nothing is sent unchecked.
func checkRawAccessTarget(paths runtimePaths, opts verbOptions, endpoint string, body []byte) (refusal, err error) {
	if rawAccessTarget(endpoint, body) == (rawTarget{}) {
		return nil, nil
	}
	s, err := newRelaySession(paths, opts)
	if err != nil {
		return nil, err
	}
	defer s.close()
	return checkRawAccessTargetIn(s, endpoint, body)
}

// checkRawAccessTargetIn is checkRawAccessTarget on an open session. It
// reads the registry and cover states live, never from the session's memo,
// so a caller sending several requests runs it right before each send: an
// earlier one may have renamed an entity id or finished a flow.
func checkRawAccessTargetIn(s *relaySession, endpoint string, body []byte) (refusal, err error) {
	target := rawAccessTarget(endpoint, body)
	if target == (rawTarget{}) {
		return nil, nil
	}
	if target.account != "" {
		return rawAccountKept(s, target.account)
	}
	if target.backup != "" {
		return rawBackupKeySaved(s)
	}
	var rows []struct {
		EntityID      string `json:"entity_id"`
		ConfigEntryID string `json:"config_entry_id"`
		DeviceID      string `json:"device_id"`
		Platform      string `json:"platform"`
		Name          string `json:"name"`
		OriginalName  string `json:"original_name"`
	}
	data, err := s.readFresh("ws", map[string]any{"type": "config/entity_registry/list"}, relaySessionRequestTimeout)
	if err == nil {
		if uerr := json.Unmarshal(data, &rows); uerr != nil {
			err = fmt.Errorf("cannot parse the entity registry: %w", uerr)
		} else if rows == nil { // a real empty registry is []
			err = errors.New("the entity registry came back empty")
		}
	}
	if err != nil {
		return nil, errors.New("cannot read what this request would remove or reconfigure, so nothing was sent: " + err.Error())
	}
	gone := map[string]bool{}
	if target.deleted != "" {
		var refusal error
		if gone, refusal, err = rawDeletedDevices(s, target.deleted); refusal != nil || err != nil {
			return refusal, err
		}
	}
	sources := map[string]bool{}
	switches := map[string]bool{}
	deviceLessSources := false
	for _, row := range rows {
		takes := target.toggled != "" && row.ConfigEntryID == target.toggled || target.entry != "" && row.ConfigEntryID == target.entry ||
			target.device != "" && row.DeviceID == target.device || gone[row.DeviceID] || row.EntityID == target.entity
		if row.EntityID == target.source || takes && strings.HasPrefix(row.EntityID, "switch.") {
			switches[row.EntityID] = true
			if row.DeviceID != "" {
				sources[row.DeviceID] = true
			} else {
				deviceLessSources = true
			}
		}
	}
	for _, row := range rows {
		if row.EntityID == target.button && destructiveButtonText(row.EntityID+" "+row.Name+" "+row.OriginalName) {
			return rawRefusal("rename of "+row.EntityID+" (its name marks a press as needing the one-time code)", rawNextUI), nil
		}
		wrapped := row.Platform == "switch_as_x" && sources[row.DeviceID]
		if row.Platform == "switch_as_x" && (wrapped || row.DeviceID == "" && deviceLessSources) && accessCapable(row.EntityID, coverClass(s, row.EntityID)) {
			var wrapper struct {
				Options struct {
					Switch struct {
						EntityID string `json:"entity_id"`
					} `json:"switch_as_x"`
				} `json:"options"`
			}
			data, err := s.readFresh("ws", map[string]any{"type": "config/entity_registry/get", "entity_id": row.EntityID}, relaySessionRequestTimeout)
			switch {
			case err == nil && json.Unmarshal(data, &wrapper) == nil && wrapper.Options.Switch.EntityID != "":
				wrapped = switches[wrapper.Options.Switch.EntityID] // another channel of the same device stays free
			case row.DeviceID == "":
				return rawRefusal("an unresolved source switch for "+row.EntityID, "cannot tell whether this change would interrupt its access control; "+rawNextUI), nil
			} // a wrapper on the device whose source can't be read counts as wrapping it
		}
		owned := row.EntityID == target.entity || (target.entry != "" && row.ConfigEntryID == target.entry) || (target.device != "" && row.DeviceID == target.device) || gone[row.DeviceID] || (target.toggled != "" && row.ConfigEntryID == target.toggled) || wrapped
		if !owned || !accessCapable(row.EntityID, coverClass(s, row.EntityID)) {
			continue
		}
		if target.toggled != "" {
			return rawRefusal("disabling, enabling, or changing polling of the integration entry "+target.toggled+", which has "+row.EntityID+" (this can interrupt its lock, alarm panel, or cover, so a door can't be locked or the alarm armed)", rawNextUI), nil
		}
		if target.source != "" {
			return rawRefusal("disabling or enabling "+target.source+", which runs "+row.EntityID+" (a disabled lock, alarm panel, or cover stops working in Home Assistant, so a door can't be locked or the alarm armed)", rawNextUI), nil
		}
		if target.device != "" {
			return rawRefusal("disabling or enabling device "+target.device+", which has "+row.EntityID+" (a disabled lock, alarm panel, or cover stops working in Home Assistant, so a door can't be locked or the alarm armed)", rawNextUI), nil
		}
		if target.deleted != "" {
			return rawRefusal("deletion of device "+target.deleted+", which has "+row.EntityID, rawNextAccessTarget), nil
		}
		return rawRefusal("removal or reconfiguration of "+row.EntityID, rawNextAccessTarget), nil
	}
	return nil, nil
}

// rawDeletedDevices: the devices deleting id removes, read live: it and its
// child devices (rows whose parent_device_id is it; Home Assistant removes
// them with it). HA NOVA's own App device is refused.
func rawDeletedDevices(s *relaySession, id string) (gone map[string]bool, refusal, err error) {
	var rows []struct {
		ID             string         `json:"id"`
		ParentDeviceID string         `json:"parent_device_id"`
		Identifiers    [][]registryID `json:"identifiers"`
	}
	data, err := s.readFresh("ws", map[string]any{"type": "config/device_registry/list"}, relaySessionRequestTimeout)
	if err == nil {
		if uerr := json.Unmarshal(data, &rows); uerr != nil {
			err = fmt.Errorf("cannot parse the device registry: %w", uerr)
		} else if rows == nil {
			err = errors.New("the device registry came back empty")
		}
	}
	if err != nil {
		return nil, nil, errors.New("cannot read which devices this request would delete, so nothing was sent: " + err.Error())
	}
	gone = map[string]bool{}
	for _, row := range rows {
		if row.ID != id && row.ParentDeviceID != id {
			continue
		}
		gone[row.ID] = true
		for _, pair := range row.Identifiers {
			if len(pair) == 2 && (pair[0] == "ha_nova" || pair[0] == "hassio" && strings.HasSuffix(string(pair[1]), "_ha_nova_relay")) {
				return nil, rawRefusal("deletion of HA NOVA's own App device", "HA NOVA would lose its way into Home Assistant; remove the App in the Home Assistant UI (Settings > Apps)"), nil
			}
		}
	}
	if !gone[id] {
		return nil, rawRefusal("deletion of unknown device "+id, "read the device registry again and resolve its current device_id"), nil
	}
	return gone, nil, nil
}

// rawAccountKept: the owner, a system account, and the account HA NOVA
// signs in with keep their access (administrator flag, active state,
// local-only sign-in), as skills/admin keeps them from deletion; read live,
// an unreadable list or an unknown account sends nothing.
func rawAccountKept(s *relaySession, id string) (refusal, err error) {
	var users []struct {
		ID              string `json:"id"`
		IsOwner         bool   `json:"is_owner"`
		SystemGenerated bool   `json:"system_generated"`
	}
	var current struct {
		ID string `json:"id"`
	}
	data, err := rawRead(s, "ws", map[string]any{"type": "config/auth/list"})
	if err == nil {
		err = json.Unmarshal(data, &users)
	}
	if err == nil {
		data, err = rawRead(s, "ws", map[string]any{"type": "auth/current_user"})
	}
	if err == nil {
		err = json.Unmarshal(data, &current)
	}
	if err == nil && (users == nil || current.ID == "") {
		err = errors.New("the account list or HA NOVA's own account came back empty")
	}
	if err != nil {
		return nil, errors.New("cannot read the accounts this request would change, so nothing was sent: " + err.Error())
	}
	if id == current.ID {
		return rawRefusal("access change of HA NOVA's own account", "HA NOVA would lose its way into Home Assistant; "+rawNextUI), nil
	}
	for _, user := range users {
		switch {
		case user.ID != id:
		case user.IsOwner:
			return rawRefusal("access change of the owner's account (Home Assistant would be left without its administrator)", rawNextUI), nil
		case user.SystemGenerated:
			return rawRefusal("access change of a system account (it belongs to an integration, not a person)", rawNextUI), nil
		default:
			return nil, nil
		}
	}
	return rawRefusal("access change of unknown account "+id, "read config/auth/list again and use the account's current id"), nil
}

// destructiveButtonText: a destructive word as a whole token, as
// buttonPressTier reads entity_id and name.
func destructiveButtonText(text string) bool {
	for _, token := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if slices.Contains(destructiveButtonWords, token) {
			return true
		}
	}
	return false
}

// coverClass: a cover's effective class from its state (the registry list
// carries none), else, for a cover with no state (disabled), from its
// registry entry; "" for other domains or when unreadable, which fails closed
// (accessCapable treats a cover with no known class as access-capable).
// Read live, never from the session memo: an earlier send may have changed it.
func coverClass(s *relaySession, entityID string) string {
	if !strings.HasPrefix(entityID, "cover.") {
		return ""
	}
	data, err := s.readFresh("core", map[string]any{"method": http.MethodGet, "path": "/api/states/" + entityID}, relaySessionRequestTimeout)
	var core struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}
	var st struct {
		Attributes struct {
			DeviceClass string `json:"device_class"`
		} `json:"attributes"`
	}
	if err == nil && json.Unmarshal(data, &core) == nil && core.Status == 200 {
		if json.Unmarshal(core.Body, &st) != nil {
			return ""
		}
		return st.Attributes.DeviceClass
	}
	if err != nil || core.Status != 404 {
		return ""
	}
	var entry struct {
		DeviceClass         string `json:"device_class"`
		OriginalDeviceClass string `json:"original_device_class"`
	}
	data, err = s.readFresh("ws", map[string]any{"type": "config/entity_registry/get", "entity_id": entityID}, relaySessionRequestTimeout)
	if err != nil || json.Unmarshal(data, &entry) != nil {
		return ""
	}
	return cmp.Or(entry.DeviceClass, entry.OriginalDeviceClass)
}
