package main

import (
	"encoding/json"
	"errors"
	"net/http"
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

// readAhead warms the memo for the consumer scan of ids and, level by level,
// of every entity a consumer whose triggers reference them acts on by name,
// within the walk's own limits (depth, acted entities, consumer configs);
// ids themselves, which the walk scans next, always.
func (e *tierEngine) readAhead(ids []string) {
	if e.s.replaying() { // a replay read the preview's reads first, and only those
		return
	}
	for depth := 0; len(ids) > 0 && depth <= tierMaxDepth && e.s.ctx.Err() == nil; depth++ {
		var level []string
		for _, id := range ids {
			if !e.ahead[id] && entityIDPattern.MatchString(id) && (depth == 0 || len(level)+e.aheadActed < tierMaxActed) {
				e.ahead[id] = true
				level = append(level, id)
			}
		}
		e.aheadActed += len(level)
		var reads []func()
		for _, id := range level {
			reads = append(reads, func() { _, _, _ = e.s.coreGET("/api/states/" + id) })
			for _, item := range e.relatedItems(id) {
				reads = append(reads, func() { _, _ = e.s.ws(e.relatedRequest(item[0], item[1])) })
			}
		}
		prefetch(reads)
		on := map[string][]string{} // consumer → the entities of this level it is related to
		for _, id := range level {
			for _, item := range e.relatedItems(id) {
				related, _ := e.related(item[0], item[1])
				for _, automation := range related["automation"] {
					on[automation] = append(on[automation], id)
				}
			}
		}
		var automations []string
		for automation := range on {
			if !e.ahead["consumer "+automation] {
				automations = append(automations, automation)
			}
		}
		sort.Strings(automations)
		automations = automations[:min(len(automations), max(tierMaxConsumers-e.aheadConfigs, 0))]
		e.aheadConfigs += len(automations)
		configs := make([]map[string]any, len(automations))
		reads = nil
		for i, automation := range automations {
			e.ahead["consumer "+automation] = true
			reads = append(reads, func() { configs[i] = e.warmConfig(automation) })
		}
		prefetch(reads)
		ids = nil
		for i, automation := range automations {
			if slices.ContainsFunc(on[automation], func(id string) bool { return triggerMatch(configs[i], id, e.needles(id), stateDelta{}) != "" }) {
				ids = append(ids, actionTargets(automationActions(configs[i]))...)
			}
		}
		sort.Strings(ids)
		ids = slices.Compact(ids)
	}
}

// relatedItems: the search/related reads of an entity's consumer scan.
func (e *tierEngine) relatedItems(id string) [][2]string {
	items := [][2]string{{"entity", id}}
	if device := e.reg[id].di; device != "" {
		items = append(items, [2]string{"device", device})
	}
	return items
}

// needles: what a consumer's trigger names when it reacts to id.
func (e *tierEngine) needles(id string) []string {
	if device := e.reg[id].di; device != "" {
		return []string{id, device}
	}
	return []string{id}
}

// warmConfig reads an automation's config as runConfig does (its stored
// config by attributes.id, else the one Home Assistant loaded), blueprint
// substituted, through the session only; nil when it can't.
func (e *tierEngine) warmConfig(automation string) map[string]any {
	status, body, err := e.s.coreGET("/api/states/" + automation)
	var st struct {
		Attributes struct {
			ID string `json:"id"`
		} `json:"attributes"`
	}
	if err != nil || status != http.StatusOK || json.Unmarshal(body, &st) != nil {
		return nil
	}
	var raw json.RawMessage
	err = &verbFailure{code: "CONFIG_NOT_FOUND"}
	if strings.TrimSpace(st.Attributes.ID) != "" {
		raw, err = readStoredConfig(e.s, "automation", st.Attributes.ID)
	}
	var notFound *verbFailure
	if errors.As(err, &notFound) {
		raw, _, err = loadedConfig(e.s, automation, "automation")
	}
	if err != nil {
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
	if e.s.wsDecode(blueprintRequest(domain, bp), &sub, "the blueprint substitution") != nil {
		return nil
	}
	return sub.Config
}

func blueprintRequest(domain string, bp map[string]any) map[string]any {
	input := bp["input"]
	if input == nil {
		input = map[string]any{}
	}
	return map[string]any{"type": "blueprint/substitute", "domain": domain, "path": bp["path"], "input": input}
}

// actionTargets: the entity ids an action tree names literally, as walk
// reaches its steps.
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
		if n["action"] != nil || n["service"] != nil {
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
