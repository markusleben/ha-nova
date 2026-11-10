package main

import (
	"encoding/json"
	"time"
)

// configLoadPoll, configLoadWait: how often and how long apply looks for the
// saved config in what Home Assistant runs. Its config view reloads after a
// save (an automation by id, scripts in full); apply sends no reload of its
// own and only reads. Tests shorten both.
var configLoadPoll, configLoadWait = 500 * time.Millisecond, 15 * time.Second

// configLoadFloor: the shortest look on the CLI once the command's budget is
// spent (a late approval, a long batch): Home Assistant reloads after its 200.
const configLoadFloor = 3 * time.Second

// awaitLoaded polls, read-only, until Home Assistant has loaded the planned
// config: the registry holds the entity for the config id (a create waits
// for it) and WS automation/config or script/config returns the plan
// (loadedAsPlanned). A registry-disabled entity is never loaded and ends it
// at once. A failed read is no answer: the poll goes on to the deadline,
// within wait; registryErr is the last registry read's failure. The entity
// existing, or the save's 200, never counts.
func awaitLoaded(s *relaySession, action configAction, wait time.Duration) (entityID string, disabled, loaded bool, waited time.Duration, registryErr error) {
	start := time.Now()
	var expected json.RawMessage // a blueprint item's substitution, read once
	for {
		if entityID == "" {
			entityID, disabled, registryErr = registryEntityFor(s, action.Domain, action.ConfigID)
		}
		if entityID != "" && !disabled {
			loaded = loadedAsPlanned(s, action, entityID, &expected)
		}
		waited = time.Since(start)
		if loaded || disabled || waited+configLoadPoll > wait {
			return entityID, disabled, loaded, waited, registryErr
		}
		time.Sleep(configLoadPoll)
	}
}

// loadedAsPlanned: whether the config Home Assistant runs for entityID (its
// raw_config) is the plan. That is the planned body as saved, or for a
// blueprint item, which Home Assistant keeps substituted, the substitution
// with the plan's own keys over it (withInstanceKeys), compared by content.
func loadedAsPlanned(s *relaySession, action configAction, entityID string, expected *json.RawMessage) bool {
	data, err := s.readFresh("ws", map[string]any{"type": action.Domain + "/config", "entity_id": entityID}, relaySessionRequestTimeout)
	var got struct {
		Config json.RawMessage `json:"config"`
	}
	if err != nil || json.Unmarshal(data, &got) != nil {
		return false
	}
	if changes, err := configChanges(action.Body, got.Config); err == nil && len(changes) == 0 {
		return true
	}
	var body map[string]any
	_ = json.Unmarshal(action.Body, &body)
	bp, isBlueprint := body["use_blueprint"].(map[string]any)
	if *expected == nil && isBlueprint {
		var sub struct {
			Config map[string]any `json:"substituted_config"`
		}
		if data, err := s.readFresh("ws", blueprintRequest(action.Domain, bp), relaySessionRequestTimeout); err == nil && json.Unmarshal(data, &sub) == nil && sub.Config != nil {
			*expected, _ = json.Marshal(withInstanceKeys(action.Domain, sub.Config, body))
		}
	}
	want, wantErr := configObjectFromBytes(*expected)
	loaded, err := configObjectFromBytes(got.Config)
	return wantErr == nil && err == nil && len(renderConfigChanges(want, loaded)) == 0
}
