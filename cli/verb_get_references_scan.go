package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"sync"
)

// The holder families get --references reads (device-replace spec §1).

// scanConfigs reads every automation and script config concurrently: the
// stored config by registry unique_id (registry-disabled items included),
// else the loaded config of a YAML or package item.
func (sc *refScanner) scanConfigs(s *relaySession, registry []refRegistryRow, states []refState) {
	type item struct{ entityID, domain, configID string }
	var items []item
	registered := map[string]bool{}
	for _, row := range registry {
		if domain, _ := splitID(row.EntityID); (domain == "automation" || domain == "script") && row.Platform == domain {
			registered[row.EntityID] = true
			items = append(items, item{row.EntityID, domain, string(row.UniqueID)})
		}
	}
	for _, st := range states {
		if domain, _ := splitID(st.EntityID); (domain == "automation" || domain == "script") && !registered[st.EntityID] {
			items = append(items, item{st.EntityID, domain, ""})
		}
	}
	if len(items) > tierMaxAutomations {
		sc.gap("automation", "", fmt.Sprintf("more than %d automations and scripts; none were scanned", tierMaxAutomations))
		return
	}
	var wg sync.WaitGroup
	for _, it := range items {
		wg.Add(1)
		go func(it item) {
			defer wg.Done()
			var raw json.RawMessage
			err := error(&verbFailure{code: "CONFIG_NOT_FOUND"})
			if it.configID != "" {
				raw, err = readStoredConfig(s, it.domain, it.configID)
			}
			managed := ""
			var notFound *verbFailure
			if errors.As(err, &notFound) {
				loaded, ok, loadErr := loadedConfig(s, it.entityID, it.domain)
				switch {
				case loadErr != nil:
					err = loadErr
				case ok:
					raw, err, managed = loaded, nil, "yaml"
				default:
					err = errors.New("no readable config (a disabled YAML item, or not loaded)")
				}
			}
			var doc any
			if err == nil {
				err = json.Unmarshal(raw, &doc)
			}
			if err != nil {
				sc.gap(it.domain, it.entityID, err.Error())
				return
			}
			sc.mu.Lock()
			sc.res.Scanned[it.domain]++
			sc.mu.Unlock()
			sc.scan(it.domain, it.entityID, it.configID, managed, doc)
			sc.scanBlueprint(s, it.domain, it.entityID, it.configID, doc)
		}(it)
	}
	wg.Wait()
}

// lovelaceBoard is one Lovelace panel; Config is nil for an auto-generated
// dashboard, which has no stored config.
type lovelaceBoard struct {
	URLPath, Title, Mode string
	Config               any
}

