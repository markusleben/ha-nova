package main

import (
	"fmt"
	"strings"
)

// This file owns the FIELD column of `ha-nova diff`: turning a change path
// into the stable, layperson-readable label, including the semantic anchors
// that replace index chains with the entity/alias a user recognizes. Cell
// rendering and value formatting live in diff_format.go; the tree walk in
// diff.go. Split keeps each file under the repo's ~400-line guardrail.

var arraySingular = map[string]string{
	"triggers": "Trigger", "conditions": "Condition", "actions": "Action", "sequence": "Step",
}

func titleFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// labelToken is one path-ordered piece of a label. Positional tokens
// ("condition 2", "#3") are structural index noise a semantic anchor may
// replace; everything else — object keys ("repeat", "below") and branch
// tokens — is real context and always survives.
type labelToken struct {
	text       string
	positional bool
}

// branchTokenKeys are array keys whose position IS the meaning: the same
// service in two choose branches, in then vs else, or in a loop guard vs
// the loop body must never collapse into indistinguishable rows in the
// pre-write confirmation table. Their tokens outrank any anchor.
var branchTokenKeys = map[string]bool{
	"choose": true, "parallel": true, "then": true, "else": true, "while": true, "until": true,
}

// humanizeLabel turns a path into a stable, readable label, e.g.
// [mode] -> "Mode", [actions,1,delay] -> "Action 2 (delay)". When an index
// segment carries a semantic anchor, the anchor replaces the positional
// tokens collected so far — structural or/and wrappers mean nothing to a
// non-technical user, the entity does.
func humanizeLabel(segs []segment) string {
	var head string
	var tokens []labelToken
	for i := 0; i < len(segs); {
		s := segs[i]
		if s.isIndex {
			tokens = append(tokens, labelToken{text: fmt.Sprintf("#%d", s.index+1), positional: true})
			i++
			continue
		}
		if i+1 < len(segs) && segs[i+1].isIndex {
			name := arraySingular[s.key]
			if name == "" {
				name = titleFirst(s.key)
			}
			token := fmt.Sprintf("%s %d", name, segs[i+1].index+1)
			switch {
			case head == "" && len(tokens) == 0:
				head = token
			case branchTokenKeys[s.key]:
				tokens = append(tokens, labelToken{text: strings.ToLower(token)})
			case segs[i+1].anchor != "":
				tokens = append(withoutPositional(tokens), labelToken{text: segs[i+1].anchor})
			default:
				tokens = append(tokens, labelToken{text: strings.ToLower(token), positional: true})
			}
			i += 2
			continue
		}
		if head == "" && len(tokens) == 0 {
			head = titleFirst(s.key)
		} else {
			tokens = append(tokens, labelToken{text: s.key})
		}
		i++
	}
	parts := make([]string, len(tokens))
	for i, t := range tokens {
		parts[i] = t.text
	}
	switch {
	case len(parts) == 0:
		return head
	case head == "":
		return strings.Join(parts, " › ")
	default:
		return head + " (" + strings.Join(parts, " › ") + ")"
	}
}

func withoutPositional(tokens []labelToken) []labelToken {
	kept := tokens[:0]
	for _, t := range tokens {
		if !t.positional {
			kept = append(kept, t)
		}
	}
	return kept
}

// configItemAnchor names one list item for the change label — the thing a
// non-technical user recognizes. Alias beats everything, then the service,
// then the entity of a trigger/condition. Pure structural wrappers (or/and/
// not) return "" so the walk continues to something recognizable; unknown
// shapes return "" and keep today's index fallback.
func configItemAnchor(v interface{}) string {
	m, ok := v.(map[string]interface{})
	if !ok {
		return ""
	}
	if alias, _ := m["alias"].(string); alias != "" {
		return fmt.Sprintf("%q", alias)
	}
	if s, _ := m["action"].(string); s != "" {
		return "action " + s
	}
	if s, _ := m["service"].(string); s != "" {
		return "action " + s
	}
	entity, _ := m["entity_id"].(string)
	if s, _ := m["platform"].(string); s != "" {
		return withEntityAnchor("trigger "+s, entity)
	}
	if s, _ := m["trigger"].(string); s != "" {
		return withEntityAnchor("trigger "+s, entity)
	}
	if s, _ := m["condition"].(string); s != "" {
		if s == "or" || s == "and" || s == "not" {
			return ""
		}
		if entity != "" {
			return "condition " + entity
		}
		return "condition " + s
	}
	return ""
}

func withEntityAnchor(base, entity string) string {
	if entity == "" {
		return base
	}
	return base + " " + entity
}

// itemAnchorFor is the single gate for anchoring: items inside a service
// call's payload (data/target/variables) are payload shapes — an actionable
// notification button carries its own action/title keys and would mislabel as
// a service call — so they never anchor. An anchor another sibling in the
// same array would also produce (two light.turn_on steps in one sequence)
// is dropped too: it cannot tell the rows apart, the positional token can.
// Every pass that builds an index segment must go through this, or the dedup
// texts of the aligned and notification passes drift apart and one change
// renders twice.
func itemAnchorFor(segs []segment, siblings []interface{}, item interface{}) string {
	for _, s := range segs {
		if !s.isIndex && (s.key == "data" || s.key == "target" || s.key == "variables") {
			return ""
		}
	}
	anchor := configItemAnchor(item)
	if anchor == "" {
		return ""
	}
	seen := 0
	for _, sibling := range siblings {
		if configItemAnchor(sibling) == anchor {
			seen++
		}
	}
	if seen > 1 {
		return ""
	}
	return anchor
}
