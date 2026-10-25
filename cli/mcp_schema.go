package main

import (
	"encoding/json"
	"fmt"
)

// checkArguments enforces the schema's required keys, known keys, and
// types, also inside arrays of objects (ha_plan kind raw's requests), so no
// argument can turn into a CLI flag.
func checkArguments(schema map[string]any, args map[string]json.RawMessage) error {
	values := map[string]any{}
	for key, raw := range args {
		var value any
		if json.Unmarshal(raw, &value) != nil {
			return fmt.Errorf("argument %q is not JSON", key)
		}
		values[key] = value
	}
	return checkObject(schema, values, "")
}

// checkObject checks one object against its schema; prefix names where a
// nested one sits ("requests[0].").
func checkObject(schema, values map[string]any, prefix string) error {
	properties, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]string)
	for _, key := range required {
		if _, present := values[key]; !present {
			return fmt.Errorf("missing required argument %q", prefix+key)
		}
	}
	for _, key := range sortedKeys(values) {
		spec, known := properties[key].(map[string]any)
		if !known {
			return fmt.Errorf("unknown argument %q", prefix+key)
		}
		if err := checkValue(spec, values[key], prefix+key); err != nil {
			return err
		}
	}
	return nil
}

func checkValue(spec map[string]any, value any, name string) error {
	ok := false
	switch spec["type"] {
	case "string":
		text, isText := value.(string)
		enum, hasEnum := spec["enum"].([]string)
		ok = isText && (!hasEnum || containsText(enum, text))
	case "integer":
		n, isNumber := value.(float64)
		ok = isNumber && n == float64(int(n))
	case "boolean":
		_, ok = value.(bool)
	case "object":
		object, isObject := value.(map[string]any)
		if _, nested := spec["properties"]; isObject && nested {
			return checkObject(spec, object, name+".")
		}
		ok = isObject
	case "array":
		items, isList := value.([]any)
		least, _ := spec["minItems"].(int)
		most, bounded := spec["maxItems"].(int)
		if isList && (len(items) < least || bounded && len(items) > most) {
			return fmt.Errorf("argument %q needs %d to %d items", name, least, most)
		}
		itemSpec, _ := spec["items"].(map[string]any)
		for i, item := range items {
			if err := checkValue(itemSpec, item, fmt.Sprintf("%s[%d]", name, i)); err != nil {
				return err
			}
		}
		ok = isList
	}
	if !ok {
		return fmt.Errorf("argument %q has the wrong type", name)
	}
	return nil
}

func containsText(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