// readLovelacePanels reads every Lovelace panel's config (storage and YAML mode,
// the default one included) in parallel. Unreadable ones come back as gaps;
// err means the panel list itself failed. The default dashboard is addressed
// by url_path only when the dashboard list carries it (HA 2026.10+ can list a
// saved Overview); else without, as the unlisted default.
func readLovelacePanels(s *relaySession) ([]lovelaceBoard, []refGap, error) {
	var listed []dashboardEntry
	var listErr error
	prefetch([]func(){func() { _, _ = s.ws(map[string]any{"type": "get_panels"}) }, func() { listed, listErr = readDashboards(s) }})
	var panels map[string]struct {
		Component string  `json:"component_name"`
		URLPath   string  `json:"url_path"`
		Title     *string `json:"title"`
		Config    struct {
			Mode string `json:"mode"`
		} `json:"config"`
	}
	if err := s.wsDecode(map[string]any{"type": "get_panels"}, &panels, "the panels"); err != nil {
		return nil, nil, err
	}
	var keys []string
	for key, p := range panels {
		if p.Component == "lovelace" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var boards []lovelaceBoard
	var gaps []refGap
	payloads := make([]map[string]any, len(keys))
	var reads []func()
	for i, key := range keys {
		p := panels[key]
		if p.URLPath == defaultDashboard && listErr != nil {
			gaps = append(gaps, refGap{Family: "dashboard", ID: p.URLPath, Reason: listErr.Error()})
			continue
		}
		payloads[i] = map[string]any{"type": "lovelace/config", "force": false}
		if p.URLPath != defaultDashboard || findDashboard(listed, defaultDashboard) != nil {
			payloads[i]["url_path"] = p.URLPath
		}
		reads = append(reads, func() { _, _ = s.ws(payloads[i]) })
	}
	prefetch(reads)
	for i, key := range keys {
		p := panels[key]
		if payloads[i] == nil { // already a gap
			continue
		}
		b := lovelaceBoard{URLPath: p.URLPath, Mode: p.Config.Mode, Title: p.URLPath}
		if p.Title != nil && *p.Title != "" {
			b.Title = *p.Title
		} else if p.URLPath == defaultDashboard {
			b.Title = "Overview"
		}
		err := s.wsDecode(payloads[i], &b.Config, "the dashboard "+p.URLPath)
		switch {
		case isUpstreamNotFound(err) && p.Config.Mode != "yaml": // auto-generated
			b.Config, err = nil, nil
		case err == nil && asMap(b.Config) == nil:
			err = errors.New("the dashboard config is not a JSON object")
		}
		if err != nil {
			gaps = append(gaps, refGap{Family: "dashboard", ID: p.URLPath, Reason: err.Error()})
			continue
		}
		boards = append(boards, b)
	}
	return boards, gaps, nil
}

// scanDashboards scans every readable dashboard; an auto-generated one has
// nothing to re-point.
func (sc *refScanner) scanDashboards(s *relaySession) {
	boards, gaps, err := readLovelacePanels(s)
	if err != nil {
		sc.gap("dashboard", "", err.Error())
		return
	}
	for _, g := range gaps {
		sc.gap(g.Family, g.ID, g.Reason)
	}
	for _, b := range boards {
		sc.res.Scanned["dashboard"]++
		if b.Config == nil {
			continue
		}
		managed := ""
		if b.Mode == "yaml" {
			managed = "yaml"
		}
		sc.scan("dashboard", b.URLPath, "", managed, b.Config)
	}
}

func (sc *refScanner) scanEnergy(s *relaySession) {
	var doc any
	err := s.wsDecode(map[string]any{"type": "energy/get_prefs"}, &doc, "the energy preferences")
	switch {
	case isUpstreamNotFound(err): // never configured
	case err != nil:
		sc.gap("energy", "", err.Error())
	default:
		sc.res.Scanned["energy"]++
		sc.scan("energy", "energy", "", "", doc)
	}
}

// scanBlueprint expands a blueprint instance like the tier engine does, with
// every needle masked in the instance's inputs: whatever needle the expansion
// still names comes from the blueprint body (a default, a template, a
// hard-coded id) and is a hit with managed "blueprint" — the blueprint file,
// not the instance, has to change, even where an input names the same id.
func (sc *refScanner) scanBlueprint(s *relaySession, domain, entityID, configID string, doc any) {
	cfg, _ := doc.(map[string]any)
	bp, isBlueprint := cfg["use_blueprint"].(map[string]any)
	if !isBlueprint {
		return
	}
	input := sc.mask(bp["input"])
	if input == nil {
		input = map[string]any{}
	}
	var sub struct {
		Config map[string]any `json:"substituted_config"`
	}
	err := s.wsDecode(map[string]any{"type": "blueprint/substitute", "domain": domain, "path": bp["path"], "input": input}, &sub, "the blueprint substitution")
	if err == nil && sub.Config == nil {
		err = errors.New("the substitution returned no config")
	}
	if err != nil {
		sc.gap(domain, entityID, fmt.Sprintf("blueprint %v could not be expanded (only its inputs were scanned): %v", bp["path"], err))
		return
	}
	sc.record(domain, entityID, configID, "blueprint", sc.find(sub.Config))
}

// mask copies v with every whole-id needle occurrence in its strings
// replaced, so a substitution no longer carries the instance's own ids.
func (sc *refScanner) mask(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[sc.mask(k).(string)] = sc.mask(x) // an id-keyed input (scene entities) too
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = sc.mask(x)
		}
		return out
	case string:
		for _, n := range matchNeedles(t, sc.needles) {
			re := regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(n) + `($|[^A-Za-z0-9_])`)
			for len(matchNeedles(t, []string{n})) > 0 { // adjacent matches share a boundary
				t = re.ReplaceAllString(t, "${1}ha_nova.masked${2}")
			}
		}
		return t
	}
	return v
}
