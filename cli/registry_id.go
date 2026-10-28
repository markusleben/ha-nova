package main

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// registryID is an id an integration gave Home Assistant: an entity's
// unique_id or a device identifier. Home Assistant does not force it to a
// string: some integrations (Nuki) store a JSON number, and one such row must
// not break the whole registry list. A number keeps its exact
// digits, null is empty, anything else fails loud.
type registryID string

func (id *registryID) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*id = registryID(s)
		return nil
	}
	if bytes.Equal(b, []byte("null")) {
		*id = ""
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var n json.Number
	if err := dec.Decode(&n); err == nil {
		*id = registryID(n.String())
		return nil
	}
	return fmt.Errorf("unique_id: unsupported JSON value %s", b)
}
