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

// humanizeLabel turns a path into a stable, readable label, e.g.
// [mode] -> "Mode", [actions,1,delay] -> "Action 2 (delay)". When an index
// segment carries a semantic anchor, the anchor replaces the index chain
// collected so far — structural or/and wrappers mean nothing to a
// non-technical user, the entity does. Branch tokens (choose/parallel) are
// the exception and survive the reset: two identical conditions in two
// choose branches (or the same service in then vs else) must not collapse
// into indistinguishable rows in the pre-write confirmation table.
func humanizeLabel(segs []segment) string {
	var head string
	var branches []string
	var leaf []string
	for i := 0; i < len(segs); {
		s := segs[i]
		if s.isIndex {
			leaf = append(leaf, fmt.Sprintf("#%d", s.index+1))
			i++
			continue
		}
		if i+1 < len(segs) && segs[i+1].isIndex {
			name := arraySingular[s.key]
			if name == "" {
				name = titleFirst(s.key)
			}
			token := fmt.Sprintf("%s %d", name, segs[i+1].index+1)
			lower := strings.ToLower(name) + fmt.Sprintf(" %d", segs[i+1].index+1)
			switch {
			case head == "" && len(leaf) == 0 && len(branches) == 0:
				head = token
			case s.key == "choose" || s.key == "parallel" || s.key == "then" || s.key == "else":
				branches = append(branches, lower)
			case segs[i+1].anchor != "":
				leaf = []string{segs[i+1].anchor}
			default:
				leaf = append(leaf, lower)
			}
			i += 2
			continue
		}
		if head == "" && len(leaf) == 0 && len(branches) == 0 {
			head = titleFirst(s.key)
		} else {
			leaf = append(leaf, s.key)
		}
		i++
	}
	full := append(append([]string{}, branches...), leaf...)
	switch {
	case len(full) == 0:
		return head
	case head == "":
		return strings.Join(full, " › ")
	default:
		return head + " (" + strings.Join(full, " › ") + ")"
	}
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
