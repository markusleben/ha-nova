package main

import (
	"regexp"
	"strconv"
	"strings"
)

// The URLs a raw WS write stores (Home Assistant's own, a blueprint's source,
// a person's or area's picture) never carry a password from the AI, as a
// setup step's or an App's setting doesn't: a URL with one, also one Go's
// URL parser can't read or reads without it (secretFlowValue), is refused in
// checkRawWS, so ha-nova relay ws, a raw plan, and a chat app's plan refuse
// it alike.

// rawURLFields: per WS type, the fields Home Assistant stores as a URL.
var rawURLFields = map[string][]string{
	"config/core/update": {"external_url", "internal_url"}, "blueprint/save": {"source_url"},
	"person/create": {"picture"}, "person/update": {"picture"},
	"config/area_registry/create": {"picture"}, "config/area_registry/update": {"picture"},
}

// rawYAMLSourceURL: a source_url key in a blueprint's YAML, which Home
// Assistant stores like the message's own (blueprint: source_url: …), its
// value on the same line or the next, after a tag, anchor, or block header.
var rawYAMLSourceURL = regexp.MustCompile(`(?m)["']?\bsource_url["']?[ \t]*:\s*(?:(?:[!&]\S*|[|>][-+0-9]*)\s+)*(.*)$`)

// rawURLSecret refuses a WS write that sets one of its URL fields, or its
// blueprint YAML's source_url, to a URL with a password.
func rawURLSecret(wsType string, message map[string]any) error {
	refusal := func(field string) error {
		return rawRefusal("WS "+wsType+" that sets "+strconv.Quote(field)+" (a password or other secret)", "secrets don't belong in a chat; "+rawNextUI)
	}
	for _, field := range rawURLFields[wsType] {
		if secretFlowValue(field, message[field]) {
			return refusal(field)
		}
	}
	if wsType != "blueprint/save" {
		return nil
	}
	yaml, _ := message["yaml"].(string)
	for _, m := range rawYAMLSourceURL.FindAllStringSubmatch(yaml, -1) {
		value := strings.TrimSpace(m[1])
		if quote := value[:min(1, len(value))]; quote == `"` || quote == "'" {
			value, _, _ = strings.Cut(value[1:], quote)
		} else {
			value, _, _ = strings.Cut(value, " #")
			value, _, _ = strings.Cut(value, ", ") // a flow mapping's next key
			value = strings.TrimRight(value, " ,}")
		}
		if secretFlowValue("source_url", value) {
			return refusal("source_url")
		}
	}
	return nil
}
