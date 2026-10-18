package main

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// dashboardHome: smallHome plus Lovelace panels — the default overview
// (nested stack, markdown template, badge, section visibility), a YAML
// dashboard, an auto-generated one, an unreadable one, and a meter whose
// source attribute names the light.
func dashboardHome(panelsFail bool) func(*http.Request, string, map[string]any) any {
	f := smallHome()
	f.states = append(f.states, st("sensor.kitchen_energy", "1.2", map[string]any{"friendly_name": "Kitchen energy", "source": "light.kitchen"}),
		st("sensor.kitchen_daily", "unavailable", nil),                                                                        // a meter whose source Home Assistant hides
		st("zone.home", "1", map[string]any{"persons": []string{"light.kitchen"}}),                                            // presence never appears
		st("person.guest", "home", map[string]any{"device_trackers": []string{"light.kitchen"}}),                              // presence never appears
		st("sensor.kitchen_min", "1", map[string]any{"friendly_name": "Kitchen min", "entity_id": []string{"light.kitchen"}})) // min/max: a helper, not a group
	f.entities = append(f.entities, ent("sensor.kitchen_daily", "", "utility_meter", "", nil), ent("sensor.kitchen_min", "", "min_max", "", nil))
	configs := map[string]any{
		"lovelace": map[string]any{"views": []any{
			map[string]any{"title": "Home", "cards": []any{
				map[string]any{"type": "vertical-stack", "cards": []any{
					map[string]any{"type": "conditional", "conditions": []any{}, "card": map[string]any{"type": "entities", "title": "Kitchen",
						"entities": []any{"sensor.temp", map[string]any{"entity": "light.kitchen"}}}},
				}},
				map[string]any{"type": "tile", "entity": "light.kitchen_status"}, // a sibling id, never a match
				map[string]any{"type": "tile", "entity": "person.guest", "visibility": []any{map[string]any{"condition": "state", "entity": "light.kitchen", "state": "on"}}},
			}},
			map[string]any{"path": "lights", "cards": []any{map[string]any{"type": "markdown", "content": "{{ states('light.kitchen') }}"},
				map[string]any{"type": "horizontal-stack", "cards": []any{map[string]any{"type": "tile", "entity": "light.kitchen"},
					map[string]any{"type": "button", "name": "Spot", "tap_action": map[string]any{"action": "toggle", "target": map[string]any{"entity_id": "light.kitchen"}}}}}},
				"badges": []any{"light.kitchen"}},
			map[string]any{"title": "Away", "sections": []any{map[string]any{
				"visibility": []any{map[string]any{"condition": "state", "entity": "light.kitchen", "state": "on"}},
				"cards": []any{map[string]any{"type": "heading", "heading": "Away"},
					map[string]any{"type": "conditional", "conditions": []any{map[string]any{"condition": "state", "entity": "light.kitchen", "state": "on"}},
						"card": map[string]any{"type": "tile", "entity": "light.kitchen"}}}}}},
		}},
		"dashboard-wall":   map[string]any{"views": []any{map[string]any{"cards": []any{map[string]any{"type": "entities", "entities": []any{"light.kitchen"}}}}}},
		"dashboard-energy": map[string]any{"views": []any{map[string]any{"title": "Energy", "cards": []any{map[string]any{"type": "tile", "entity": "sensor.temp"}}}}},
	}
	return func(r *http.Request, endpoint string, payload map[string]any) any {
		switch payload["type"] {
		case "get_panels":
			if panelsFail {
				return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "unknown_error: boom"}
			}
			return map[string]any{
				"lovelace":         map[string]any{"component_name": "lovelace", "url_path": "lovelace", "title": nil, "config": map[string]any{"mode": "storage"}},
				"dashboard-wall":   map[string]any{"component_name": "lovelace", "url_path": "dashboard-wall", "title": "Wall", "config": map[string]any{"mode": "yaml"}},
				"dashboard-energy": map[string]any{"component_name": "lovelace", "url_path": "dashboard-energy", "title": "Energy", "config": map[string]any{"mode": "storage"}},
				"dashboard-auto":   map[string]any{"component_name": "lovelace", "url_path": "dashboard-auto", "title": "Auto", "config": map[string]any{"mode": "storage"}},
				"dashboard-list":   map[string]any{"component_name": "lovelace", "url_path": "dashboard-list", "title": "List", "config": map[string]any{"mode": "storage"}},
				"dashboard-broken": map[string]any{"component_name": "lovelace", "url_path": "dashboard-broken", "title": "Broken", "config": map[string]any{"mode": "yaml"}},
				"config":           map[string]any{"component_name": "config", "url_path": "config"},
			}
		case "lovelace/dashboards/list":
			return []any{}
		case "lovelace/config":
			path, ok := payload["url_path"].(string)
			if !ok {
				path = "lovelace"
			}
			switch path {
			case "dashboard-auto":
				return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "config_not_found: No config found."}
			case "dashboard-list":
				return []any{} // not a config object
			case "dashboard-broken":
				return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "unknown_error: the YAML file is broken"}
			}
			return configs[path]
		}
		return f.reply(r, endpoint, payload)
	}
}

func runWhereUsed(t *testing.T, panelsFail bool) whereUsedResult {
	t.Helper()
	paths, _ := setupVerbRelayTest(t, dashboardHome(panelsFail))
	exit, env := runVerbForTest(t, func() int { return runMapCommand(paths, []string{"--entity", "light.kitchen"}) })
	if exit != 0 {
		t.Fatalf("exit %d %+v", exit, env.Error)
	}
	return decodeInto[whereUsedResult](t, env.Result)
}

