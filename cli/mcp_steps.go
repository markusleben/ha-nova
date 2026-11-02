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

// copyFirstRule leads the answer_rule of a plan that goes back for its copy
// (rawPlanSteps): its `say` is not the one to show, as with batchRule.
const copyFirstRule = "Don't show this `say`: plan again with the copy first (next_step). "

// withPlanSteps leads the next_step of a pending ha_plan result with its
// steps, a raw plan's or what uses an automation or script it deletes (ahead
// of a batch hint, so they hold for the batch too), then renders the
// result's text again from its envelope (toolResult).
func withPlanSteps(result map[string]any, tool string, args map[string]json.RawMessage) map[string]any {
	envelope, _ := result["structuredContent"].(map[string]any)
	if tool != "ha_plan" || envelope["status"] != "needs_confirmation" {
		return result
	}
	lead, copyFirst := deleteUsesStep(envelope), false
	if text(args, "kind") == "raw" {
		lead, copyFirst = rawPlanSteps(args["requests"])
	}
	if lead == "" {
		return result
	}
	own, _ := envelope["next_step"].(string)
	envelope["next_step"] = strings.TrimSpace(lead + " " + own)
	if rule, _ := envelope["answer_rule"].(string); copyFirst {
		envelope["answer_rule"] = copyFirstRule + rule
	}
	result["content"].([]map[string]any)[0]["text"] = string(ruleFirst(envelope))
	return result
}

// rawPlanSteps: a raw plan that deletes, renames an entity id, or disables
// something with fewer backups saves before it than such items so far (one
// copy per item) goes back for the copy and for what uses it
// (skills/ha-nova/config-snapshots.md → Capture), and copyFirst says so; one
// with a scene or helper takes the others of its kind along, with one ask
// (skills/ha-nova/batch-safety.md: one family per manifest; 20 requests
// hold 10 deletes with their copies).
func rawPlanSteps(requests json.RawMessage) (string, bool) {
	var items []rawRequest
	_ = json.Unmarshal(requests, &items) // the verb planned them: they parse
	var steps []string
	saves, needed, copyFirst, grouped := 0, 0, false, false
	for _, item := range items {
		payload, _ := decodeObject(item.Payload)
		owner, sceneOrHelper := rawStepKind(item.Endpoint, payload)
		if owner != "" {
			needed++
		}
		if owner != "" && needed > saves && !copyFirst {
			copyFirst = true
			step := fmt.Sprintf("Before you show this plan: read what uses it (ha_get with references) and its current config, then plan again with a backups save first and tell the user what uses it; load ha_skill name %s for the payloads.", owner)
			if owner == "yaml-config" {
				step = "Before you show this plan: read the file (ha_read endpoint files, payload action read_file), then plan again with a backups save of it first; load ha_skill name yaml-config for the payloads."
			}
			steps = append(steps, step)
		}
		if item.Endpoint == "backups" && payload["action"] == "save" {
			saves++
		}
		grouped = grouped || sceneOrHelper
	}
	if grouped {
		steps = append(steps, "If this request changes several scenes, or several helpers: plan each kind as one raw plan, never scenes and helpers together (up to 10 changes, or up to 10 deletes after their copies), and ask once per plan.")
	}
	return strings.Join(steps, " "), copyFirst
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
	case endpoint == "files" && payload["action"] == "delete_file" && payload["backup"] != false: // backup false: a new file's rollback (rawYAMLCheck)
		owner = "yaml-config"
	}
	return owner, helper || scene
}

// deleteUsesStep: an automation or script delete plan names what uses it in
// its preview: its consumers, which break; none, when the scan was complete
// (consumers: []); or that HA NOVA couldn't check (no consumers; plan_store.go).
func deleteUsesStep(envelope map[string]any) string {
	config, _ := envelope["config"].(map[string]any)
	if config["op"] != "delete" || config["domain"] != "automation" && config["domain"] != "script" {
		return ""
	}
	switch consumers, scanned := envelope["consumers"].([]any); {
	case len(consumers) > 0:
		return "In this plan's preview, tell the user which automations, scripts, or scenes use it (consumers, by name): they break once it is gone."
	case scanned:
		return "In this plan's preview, tell the user HA NOVA found no automation, script, or scene that uses it."
	}
	return "In this plan's preview, tell the user HA NOVA couldn't check what uses it."
}
