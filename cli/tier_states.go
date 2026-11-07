package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
		return body, true, nil
	case status == http.StatusNotFound:
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("Home Assistant answered HTTP %d for %s", status, id)
}

func readStateList(s *relaySession) (map[string]json.RawMessage, error) {
	status, body, err := s.coreGET("/api/states")
	if err == nil && status != http.StatusOK {
		err = fmt.Errorf("Home Assistant answered HTTP %d for /api/states", status)
	}
	var rows []json.RawMessage
	if err == nil {
		err = json.Unmarshal(body, &rows)
	}
	if err == nil && rows == nil { // a null body is no list: never "every entity is absent"
		err = errors.New("Home Assistant answered /api/states without a list")
	}
	if err != nil {
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
