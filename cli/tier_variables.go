package main

import (
	"regexp"
	"strings"
)

// An automation's literal top-level variables, bound once (MAR-127 PR2): in a
// consumer run (its own trigger fired), a step target written "{{ name }}"
// names that value, so the walk can classify it instead of an unresolved
// target. A name any other place can bind (a variables step or trigger
// variables, a response_variable, trigger and the other run variables) stays
// a template. A run started by a call is never resolved: automation.trigger
// passes variables that win over these defaults, and a script's fields come
// from its caller.

var bareVariable = regexp.MustCompile(`^\s*\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}\s*$`)

// plainIDs: ids, or ids separated by commas, that Home Assistant's native
// rendering keeps a string ("['garage']" would render as a list, "12" as a
// number); anything else stays a template.
var plainIDs = regexp.MustCompile(`^[a-z_][a-z0-9_]*(\.[a-z0-9_]+)?(\s*,\s*[a-z_][a-z0-9_]*(\.[a-z0-9_]+)?)*$`)

var runVariables = map[string]bool{"trigger": true, "this": true, "repeat": true, "wait": true, "context": true, "variables": true}

// literalActions answers an automation's actions with each bare reference to
// a literal top-level variable replaced by its value.
func literalActions(cfg map[string]any) any {
	actions := automationActions(cfg)
	declared, _ := cfg["variables"].(map[string]any)
	values := map[string]any{}
	for name, value := range declared {
		if literalValue(value) && !runVariables[name] {
			values[name] = value
		}
	}
	rest := map[string]any{}
	for key, node := range cfg {
		if key != "variables" {
			rest[key] = node
		}
	}
	dropRebound(rest, values)
	if len(values) == 0 {
		return actions
	}
	return substitute(actions, values)
}

// literalValue: plain ids, or a list of them.
func literalValue(value any) bool {
	switch v := value.(type) {
	case string:
		return plainIDs.MatchString(v)
	case []any:
		for _, item := range v {
			if s, isString := item.(string); !isString || !plainIDs.MatchString(s) {
				return false
			}
		}
		return true
	}
	return false
}

// dropRebound removes every name the node can bind at runtime.
func dropRebound(node any, values map[string]any) {
	switch n := node.(type) {
	case []any:
		for _, child := range n {
			dropRebound(child, values)
		}
	case map[string]any:
		for key, child := range n {
			switch bound := child.(type) {
			case map[string]any:
				if key == "variables" || key == "trigger_variables" {
					for name := range bound {
						delete(values, name)
					}
				}
			case string:
				if key == "response_variable" {
					delete(values, strings.TrimSpace(bound))
				}
			}
			dropRebound(child, values)
		}
	}
}

func substitute(node any, values map[string]any) any {
	switch n := node.(type) {
	case []any:
		out := make([]any, len(n))
		for i, child := range n {
			out[i] = substitute(child, values)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(n))
		for key, child := range n {
			out[key] = substitute(child, values)
		}
		return out
	case string:
		if m := bareVariable.FindStringSubmatch(n); m != nil {
			if value, literal := values[m[1]]; literal {
				return value
			}
		}
	}
	return node
}
