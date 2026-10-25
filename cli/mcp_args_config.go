package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// argv for the config routes beyond automations and scripts: dashboards
// (ha_plan write, delete, revert; ha_get dashboard and dashboards), rooms,
// and templates. Same rule as mcp_args.go: every value goes in as
// --flag=value or after a check, so no argument becomes a CLI flag.

// dashboardOnlyArgs: ha_plan arguments that only domain dashboard takes.
var dashboardOnlyArgs = []string{"base", "title", "icon", "require_admin", "hide_sidebar"}

// checkPlanDomain refuses a domain and its arguments where the CLI's plan
// kind has no such form (plan_config.go usage strings).
func checkPlanDomain(kind string, args map[string]json.RawMessage) error {
	domain := text(args, "domain")
	_, hasID := args["id"]
	_, hasEntity := args["entity_id"]
	if domain != "dashboard" {
		for _, key := range dashboardOnlyArgs {
			if _, present := args[key]; present {
				return fmt.Errorf("argument %q is for domain dashboard", key)
			}
		}
	}
	switch {
	case domain == "device_area" && kind != "revert":
		return errors.New("domain device_area (a rooms record) is for kind revert only")
	case kind != "delete":
	case domain == "dashboard" && (!hasID || hasEntity):
		return errors.New("a dashboard delete takes id (its url_path), not entity_id")
	case domain != "" && domain != "dashboard", domain == "" && hasID:
		return errors.New("kind delete takes entity_id (automation.x or script.x), or domain dashboard with id")
	}
	return nil
}

// planKindArgv: kinds rooms and template, and a dashboard delete; nil when
// the kind is another one.
func planKindArgv(kind string, args map[string]json.RawMessage) ([]string, error) {
	switch {
	case kind == "rooms":
		return []string{"rooms"}, nil
	case kind == "template":
		name := text(args, "name")
		if !slices.Contains(automationTemplates, name) {
			return nil, fmt.Errorf("kind template needs name, one of %v", automationTemplates)
		}
		return []string{"template", name}, nil
	case kind == "delete" && text(args, "domain") == "dashboard":
		return []string{"delete", "--domain=dashboard", "--id=" + text(args, "id")}, nil
	}
	return nil, nil
}

// getArgv: exactly one of entity_id, dashboard, or dashboards, as the CLI's
// get takes them.
func getArgv(args map[string]json.RawMessage, _ *mcpTempFiles) ([]string, error) {
	_, entity := args["entity_id"]
	dashboard, dashboards := text(args, "dashboard") != "", boolArg(args, "dashboards")
	modes := 0
	for _, on := range []bool{entity, dashboard, dashboards} {
		if on {
			modes++
		}
	}
	switch {
	case modes != 1:
		return nil, errors.New("pass exactly one of entity_id, dashboard, or dashboards")
	case entity:
		return flagsThen(args, []string{"config", "related", "references"}, nil, "entity_id")
	case boolArg(args, "related") || boolArg(args, "references"):
		return nil, errors.New("related and references go with entity_id only")
	case dashboards && boolArg(args, "config"):
		return nil, errors.New("config goes with entity_id or dashboard, not dashboards")
	}
	return flagsThen(args, []string{"config", "dashboards"}, []string{"dashboard"}, "")
}

func boolArg(args map[string]json.RawMessage, key string) bool {
	var on bool
	return json.Unmarshal(args[key], &on) == nil && on
}

// largeDashboardStep: ha_get returns a dashboard config whole, but a chat
// app may cut a result over ha_read's bound; the model then reads single
// views before it plans a write from that config.
func largeDashboardStep(envelope map[string]any) string {
	if envelope["verb"] != "get" {
		return ""
	}
	result, _ := envelope["result"].(map[string]any)
	boards, _ := result["dashboards"].([]any)
	for _, raw := range boards {
		board, _ := raw.(map[string]any)
		config, present := board["config"]
		if size, _ := json.Marshal(config); !present || len(size) <= mcpReadMaxResult {
			continue
		}
		payload := fmt.Sprintf("{type: lovelace/config, url_path: %v}", board["url_path"])
		if board["url_path"] == defaultDashboard {
			payload += " (no url_path where that is not found)"
		}
		return "If your app cut this result short, read single views with ha_read (endpoint ws, payload " + payload + ", jq .data.views[N]) before you plan a write; never plan from a partial config."
	}
	return ""
}
