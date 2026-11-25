package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
)

// useStateList makes the check of a draft read every state from one GET
// /api/states (#1025). A routine reaches dozens of entities and their
// consumer automations, and one state read each cost a round trip over Home
// Assistant Link. Other checks keep reading the few states they need.
func (e *tierEngine) useStateList() {
	e.stateList = sync.OnceValues(func() (map[string]json.RawMessage, error) { return readStateList(e.s) })
}

// rawState reads one entity's state; present false: it has none (HTTP 404,
// or missing from the state list, which /api/states/<id> answers from).
// Safe to call concurrently (session reads only).
func (e *tierEngine) rawState(id string) (json.RawMessage, bool, error) {
	if e.stateList != nil {
		states, err := e.stateList()
		body, present := states[id]
		return body, present, err
	}
	status, body, err := e.s.coreGET("/api/states/" + id)
	switch {
	case err != nil:
		return nil, false, err
	case status == http.StatusOK:
		var head stateHead
		if json.Unmarshal(body, &head) != nil || head.EntityID != id || head.State == nil { // null or {} is no state: unread, never an empty one (MAR-272)
			return nil, false, fmt.Errorf("Home Assistant answered no state for %s", id)
		}
		return body, true, nil
	case status == http.StatusNotFound:
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("Home Assistant answered HTTP %d for %s", status, id)
}

// stateHead: what every state Home Assistant answers carries.
type stateHead struct {
	EntityID string  `json:"entity_id"`
	State    *string `json:"state"`
}

// stateListError: why an answer to GET /api/states is no state list, nil
// when it is one. Home Assistant always has states, so an empty or null
// list, or a row without an entity id, is no answer: never "every entity is
// absent" (MAR-272).
func stateListError(status int, body json.RawMessage, err error) error {
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("Home Assistant answered HTTP %d for /api/states", status)
	}
	var rows []stateHead
	if err := json.Unmarshal(body, &rows); err != nil {
		return fmt.Errorf("cannot parse the state list: %w", err)
	}
	if len(rows) == 0 || slices.ContainsFunc(rows, func(r stateHead) bool { return !strings.Contains(r.EntityID, ".") || r.State == nil }) {
		return errors.New("Home Assistant answered /api/states without a state list")
	}
	return nil
}

func readStateList(s *relaySession) (map[string]json.RawMessage, error) {
	status, body, err := s.coreGET("/api/states")
	if err := stateListError(status, body, err); err != nil {
		return nil, err
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	states := make(map[string]json.RawMessage, len(rows))
	for _, row := range rows {
		var head struct {
			EntityID string `json:"entity_id"`
		}
		if err := json.Unmarshal(row, &head); err != nil {
			return nil, fmt.Errorf("cannot parse the state list: %w", err)
		}
		states[head.EntityID] = row
	}
	return states, nil
}
