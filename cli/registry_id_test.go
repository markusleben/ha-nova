package main

import (
	"encoding/json"
	"testing"
)

// Home Assistant keeps a unique_id as the integration gave it: Nuki stores a
// JSON number. One such row must not break the whole registry list.
func TestRegistryUniqueIDAcceptsNumbers(t *testing.T) {
	list := []byte(`[{"entity_id":"lock.haustur","platform":"nuki","unique_id":743657972},
		{"entity_id":"sensor.big","platform":"x","unique_id":123456789012345678901234567890},
		{"entity_id":"light.hall","platform":"hue","unique_id":"hue-1"},
		{"entity_id":"light.none","platform":"hue","unique_id":null},
		{"entity_id":"light.missing","platform":"hue"}]`)
	want := []string{"743657972", "123456789012345678901234567890", "hue-1", "", ""}
	var full []fullRegistryRow
	if err := json.Unmarshal(list, &full); err != nil {
		t.Fatalf("full registry: %v", err)
	}
	var refs []refRegistryRow
	if err := json.Unmarshal(list, &refs); err != nil {
		t.Fatalf("references registry: %v", err)
	}
	for i, id := range want {
		if string(full[i].UniqueID) != id || string(refs[i].UniqueID) != id {
			t.Fatalf("row %d: got %q / %q, want %q", i, full[i].UniqueID, refs[i].UniqueID, id)
		}
	}
	var bad []fullRegistryRow
	if err := json.Unmarshal([]byte(`[{"entity_id":"x.y","unique_id":{"a":1}}]`), &bad); err == nil {
		t.Fatal("an object unique_id must fail loud")
	}
}

func TestPlanWriteWithANumericUniqueIDInTheRegistry(t *testing.T) {
	f := newFakeHA()
	f.full = append(f.full, map[string]any{"entity_id": "lock.haustur", "platform": "nuki", "unique_id": json.Number("743657972")})
	h := newWriteHarness(t, f)
	exit, env := h.run(runPlanCommand, "write", "--domain", "automation", "--file", draftFile(t, hallDraft("light.turn_on")))
	if exit != 3 || env.Error != nil || env.Plan.ID == "" {
		t.Fatalf("plan write: exit %d error %+v", exit, env.Error)
	}
	if exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID); exit != 0 {
		t.Fatalf("apply: exit %d env %+v", exit, applied)
	}
}
