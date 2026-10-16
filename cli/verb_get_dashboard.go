package main

import (
	"encoding/json"
)

// get --dashboard <url_path> / --dashboards (dashboard-write spec): one
// dashboard's structure, or the broken-card sweep over every dashboard.
// Read-only, YAML dashboards included.

type dashboardView struct {
	Title string   `json:"title"`
	Path  string   `json:"path,omitempty"`
	Type  string   `json:"type,omitempty"`
	Cards []string `json:"cards"` // "section 1 › card 2: tile light.kitchen"
}

type dashboardReport struct {
	URLPath string `json:"url_path"`
	Title   string `json:"title,omitempty"`
	Mode    string `json:"mode"` // storage | yaml | empty (no stored config yet)
	// ConfigDigest: pass it to plan write --base; it pins the config the draft starts from.
	ConfigDigest string            `json:"config_digest"`
	Views        int               `json:"views"`
	Cards        int               `json:"cards"`
	Layout       []dashboardView   `json:"layout,omitempty"` // --dashboard only
	CustomTypes  map[string]string `json:"custom_types,omitempty"`
	Issues       []dashboardIssue  `json:"issues,omitempty"`
	Config       json.RawMessage   `json:"config,omitempty"`   // --dashboard --config
	Strategy     string            `json:"strategy,omitempty"` // a strategy dashboard: Home Assistant generates its views
	// Error: this dashboard could not be read; the sweep goes on with the others.
	Error string `json:"error,omitempty"`
}

type dashboardsResult struct {
	Dashboards []dashboardReport  `json:"dashboards"`
	Resources  []lovelaceResource `json:"resources,omitempty"`
}

func runGetDashboards(s *relaySession, env *verbEnvelope, urlPath string, withConfig bool) error {
	entries, err := readDashboards(s)
	if err != nil {
		return err
	}
	if urlPath != "" && findDashboard(entries, urlPath) == nil && urlPath != defaultDashboard {
		return dashboardNotFound(urlPath, entries)
	}
	if findDashboard(entries, defaultDashboard) == nil { // the unlisted default dashboard
		entries = append([]dashboardEntry{{URLPath: defaultDashboard, Mode: "storage"}}, entries...)
	}
	states, err := loadStates(s)
	if err != nil {
		return err
	}
	e := newTierEngine(s)
	rows, err := e.fullRegistry()
	if err != nil {
		return err
	}
	resources, err := readResources(s)
	if err != nil {
		return err
	}
	present := presenceFrom(states, rows)
	result := dashboardsResult{Dashboards: []dashboardReport{}}
	for _, entry := range entries {
		if urlPath != "" && entry.URLPath != urlPath {
			continue
		}
		raw, err := readDashboardConfig(s, entry.URLPath, entry.raw == nil, false)
		if err != nil && urlPath != "" {
			return err
		}
		if err != nil { // one unreadable dashboard never hides the others
			result.Dashboards = append(result.Dashboards, dashboardReport{URLPath: entry.URLPath, Title: entry.Title, Mode: entry.Mode, Error: err.Error()})
			continue
		}
		report := dashboardReport{URLPath: entry.URLPath, Title: entry.Title, Mode: entry.Mode, ConfigDigest: digestOf(raw)}
		cfg, _ := configObjectFromBytes(raw)
		switch {
		case cfg == nil && entry.Mode == "yaml": // a YAML dashboard without its file is broken, not empty
			report.Issues = []dashboardIssue{{Kind: "config_missing", Where: "Dashboard", Detail: "its YAML file could not be loaded (Home Assistant: config_not_found)"}}
		case cfg == nil:
			report.Mode = "empty"
		default:
			report.Strategy, _ = asMap(cfg["strategy"])["type"].(string)
			report.Issues = dashboardIssues(cfg, present, resources)
			for _, cardType := range customTypes(cfg) {
				if report.CustomTypes == nil {
					report.CustomTypes = map[string]string{}
				}
				report.CustomTypes[cardType] = providedBy(cardType, resources)
			}
		}
		for i, view := range dashboardViews(cfg) {
			report.Views++
			report.Cards += viewCardCount(view)
			if urlPath != "" {
				report.Layout = append(report.Layout, layoutOf(i, view))
			}
		}
		if withConfig {
			report.Config = raw
		}
		result.Dashboards = append(result.Dashboards, report)
	}
	if urlPath != "" {
		result.Resources = resources
	}
	env.Result = result
	return nil
}

func layoutOf(i int, view map[string]any) dashboardView {
	title, _ := view["title"].(string)
	if title == "" {
		title = viewLabel(i, view)
	}
	out := dashboardView{Title: title, Cards: []string{}}
	out.Path, _ = view["path"].(string)
	out.Type, _ = view["type"].(string)
	for _, c := range viewContainers(view) {
		for j, card := range c.items {
			out.Cards = append(out.Cards, itemLabel(c.label, j)+": "+cardSummary(card))
		}
	}
	return out
}
