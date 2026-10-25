package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Where the lines of a raw plan (plan_raw_say.go) read their names: one
// read per list and plan, never an id alone when a name can be read.

// rawNamer reads names for the lines, each list once per plan.
type rawNamer struct {
	s         *relaySession
	e         *tierEngine
	lists     map[string]map[string]string // list request → id → name
	prefs     map[string]any               // energy/get_prefs (rawItemTier)
	prefsOK   bool
	prefsRead bool
	snapshots map[string]any // the snapshot store's list, by file (snapshotListed)
	deleting  map[string]any // the listed entries of the copies the plan deletes
}

func newRawNamer(s *relaySession, e *tierEngine) *rawNamer {
	return &rawNamer{s: s, e: e, lists: map[string]map[string]string{}}
}

// rawRead sends one read the raw guard allows (names, energy prefs, a
// helper's list item), fresh.
func rawRead(s *relaySession, endpoint string, payload map[string]any) (json.RawMessage, error) {
	body, err := json.Marshal(payload)
	if err == nil && (checkRawRelayRequest(endpoint, body) != nil || !rawRelayRead(endpoint, body)) {
		err = fmt.Errorf("%s is not a raw read", body)
	}
	if err != nil {
		return nil, err
	}
	return s.sendWithin(endpoint, body, relaySessionRequestTimeout)
}

// names: id → name of one list read; an item's id is the first of idKeys
// it has, its name the first non-empty of nameKeys.
func (n *rawNamer) names(request map[string]any, idKeys, nameKeys []string) map[string]string {
	key, _ := json.Marshal(request)
	if m, ok := n.lists[string(key)]; ok {
		return m
	}
	m := map[string]string{}
	n.lists[string(key)] = m
	data, err := rawRead(n.s, "ws", request)
	var rows []map[string]any
	if err != nil || json.Unmarshal(data, &rows) != nil {
		// A list inside an object: person/list {storage, config},
		// assist_pipeline/pipeline/list {pipelines}, backup/info {backups}.
		var wrapped map[string]json.RawMessage
		if err != nil || json.Unmarshal(data, &wrapped) != nil {
			return m
		}
		for _, list := range wrapped {
			var more []map[string]any
			if json.Unmarshal(list, &more) == nil {
				rows = append(rows, more...)
			}
		}
	}
	for _, row := range rows {
		for _, idKey := range idKeys {
			id, _ := row[idKey].(string)
			if id == "" {
				continue
			}
			for _, nameKey := range nameKeys {
				if name, _ := row[nameKey].(string); name != "" && m[id] == "" {
					m[id] = name
				}
			}
			break
		}
	}
	return m
}

func (n *rawNamer) named(request map[string]any, id any, idKeys, nameKeys []string) string {
	text, _ := id.(string)
	if name := n.names(request, idKeys, nameKeys)[text]; name != "" {
		return sayText(name, true)
	}
	return sayText(text, true)
}

func (n *rawNamer) entity(id any) string {
	text, _ := id.(string)
	if st, err := n.e.state(text); err == nil && st != nil {
		if name := st.str("friendly_name"); sayText(name, false) != "" {
			return sayText(name, true)
		}
	}
	return sayText(text, true)
}

func (n *rawNamer) device(id any) string {
	return n.named(map[string]any{"type": "config/device_registry/list"}, id, []string{"id"}, []string{"name_by_user", "name"})
}

func (n *rawNamer) area(id any) string {
	if id == nil {
		return "no area"
	}
	return n.named(map[string]any{"type": "config/area_registry/list"}, id, []string{"area_id"}, []string{"name"})
}

func (n *rawNamer) entry(id any) string {
	return n.named(map[string]any{"type": "config_entries/get"}, id, []string{"entry_id"}, []string{"title", "domain"})
}

// calendarEvent names the event a delete removes, read from the calendar
// a year around today, and which occurrences.
func (n *rawNamer) calendarEvent(payload map[string]any) string {
	uid, _ := payload["uid"].(string)
	entity, _ := payload["entity_id"].(string)
	recurrence, _ := payload["recurrence_id"].(string)
	now := verbNow().UTC()
	path := fmt.Sprintf("/api/calendars/%s?start=%s&end=%s", entity, now.AddDate(-1, 0, 0).Format("2006-01-02T15:04:05Z"), now.AddDate(1, 0, 0).Format("2006-01-02T15:04:05Z"))
	var core struct {
		Body []struct {
			UID     string `json:"uid"`
			Summary string `json:"summary"`
			Start   struct {
				Date     string `json:"date"`
				DateTime string `json:"dateTime"`
			} `json:"start"`
		} `json:"body"`
	}
	name := "the event " + sayText(uid, true) // never found: its id is all there is
	if !entityIDPattern.MatchString(entity) {
		return name
	}
	if data, err := rawRead(n.s, "core", map[string]any{"method": http.MethodGet, "path": path}); err == nil && json.Unmarshal(data, &core) == nil {
		matches := 0
		for _, event := range core.Body {
			if event.UID != uid || sayText(event.Summary, false) == "" {
				continue
			}
			if matches++; matches == 1 {
				name = "the event " + sayText(event.Summary, true)
				if when := plainDate(firstNonEmpty(event.Start.DateTime, event.Start.Date)); when != "" {
					name += " (" + when + ")"
				}
			}
		}
		if matches > 1 && recurrence == "" {
			name += " with all its repeats"
		}
	}
	on := "one occurrence"
	if when := plainDate(recurrence); when != "" {
		on = "the one on " + when
	}
	switch {
	case recurrence != "" && payload["recurrence_range"] == "THISANDFUTURE":
		return name + ": " + on + " and every later one"
	case recurrence != "":
		return name + ": " + on + " only"
	}
	return name
}

// plainDate: a calendar date or time as people write it ("2 November 2026",
// "2 November 2026, 14:00"); "" when it can't be read.
func plainDate(text string) string {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "20060102T150405Z", "20060102T150405"} {
		if t, err := time.Parse(layout, text); err == nil {
			return t.Format("2 January 2006, 15:04")
		}
	}
	for _, layout := range []string{"2006-01-02", "20060102"} {
		if t, err := time.Parse(layout, text); err == nil {
			return t.Format("2 January 2006")
		}
	}
	return ""
}

// scene: a scene config's name, read through its own config.
func (n *rawNamer) scene(id string) string {
	data, err := rawRead(n.s, "core", map[string]any{"method": http.MethodGet, "path": "/api/config/scene/config/" + id})
	var core struct {
		Body struct {
			Name string `json:"name"`
		} `json:"body"`
	}
	if err == nil && json.Unmarshal(data, &core) == nil && sayText(core.Body.Name, false) != "" {
		return sayText(core.Body.Name, true)
	}
	return sayText(id, true)
}

// rawTargetLists: where a target id's name is read.
var rawTargetLists = map[string]struct {
	request    string
	ids, names []string
}{
	"user_id":      {"config/auth/list", []string{"id"}, []string{"name"}},
	"pipeline_id":  {"assist_pipeline/pipeline/list", []string{"id"}, []string{"name"}},
	"backup_id":    {"backup/info", []string{"backup_id"}, []string{"name"}},
	"repository":   {"hacs/repositories/list", []string{"id"}, []string{"full_name", "name"}},
	"resource_id":  {"lovelace/resources", []string{"id"}, []string{"url"}},
	"dashboard_id": {"lovelace/dashboards/list", []string{"id", "url_path"}, []string{"title"}},
}
