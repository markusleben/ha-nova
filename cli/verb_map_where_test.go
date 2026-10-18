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
	f.states = append(f.states, st("sensor.kitchen_energy", "1.2", map[string]any{"friendly_name": "Kitchen energy", "source": "light.kitchen"}))
	configs := map[string]any{
		"lovelace": map[string]any{"views": []any{
			map[string]any{"title": "Home", "cards": []any{
				map[string]any{"type": "vertical-stack", "cards": []any{
					map[string]any{"type": "conditional", "conditions": []any{}, "card": map[string]any{"type": "entities", "title": "Kitchen",
						"entities": []any{"sensor.temp", map[string]any{"entity": "light.kitchen"}}}},
				}},
				map[string]any{"type": "tile", "entity": "light.kitchen_status"}, // a sibling id, never a match
			}},
			map[string]any{"path": "lights", "cards": []any{map[string]any{"type": "markdown", "content": "{{ states('light.kitchen') }}"}},
				"badges": []any{"light.kitchen"}},
			map[string]any{"title": "Away", "sections": []any{map[string]any{
				"visibility": []any{map[string]any{"condition": "state", "entity": "light.kitchen", "state": "on"}},
				"cards":      []any{map[string]any{"type": "heading", "heading": "Away"}}}}},
		}},
		"dashboard-wall": map[string]any{"views": []any{map[string]any{"cards": []any{map[string]any{"type": "entities", "entities": []any{"light.kitchen"}}}}}},
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
				"dashboard-broken": map[string]any{"component_name": "lovelace", "url_path": "dashboard-broken", "title": "Broken", "config": map[string]any{"mode": "yaml"}},
				"config":           map[string]any{"component_name": "config", "url_path": "config"},
			}
		case "lovelace/config":
			path, ok := payload["url_path"].(string)
			if !ok {
				path = "lovelace"
			}
			switch path {
			case "dashboard-auto":
				return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "config_not_found: No config found."}
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
		{Dashboard: "Overview", URLPath: "lovelace", View: "/lights", Card: "markdown"},
		{Dashboard: "Overview", URLPath: "lovelace", View: "/lights", Card: "badge: light.kitchen"},
		{Dashboard: "Overview", URLPath: "lovelace", View: "Away"}, // only a section's visibility names it
	}
	if r.Dashboards.Total != len(want) || !slices.Equal(r.Dashboards.Rows, want) {
		t.Errorf("dashboards %+v\nwant %+v", r.Dashboards.Rows, want)
	}
	if !slices.Contains(r.NotChecked, "1 dashboard (could not be read)") || slices.Contains(r.NotChecked, "dashboards") {
		t.Errorf("not_checked %v", r.NotChecked)
	}
	// the meter names the light in its source; the group and scene are listed on their own
	if r.Helpers.Total != 1 || r.Helpers.Rows[0].EntityID != "sensor.kitchen_energy" || r.Helpers.Rows[0].Name != "Kitchen energy" {
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
	if exit != 0 || r.Dashboards.Total != 5 || r.Dashboards.Shown != 2 || len(r.Dashboards.Rows) != 2 ||
		!strings.HasPrefix(r.Dashboards.Rows[1].Card, "vertical-stack") {
		t.Errorf("exit %d dashboards %+v", exit, r.Dashboards)
	}
}
