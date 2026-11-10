package main

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"
)

// Read-ahead for the consumer scan (docs/work/2026-11-03-cut-plan-drift-spec.md).
// The walk is depth first and reads one entity's consumers at a time, so a
// home with a few levels of automations spent its read budget on round trips
// and was cut at another point each run. readAhead reads the same things
// breadth first, a level at a time and in parallel, into the session memo
// only: the walk still decides everything, in its own order, from those
// answers, so the plan is the one a slower walk would make.

// readAhead warms the memo for the consumer scan of ids, which the walk
// reached at depth, and, level by level, of every entity a consumer whose
// triggers reference them acts on by name, within the walk's own limits
// (depth, acted entities, consumer configs); ids themselves, which the walk
// scans next, always.
func (e *tierEngine) readAhead(ids []string, depth int) {
	if e.s.replaying() { // a replay read the preview's reads first, and only those
		return
	}
	for first := true; len(ids) > 0 && depth <= tierMaxDepth && e.s.ctx.Err() == nil; first, depth = false, depth+1 {
		var level []string
		for _, id := range ids {
			if !e.ahead[id] && entityIDPattern.MatchString(id) && (first || len(level)+e.aheadActed < tierMaxActed) {
				e.ahead[id] = true
				level = append(level, id)
			}
		}
		e.aheadActed += len(level)
		var reads []func()
		for _, id := range level {
			reads = append(reads, func() { _, _, _ = e.rawState(id) })
			items, _, _ := e.relatedItems(id) // an unreadable registry: the walk's scan says so
			for _, item := range items {
				reads = append(reads, func() { _, _ = e.s.ws(e.relatedRequest(item[0], item[1])) })
			}
		}
		prefetch(reads)
		on := map[string][]string{}                                  // consumer → the entities of this level it is related to
		named, places := map[string]bool{}, map[string][][2]string{} // places: where it was found, when not by name (scanConsumers)
		for _, id := range level {
			items, direct, _ := e.relatedItems(id)
			for i, item := range items {
				related, _ := e.related(item[0], item[1])
				for _, automation := range related["automation"] {
					on[automation] = append(on[automation], id)
					if i < direct {
						named[automation] = true
					} else {
						places[automation] = append(places[automation], item)
					}
				}
			}
		}
		var automations, placeOnly []string
		for automation := range on {
			if e.ahead["consumer "+automation] {
				continue
			}
			if named[automation] {
				automations = append(automations, automation)
			} else {
				placeOnly = append(placeOnly, automation)
			}
		}
		sort.Strings(automations)
		sort.Strings(placeOnly)
		automations = automations[:min(len(automations), max(tierMaxConsumers-e.aheadConfigs, 0))]
		e.aheadConfigs += len(automations)
		automations = append(automations, placeOnly...) // uncapped, like scanConsumers: the read deadline bounds them
		configs := make([]map[string]any, len(automations))
		reads = nil
		for i, automation := range automations {
			e.ahead["consumer "+automation] = true
			reads = append(reads, func() { configs[i] = e.warmConfig(automation) })
		}
		prefetch(reads)
		ids = nil
		for i, automation := range automations {
			if (named[automation] || targetsPlace(configs[i], places[automation])) && slices.ContainsFunc(on[automation], func(id string) bool { return triggerMatch(configs[i], id, e.needles(id), stateDelta{}) != "" }) {
				ids = append(ids, actionTargets(literalActions(configs[i]))...)
			}
		}
		sort.Strings(ids)
		ids = slices.Compact(ids)
	}
}

// needles: what a consumer's trigger names when it reacts to id.
func (e *tierEngine) needles(id string) []string {
	if device := e.reg[id].di; device != "" {
		return []string{id, device}
	}
	return []string{id}
}

// warmConfig reads an automation's config as runConfig does (the one Home
// Assistant loaded), blueprint substituted, through the session only; nil
// when it can't.
func (e *tierEngine) warmConfig(automation string) map[string]any {
	raw, ok, err := loadedConfig(e.s, automation, "automation")
	if err != nil || !ok {
		return nil
	}
	return e.warmBlueprint("automation", raw)
}

// warmBlueprint substitutes a config's blueprint through the session, as
// parseConfig asks for it; the config itself when it uses none.
func (e *tierEngine) warmBlueprint(domain string, raw json.RawMessage) map[string]any {
	var cfg map[string]any
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	bp, isBlueprint := cfg["use_blueprint"].(map[string]any)
	if !isBlueprint {
		return cfg
	}
	var sub struct {
		Config map[string]any `json:"substituted_config"`
	}
	if e.s.wsDecode(blueprintRequest(domain, bp), &sub, "the blueprint substitution") != nil || sub.Config == nil {
		return nil
	}
	return withInstanceKeys(domain, sub.Config, cfg)
}

func blueprintRequest(domain string, bp map[string]any) map[string]any {
	input := bp["input"]
	if input == nil {
		input = map[string]any{}
	}
	return map[string]any{"type": "blueprint/substitute", "domain": domain, "path": bp["path"], "input": input}
}

// actionTargets: the entity ids an action tree names literally, as walk
// reaches its steps: only those of a literal service (storedCall).
func actionTargets(node any) []string {
	var out []string
	switch n := node.(type) {
	case []any:
		for _, child := range n {
			out = append(out, actionTargets(child)...)
		}
	case map[string]any:
		if n["enabled"] == false || n["condition"] != nil || n["trigger"] != nil || n["platform"] != nil {
			return nil
		}
		name, _ := n["action"].(string)
		if name == "" {
			name, _ = n["service"].(string)
		}
		if name = strings.ToLower(name); name != "" && !isTemplated(name) && entityIDPattern.MatchString(name) && !readOnlyResponseServices[name] { // a read's target: classify scans nothing for it
			targets, _, _ := collectTargets(n)
			out = append(out, targets["entity_id"]...)
		}
		for _, key := range sortedKeys(n) {
			if !walkSkipKeys[key] {
				out = append(out, actionTargets(n[key])...)
			}
		}
	}
	return out
}
