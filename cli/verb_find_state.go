package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// mergeStates reads /api/states once: with --state it keeps the matches in
// that state (case-insensitive); either way entities with a state but no
// registry row (YAML without unique_id) that match the phrase on entity_id or
// friendly_name are added when no area filter applies — never dropped
// silently; their area is unknown, so --area cannot include them, and they
// carry no aliases. seen: some state has the --domain prefix.
//
// Name rule: a match shows the state's friendly_name when it has one. Home
// Assistant sets friendly_name to a user-set registry name verbatim, so this
// only replaces the registry fallback (original_name, e.g. the bare "Lock" of
// a has_entity_name entity whose friendly_name is "Entrance Door Lock").
func mergeStates(s *relaySession, matches []verbTarget, q findQuery) ([]verbTarget, bool, error) {
	status, body, err := s.coreGET("/api/states")
	var states []struct {
		EntityID   string `json:"entity_id"`
		State      string `json:"state"`
		Attributes struct {
			FriendlyName string `json:"friendly_name"`
			Members      any    `json:"entity_id"` // a list on group entities
		} `json:"attributes"`
	}
	if err == nil && status != http.StatusOK {
		err = fmt.Errorf("Home Assistant answered HTTP %d for /api/states", status)
	}
	if err == nil {
		err = json.Unmarshal(body, &states)
	}
	if err != nil {
		return nil, false, err
	}
	inState := func(state string) bool { return q.state == "" || strings.EqualFold(strings.TrimSpace(state), q.state) }
	seen := false
	current, friendly, group := map[string]string{}, map[string]string{}, map[string]bool{}
	for _, st := range states {
		current[st.EntityID] = st.State
		friendly[st.EntityID] = strings.TrimSpace(st.Attributes.FriendlyName)
		seen = seen || (q.domain != "" && strings.HasPrefix(st.EntityID, q.domain+"."))
		if domain, _ := splitID(st.EntityID); !notGroupDomains[domain] {
			_, group[st.EntityID] = st.Attributes.Members.([]any) // a group counts its members again
		}
	}
	var compact struct {
		Entities []struct{ EI string } `json:"entities"`
	}
	if err := s.wsDecode(map[string]any{"type": "config/entity_registry/list_for_display"}, &compact, "the compact entity registry"); err != nil {
		return nil, false, err
	}
	known := map[string]bool{} // registered entities: matched (or not) through the registry above
	for _, row := range compact.Entities {
		known[row.EI] = true
	}
	kept := []verbTarget{}
	for _, m := range matches {
		known[m.EntityID] = true
		state, ok := current[m.EntityID]
		if name := friendly[m.EntityID]; name != "" {
			m.Name = name
		}
		switch {
		case q.state == "": // a plain listing keeps every registered match
			m.Group = group[m.EntityID]
			kept = append(kept, m)
		case ok && inState(state):
			m.State, m.Group = state, group[m.EntityID]
			kept = append(kept, m)
		}
	}
	tokens := foldTokens(q.phrase)
	for _, st := range states {
		if known[st.EntityID] || !inState(st.State) || q.area != "" ||
			(q.domain != "" && !strings.HasPrefix(st.EntityID, q.domain+".")) ||
			!tokensMatch(tokens, st.EntityID+" "+st.Attributes.FriendlyName) {
			continue
		}
		row := verbTarget{EntityID: st.EntityID, Name: friendly[st.EntityID], Group: group[st.EntityID], By: "state"}
		if q.state != "" {
			row.State = st.State
		}
		kept = append(kept, row)
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].EntityID < kept[j].EntityID })
	return kept, seen, nil
}
