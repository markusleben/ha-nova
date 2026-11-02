package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Steps at the decision point: a chat model sees only the tool list and the
// results (claude.ai passes it no server instructions, and it often skips
// ha_skill), so the one rule a skill holds for the moment the model decides
// rides in that result's next_step, one short sentence each. The skill
// stays the rule's source.

// withPlanSteps adds the steps of a pending ha_plan result to its
// next_step, the raw plan's first and a delete plan's last, then renders
// the result's text again from its envelope (toolResult).
func withPlanSteps(result map[string]any, tool string, args map[string]json.RawMessage) map[string]any {
	envelope, _ := result["structuredContent"].(map[string]any)
	if tool != "ha_plan" || envelope["status"] != "needs_confirmation" {
		return result
	}
	first, last := "", deleteUsesStep(envelope)
	if text(args, "kind") == "raw" {
		first = rawPlanSteps(args["requests"])
	}
	if first == "" && last == "" {
		return result
	}
	own, _ := envelope["next_step"].(string)
	envelope["next_step"] = strings.TrimSpace(strings.TrimSpace(first+" "+own) + " " + last)
	result["content"].([]map[string]any)[0]["text"] = string(ruleFirst(envelope))
	return result
}

// rawPlanSteps: a raw plan that deletes, renames an entity id, or disables
// something with fewer backups saves before it than such items so far (one
// copy per item) goes back for the copy and for what uses it
// (skills/ha-nova/config-snapshots.md → Capture); one with a scene or
// helper takes the others of this request along, with one ask.
func rawPlanSteps(requests json.RawMessage) string {
	var items []rawRequest
	_ = json.Unmarshal(requests, &items) // the verb planned them: they parse
	var steps []string
	saves, needed, grouped := 0, 0, false
	for _, item := range items {
		payload, _ := decodeObject(item.Payload)
		owner, sceneOrHelper := rawStepKind(item.Endpoint, payload)
		if owner != "" {
			needed++
		}
		if owner != "" && needed > saves && steps == nil {
			steps = append(steps, fmt.Sprintf("Before you show this plan: read what uses it (ha_get with references) and its current config, then plan again with a backups save first and tell the user what uses it; load ha_skill name %s for the payloads.", owner))
		}
		if item.Endpoint == "backups" && payload["action"] == "save" {
			saves++
		}
		grouped = grouped || sceneOrHelper
	}
	if grouped {
		steps = append(steps, "If this request changes several scenes or helpers: plan them as one raw plan with all their requests (up to 20) and ask once.")
	}
	return strings.Join(steps, " ")
}

// rawStepKind: the skill whose payloads give the item its config copy
// first ("" when it takes none), and whether it changes a scene or helper.
func rawStepKind(endpoint string, payload map[string]any) (owner string, sceneOrHelper bool) {
	wsType, _ := payload["type"].(string)
	collection, op, _ := rawCollection(wsType)
	helper := endpoint == "ws" && containsText(rawHelperCollections, collection)
	target, _ := payload["path"].(string)
	target, _, _ = strings.Cut(target, "?")
	scene := endpoint == "core" && rawSceneConfigPath.MatchString(target)
	_, renames := payload["new_entity_id"]
	disables := payload["disabled_by"] != nil // null enables
	switch {
	case helper && op == "delete":
		owner = "helper"
	case scene && payload["method"] == http.MethodDelete:
		owner = "scene"
	case endpoint == "ws" && (wsType == "config/device_registry/remove" || wsType == "config/entity_registry/update" && (renames || disables) ||
		wsType == "config/device_registry/update" && disables):
		owner = "organize"
	case endpoint == "files" && payload["action"] == "delete_file":
		owner = "yaml-config"
	}
	return owner, helper || scene
}

// deleteUsesStep: an automation or script delete plan names what uses it,
// above `say`: its consumers, which break; none, when the scan was complete
// (consumers: []); or that HA NOVA couldn't check (no consumers; plan_store.go).
func deleteUsesStep(envelope map[string]any) string {
	config, _ := envelope["config"].(map[string]any)
	if config["op"] != "delete" || config["domain"] != "automation" && config["domain"] != "script" {
		return ""
	}
	switch consumers, scanned := envelope["consumers"].([]any); {
	case len(consumers) > 0:
		return "Above `say`, tell the user which automations, scripts, or scenes use it (consumers, by name): they break once it is gone."
	case scanned:
		return "Above `say`, tell the user HA NOVA found no automation, script, or scene that uses it."
	}
	return "Above `say`, tell the user HA NOVA couldn't check what uses it."
}
