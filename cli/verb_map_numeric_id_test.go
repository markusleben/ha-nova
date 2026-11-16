package main

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
)

// #1184: Home Assistant keeps an automation's YAML id as a number when it was
// written as one (id: 1700000000000), in the entity registry's unique_id and
// in the automation's id attribute. The map reads it as text, and an id it
// can't read leaves that one entity without it, named, never the whole section.
func TestMapAutomationsReadNumericIDs(t *testing.T) {
	f := smallHome()
	for _, e := range f.entities {
		if e["entity_id"] == "automation.morning" {
			e["unique_id"] = json.Number("1700000000000")
		}
	}
	f.entities = append(f.entities, ent("automation.odd", "", "automation", "", map[string]any{"unique_id": map[string]any{"nested": true}}))
	f.states = append(f.states, st("automation.odd", "on", map[string]any{"friendly_name": "Odd", "id": json.Number("1700000000001"), "last_triggered": ago(time.Hour)}))
	exit, env, _ := runMap(t, f, "--section", "automations", "--limit", "200")
	if exit != 0 || env.Error != nil {
		t.Fatalf("exit %d, error %+v", exit, env.Error)
	}
	autos := decodeInto[struct {
		Rows       []mapRow `json:"rows"`
		Unreadable []string `json:"unreadable"`
	}](t, env.Result)
	names := []string{}
	for _, row := range autos.Rows {
		names = append(names, row.Name)
	}
	if !slices.Contains(names, "Morning") || !slices.Contains(names, "Odd") || !slices.Equal(autos.Unreadable, []string{"automation.odd"}) {
		t.Fatalf("rows %v, unreadable %v", names, autos.Unreadable)
	}
}

func TestAutomationConfigIDFromANumber(t *testing.T) {
	id, err := resolveConfigID(nil, "automation.morning", "automation", json.RawMessage(`{"attributes":{"id":1700000000000}}`))
	if err != nil || id != "1700000000000" {
		t.Fatalf("config id %q, err %v", id, err)
	}
}
