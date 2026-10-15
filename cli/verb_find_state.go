package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

type stateRow struct {
	EntityID   string `json:"entity_id"`
	State      string `json:"state"`
	Attributes struct {
		FriendlyName string `json:"friendly_name"`
		Members      any    `json:"entity_id"` // a list on group entities
	} `json:"attributes"`
}

// loadStates reads /api/states (once per session: the read is memoized).
func loadStates(s *relaySession) ([]stateRow, error) {
	status, body, err := s.coreGET("/api/states")
	var states []stateRow
	if err == nil && status != http.StatusOK {
		err = fmt.Errorf("Home Assistant answered HTTP %d for /api/states", status)
	}
	if err == nil {
		err = json.Unmarshal(body, &states)
	}
	return states, err
}

func statesUnreadNotice(err error) verbNotice {
	return verbNotice{Kind: "partial", Text: "only registry entities were searched (no YAML/template entities, registry names only): /api/states could not be read: " + err.Error()}
}

// stateHits: entities with a state but no registry row (YAML without
// unique_id) that match the phrase on entity_id or friendly_name — never
// under --area (their area is unknown), never by alias (they have none).
func stateHits(s *relaySession, states []stateRow, q findQuery) ([]verbTarget, error) {
	var compact struct {
		Entities []struct{ EI string } `json:"entities"`
	}
	if err := s.wsDecode(map[string]any{"type": "config/entity_registry/list_for_display"}, &compact, "the compact entity registry"); err != nil {
		return nil, err
	}
	known := map[string]bool{} // registered entities match through the registry
	for _, row := range compact.Entities {
		known[row.EI] = true
	}
	tokens := foldTokens(q.phrase)
	hits := []verbTarget{}
	for _, st := range states {
		if known[st.EntityID] || !inState(q, st.State) || q.area != "" ||
			(q.domain != "" && !strings.HasPrefix(st.EntityID, q.domain+".")) ||
			!tokensMatch(tokens, st.EntityID+" "+st.Attributes.FriendlyName) {
			continue
		}
		row := verbTarget{EntityID: st.EntityID, Name: strings.TrimSpace(st.Attributes.FriendlyName), By: "state"}
		if q.state != "" {
			row.State = st.State
		}
		hits = append(hits, row)
	}
	return hits, nil
}

func inState(q findQuery, state string) bool {
	return q.state == "" || strings.EqualFold(strings.TrimSpace(state), q.state)
}

// mergeStates: with --state it keeps the registry matches in that state
// (case-insensitive), then adds the stateHits not already matched — never
// dropped silently. seen: some state has the --domain prefix.
//
// Name rule: a match shows the state's friendly_name when it has one. Home
// Assistant sets friendly_name to a user-set registry name verbatim, so this
// only replaces the registry fallback (original_name, e.g. the bare "Lock" of
// a has_entity_name entity whose friendly_name is "Entrance Door Lock").
func mergeStates(states []stateRow, friendly map[string]string, matches, hits []verbTarget, q findQuery) ([]verbTarget, bool) {
	seen := false
	current, group := map[string]string{}, map[string]bool{}
	for _, st := range states {
		current[st.EntityID] = st.State
		seen = seen || (q.domain != "" && strings.HasPrefix(st.EntityID, q.domain+"."))
		if domain, _ := splitID(st.EntityID); !notGroupDomains[domain] {
			_, group[st.EntityID] = st.Attributes.Members.([]any) // a group counts its members again
		}
	}
	kept, matched := []verbTarget{}, map[string]bool{}
	for _, m := range matches {
		matched[m.EntityID] = true
		state, ok := current[m.EntityID]
		if name := friendly[m.EntityID]; name != "" {
			m.Name = name
		}
		switch {
		case q.state == "": // a plain listing keeps every registered match
			m.Group = group[m.EntityID]
			kept = append(kept, m)
		case ok && inState(q, state):
			m.State, m.Group = state, group[m.EntityID]
			kept = append(kept, m)
		}
	}
	for _, h := range hits {
		if !matched[h.EntityID] {
			h.Group = group[h.EntityID]
			kept = append(kept, h)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].EntityID < kept[j].EntityID })
	return kept, seen
}
