package main

import (
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// yamlMapping: n's pairs by literal string key; a non-mapping (nil is
// empty), a duplicate, a merge key, or any other key is unknown.
func yamlMapping(n *yaml.Node) (map[string]*yaml.Node, error) {
	out := map[string]*yaml.Node{}
	if n == nil || n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null" {
		return out, nil
	}
	if n.Kind != yaml.MappingNode {
		return nil, yamlFail(yamlUnread)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, ok := yamlString(n.Content[i])
		if _, dup := out[key]; !ok || dup {
			return nil, yamlFail(yamlUnread)
		}
		out[key] = n.Content[i+1]
	}
	return out, nil
}

// yamlPlain: a plain scalar both YAML 1.1 (Home Assistant) and 1.2 (here)
// read as this same string.
var yamlPlain = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_. \-]*$`)

var yaml11Words = map[string]bool{"y": true, "yes": true, "n": true, "no": true, "true": true, "false": true, "on": true, "off": true, "null": true}

func yamlString(n *yaml.Node) (string, bool) {
	if n == nil || n.Kind != yaml.ScalarNode || n.ShortTag() != "!!str" {
		return "", false
	}
	if n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0 {
		return n.Value, true
	}
	return n.Value, yamlPlain.MatchString(n.Value) && !yaml11Words[strings.ToLower(n.Value)]
}

func yamlList(n *yaml.Node) ([]*yaml.Node, error) {
	switch {
	case n == nil || n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null":
		return nil, nil
	case n.Kind == yaml.SequenceNode:
		return n.Content, nil
	case n.Kind == yaml.MappingNode:
		return []*yaml.Node{n}, nil
	}
	return nil, yamlFail(yamlUnread)
}

// projectDomain adds one domain value's helper entries and template switches.
func projectDomain(p *yamlProjection, domain string, value *yaml.Node) error {
	items, err := yamlList(value)
	for _, item := range items {
		m, err := yamlMapping(item)
		if err != nil {
			return err
		}
		platform, set := m["platform"]
		name, literal := yamlString(platform)
		switch {
		case set && !literal && (domain == "climate" || domain == "switch"):
			return yamlFail(yamlUnread)
		case domain == "climate" && name == "generic_thermostat":
			err = p.helper("generic_thermostat", m["heater"])
		case domain == "generic_hygrostat":
			err = p.helper("generic_hygrostat", m["humidifier"])
		case domain == "switch" && name == "template":
			err = p.legacy(m["switches"])
		case domain == "template":
			err = p.block(m)
		}
		if err != nil {
			return err
		}
	}
	return err
}

func (p *yamlProjection) helper(integration string, n *yaml.Node) error {
	id, ok := yamlString(n)
	if !ok || !entityIDPattern.MatchString(id) {
		return yamlFail(yamlUnread)
	}
	p.Helpers[integration] = append(p.Helpers[integration], id)
	return nil
}

// legacy: a legacy template switch platform's entries count as duplicates
// of a modern one with the same unique id; they are never bounded.
func (p *yamlProjection) legacy(n *yaml.Node) error {
	switches, err := yamlMapping(n)
	for _, entry := range switches {
		m, mapped := yamlMapping(entry)
		if mapped != nil {
			return mapped
		}
		p.add(m["unique_id"], "", nil)
	}
	return err
}

// block: a template block's switch entries, with Home Assistant's registry
// unique id (template/helpers.py: "<block unique_id>-<entry unique_id>").
func (p *yamlProjection) block(m map[string]*yaml.Node) error {
	trigger := slices.ContainsFunc([]string{"triggers", "trigger", "actions", "action", "conditions", "variables", "trigger_variables"}, func(k string) bool {
		_, set := m[k]
		return set
	})
	if _, set := m["use_blueprint"]; set {
		p.Unreadable = true // a blueprint's entities aren't in this file
		return nil
	}
	prefix := ""
	if n, set := m["unique_id"]; set {
		var ok bool
		if prefix, ok = yamlString(n); !ok {
			p.Unreadable = true
			return nil
		}
	}
	entries, err := yamlList(m["switch"])
	for _, entry := range entries {
		e, mapped := yamlMapping(entry)
		if mapped != nil {
			return mapped
		}
		if _, set := e["use_blueprint"]; set {
			p.Unreadable = true
			continue
		}
		if trigger {
			p.add(e["unique_id"], prefix, nil)
		} else {
			p.add(e["unique_id"], prefix, e)
		}
	}
	return err
}

// add counts one switch entry under its unique id; entry is nil for one
// that is never bounded (legacy, trigger-based).
func (p *yamlProjection) add(n *yaml.Node, prefix string, entry map[string]*yaml.Node) {
	if n == nil {
		return // no unique id: no registry row, so never a heater bounded here
	}
	uid, ok := yamlString(n)
	switch {
	case !ok:
		p.Unreadable = true
		return
	case uid == "":
		return
	case prefix != "":
		uid = prefix + "-" + uid
	}
	sw := p.Switches[uid]
	sw.Count++
	sw.Steps, sw.Plain = nil, false
	if entry != nil {
		sw.Steps, sw.Plain = yamlSteps(entry)
	}
	p.Switches[uid] = sw
}

var yamlTurn = regexp.MustCompile(`^(switch|fan|homeassistant)\.turn_(on|off)$`)

// yamlSteps: turn_on and turn_off when every step is exactly a literal
// turn_on/turn_off of a switch, fan, or homeassistant on literal entity ids.
func yamlSteps(entry map[string]*yaml.Node) ([]yamlStep, bool) {
	var steps []yamlStep
	for _, key := range []string{"turn_on", "turn_off"} {
		items, err := yamlList(entry[key])
		if err != nil {
			return nil, false
		}
		for _, item := range items {
			m, err := yamlMapping(item)
			if err != nil || len(m) != 2 {
				return nil, false
			}
			service, ids := m["action"], m["target"]
			if service == nil {
				service = m["service"]
			}
			if ids != nil {
				target, err := yamlMapping(ids)
				if err != nil || len(target) != 1 {
					return nil, false
				}
				ids = target["entity_id"]
			} else {
				ids = m["entity_id"]
			}
			name, ok := yamlString(service)
			if !ok || !yamlTurn.MatchString(name) || ids == nil {
				return nil, false
			}
			list := []*yaml.Node{ids}
			if ids.Kind == yaml.SequenceNode {
				list = ids.Content
			}
			step := yamlStep{Service: name}
			for _, n := range list {
				id, ok := yamlString(n)
				if !ok || !entityIDPattern.MatchString(id) {
					return nil, false
				}
				step.Targets = append(step.Targets, id)
			}
			if len(step.Targets) == 0 {
				return nil, false
			}
			steps = append(steps, step)
		}
	}
	return steps, true
}