func TestMapWhereUsedFindsDashboardCards(t *testing.T) {
	r := runWhereUsed(t, false)
	want := []dashboardUse{ // dashboards in url_path order
		{Dashboard: "Wall", URLPath: "dashboard-wall", View: "view 1", Card: "entities (1 entity)"},
		{Dashboard: "Overview", URLPath: "lovelace", View: "Home", Card: `vertical-stack (1 card) › conditional › entities "Kitchen"`},
		{Dashboard: "Overview", URLPath: "lovelace", View: "Home", Card: "tile"}, // a person's card: no presence id
		{Dashboard: "Overview", URLPath: "lovelace", View: "/lights", Card: "markdown"},
		{Dashboard: "Overview", URLPath: "lovelace", View: "/lights", Card: "horizontal-stack (2 cards) › tile light.kitchen"},
		{Dashboard: "Overview", URLPath: "lovelace", View: "/lights", Card: `horizontal-stack (2 cards) › button "Spot"`},
		{Dashboard: "Overview", URLPath: "lovelace", View: "/lights", Card: "badge: light.kitchen"},
		{Dashboard: "Overview", URLPath: "lovelace", View: "Away"},                      // a section's visibility
		{Dashboard: "Overview", URLPath: "lovelace", View: "Away", Card: "conditional"}, // its condition
		{Dashboard: "Overview", URLPath: "lovelace", View: "Away", Card: "conditional › tile light.kitchen"},
	}
	if r.Dashboards.Total != len(want) || !slices.Equal(r.Dashboards.Rows, want) {
		t.Errorf("dashboards %+v\nwant %+v", r.Dashboards.Rows, want)
	}
	if !slices.Contains(r.NotChecked, "2 dashboards (could not be read)") || !slices.Contains(r.NotChecked, "1 unavailable helper (source hidden)") || slices.Contains(r.NotChecked, "dashboards") {
		t.Errorf("not_checked %v", r.NotChecked)
	}
	// the meter names the light in its source; the group and scene are listed on their own
	if r.Helpers.Total != 2 || r.Helpers.Rows[0].EntityID != "sensor.kitchen_energy" || r.Helpers.Rows[0].Name != "Kitchen energy" ||
		r.Helpers.Rows[1].EntityID != "sensor.kitchen_min" || r.Groups.Total != 1 || r.Groups.Rows[0].EntityID != "light.group_all" {
		t.Errorf("helpers %+v", r.Helpers)
	}
}

func TestMapWhereUsedKeepsDashboardsUncheckedWhenPanelsFail(t *testing.T) {
	r := runWhereUsed(t, true)
	if r.Dashboards.Total != 0 || !slices.Contains(r.NotChecked, "dashboards") {
		t.Errorf("dashboards %+v not_checked %v", r.Dashboards, r.NotChecked)
	}
	if r.Automations.Total != 2 {
		t.Errorf("a failed panel list must not lose the rest: %+v", r.Automations)
	}
}

func TestMapWhereUsedLimitCapsDashboardRows(t *testing.T) {
	paths, _ := setupVerbRelayTest(t, dashboardHome(false))
	exit, env := runVerbForTest(t, func() int { return runMapCommand(paths, []string{"--entity", "light.kitchen", "--limit", "2"}) })
	r := decodeInto[whereUsedResult](t, env.Result)
	if exit != 0 || r.Dashboards.Total != 10 || r.Dashboards.Shown != 2 || len(r.Dashboards.Rows) != 2 ||
		!strings.HasPrefix(r.Dashboards.Rows[1].Card, "vertical-stack") {
		t.Errorf("exit %d dashboards %+v", exit, r.Dashboards)
	}
}

// HA 2026.10+ can list a saved Overview: it is read by its url_path then, and
// an unreadable dashboard list makes it a gap, never an empty Overview.
func TestMapWhereUsedReadsAListedDefaultByItsPath(t *testing.T) {
	for _, listFails := range []bool{false, true} {
		home := dashboardHome(false)
		paths, _ := setupVerbRelayTest(t, func(r *http.Request, endpoint string, payload map[string]any) any {
			switch payload["type"] {
			case "lovelace/dashboards/list":
				if listFails {
					return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "unknown_error: boom"}
				}
				return []any{map[string]any{"id": "lovelace", "url_path": "lovelace", "title": "Overview", "mode": "storage"}}
			case "lovelace/config":
				if _, addressed := payload["url_path"]; !addressed {
					return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "config_not_found: No config found."}
				}
			}
			return home(r, endpoint, payload)
		})
		exit, env := runVerbForTest(t, func() int { return runMapCommand(paths, []string{"--entity", "light.kitchen"}) })
		r := decodeInto[whereUsedResult](t, env.Result)
		overview := 0
		for _, row := range r.Dashboards.Rows {
			if row.URLPath == "lovelace" {
				overview++
			}
		}
		switch {
		case exit != 0:
			t.Fatalf("exit %d %+v", exit, env.Error)
		case !listFails && (overview != 9 || !slices.Contains(r.NotChecked, "2 dashboards (could not be read)")):
			t.Errorf("listed default: overview rows %d not_checked %v", overview, r.NotChecked)
		case listFails && (overview != 0 || !slices.Contains(r.NotChecked, "3 dashboards (could not be read)")):
			t.Errorf("unreadable list: overview rows %d not_checked %v", overview, r.NotChecked)
		}
	}
}
