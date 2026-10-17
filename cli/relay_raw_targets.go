package main

import (
	"encoding/json"
	"errors"
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
// word that makes its press typed. Before sending one, the CLI reads the
// registry and refuses it when the target is such an entity.

var rawEntryDeletePath = regexp.MustCompile(`^/api/config/config_entries/entry/([A-Za-z0-9_-]+)$`)

const rawNextAccessTarget = "it controls a lock, alarm panel, or access cover, and removing or reconfiguring it would change which actions need the one-time code; do this in the Home Assistant UI"

// rawAccessTarget names the entry or entity a raw request would remove or
// reconfigure ("" = not one of the three).
func rawAccessTarget(endpoint string, body []byte) (entryID, entityID, button string) {
	payload, ok := decodeObject(body)
	if !ok {
		return "", "", ""
	}
	switch endpoint {
	case "core":
		method, _ := payload["method"].(string)
		method = strings.ToUpper(strings.TrimSpace(method))
		raw, _ := payload["path"].(string)
		pathPart, _, _ := strings.Cut(raw, "#")
		pathPart, _, _ = strings.Cut(pathPart, "?")
		if m := rawEntryDeletePath.FindStringSubmatch(pathPart); m != nil && method == "DELETE" {
			return m[1], "", ""
		}
		inner, _ := payload["body"].(map[string]any)
		switch {
		case method != "POST":
		case pathPart == "/api/config/config_entries/options/flow": // handler is the entry id
			handler, _ := inner["handler"].(string)
			return handler, "", ""
		case pathPart == "/api/config/config_entries/flow": // a reconfigure flow names its entry
			entry, _ := inner["entry_id"].(string)
			return entry, "", ""
		}
	case "ws":
		for _, message := range []any{payload, payload["message"]} {
			m, _ := message.(map[string]any)
			id, _ := m["entity_id"].(string)
			id = strings.ToLower(id)
			_, renames := m["name"]
			_, newID := m["new_entity_id"]
			switch {
			case m["type"] == "config/entity_registry/remove":
				return "", id, ""
			case m["type"] == "config/entity_registry/update" && strings.HasPrefix(id, "button.") && (renames || newID):
				return "", "", id
			}
		}
	}
	return "", "", ""
}

// checkRawAccessTarget reads the registry for a request rawAccessTarget
// names: refusal is the safety rule (exit 4), err a failed read (exit 1) —
// either way nothing is sent unchecked.
func checkRawAccessTarget(paths runtimePaths, opts verbOptions, endpoint string, body []byte) (refusal, err error) {
	entryID, entityID, button := rawAccessTarget(endpoint, body)
	if entryID == "" && entityID == "" && button == "" {
		return nil, nil
	}
	s, err := newRelaySession(paths, opts)
	if err != nil {
		return nil, err
	}
	defer s.close()
	var rows []struct {
		EntityID      string `json:"entity_id"`
		ConfigEntryID string `json:"config_entry_id"`
		Name          string `json:"name"`
		OriginalName  string `json:"original_name"`
	}
	if err := s.wsDecode(map[string]any{"type": "config/entity_registry/list"}, &rows, "the entity registry"); err != nil {
		return nil, errors.New("cannot read what this request would remove or reconfigure, so nothing was sent: " + err.Error())
	}
	for _, row := range rows {
		if row.EntityID == button && destructiveButtonText(row.EntityID+" "+row.Name+" "+row.OriginalName) {
			return rawRefusal("rename of "+row.EntityID+" (its name marks a press as needing the one-time code)", rawNextUI), nil
		}
		if (row.EntityID == entityID || (entryID != "" && row.ConfigEntryID == entryID)) && accessCapable(row.EntityID, coverClass(s, row.EntityID)) {
			return rawRefusal("removal or reconfiguration of "+row.EntityID, rawNextAccessTarget), nil
		}
	}
	return nil, nil
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
// carries none); "" for other domains or when unreadable, which fails closed
// (accessCapable treats a cover with no known class as access-capable).
func coverClass(s *relaySession, entityID string) string {
	if !strings.HasPrefix(entityID, "cover.") {
		return ""
	}
	status, body, err := s.coreGET("/api/states/" + entityID)
	var st struct {
		Attributes struct {
			DeviceClass string `json:"device_class"`
		} `json:"attributes"`
	}
	if err != nil || status != 200 || json.Unmarshal(body, &st) != nil {
		return ""
	}
	return st.Attributes.DeviceClass
}
