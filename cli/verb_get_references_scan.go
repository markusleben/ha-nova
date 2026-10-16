package main

import (
	"encoding/json"
	"errors"
	"fmt"
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
			items = append(items, item{row.EntityID, domain, row.UniqueID})
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
			own := sc.scan(it.domain, it.entityID, it.configID, managed, doc)
			sc.scanBlueprint(s, it.domain, it.entityID, it.configID, doc, own)
		}(it)
	}
	wg.Wait()
}

// scanDashboards reads every Lovelace panel's config; an auto-generated
// dashboard has none and nothing to re-point.
func (sc *refScanner) scanDashboards(s *relaySession) {
	var panels map[string]struct {
		Component string `json:"component_name"`
		URLPath   string `json:"url_path"`
		Config    struct {
			Mode string `json:"mode"`
		} `json:"config"`
	}
	if err := s.wsDecode(map[string]any{"type": "get_panels"}, &panels, "the panels"); err != nil {
		sc.gap("dashboard", "", err.Error())
		return
	}
	var keys []string
	for key, p := range panels {
		if p.Component == "lovelace" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		p := panels[key]
		payload := map[string]any{"type": "lovelace/config", "force": false}
		if p.URLPath != "lovelace" { // the default dashboard is addressed without url_path
			payload["url_path"] = p.URLPath
		}
		var doc any
		err := s.wsDecode(payload, &doc, "the dashboard "+p.URLPath)
		switch {
		case isUpstreamNotFound(err) && p.Config.Mode != "yaml": // auto-generated
			sc.res.Scanned["dashboard"]++
			continue
		case err != nil:
			sc.gap("dashboard", p.URLPath, err.Error())
			continue
		}
		sc.res.Scanned["dashboard"]++
		managed := ""
		if p.Config.Mode == "yaml" {
			managed = "yaml"
		}
		sc.scan("dashboard", p.URLPath, "", managed, doc)
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

// scanBlueprint expands a blueprint instance like the tier engine does: a
// needle the instance's inputs do not name but the blueprint body (a
// default, a template) does is a hit with managed "blueprint" — the
// blueprint file, not the instance, has to change.
func (sc *refScanner) scanBlueprint(s *relaySession, domain, entityID, configID string, doc any, own map[string]*refFound) {
	cfg, _ := doc.(map[string]any)
	bp, isBlueprint := cfg["use_blueprint"].(map[string]any)
	if !isBlueprint {
		return
	}
	input := bp["input"]
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
	sc.record(domain, entityID, configID, "blueprint", sc.find(sub.Config), own)
}
