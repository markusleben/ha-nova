package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
)

// get --dead-references (docs/work/2026-11-24-dead-reference-sweep-spec.md):
// one read-only sweep of every automation and script for literal references
// to entities, devices, areas, and actions Home Assistant no longer has. The
// loaded configs come in ws batches (relay_batch.go); disabled or unloaded
// items are read from their stored config.

type deadRefResult struct {
	Scanned    map[string]int `json:"scanned"`
	Findings   []deadRef      `json:"findings"`
	NotScanned []refGap       `json:"not_scanned,omitempty"`
	NotCovered []string       `json:"not_covered"`
}

// deadRef: one holder naming one reference that does not resolve to a
// working item. Status is missing, disabled, not_loaded, or unavailable.
type deadRef struct {
	Holder   string   `json:"holder"`
	Name     string   `json:"name,omitempty"`
	ConfigID string   `json:"config_id,omitempty"`
	Managed  string   `json:"managed,omitempty"` // "blueprint": the reference comes from the blueprint the stored config uses
	Kind     string   `json:"kind"`              // entity, device, area, or action
	Ref      string   `json:"ref"`
	EntityID string   `json:"entity_id,omitempty"` // a registry id's entity, when the registry has it
	Status   string   `json:"status"`
	Paths    []string `json:"paths"`
	Template bool     `json:"template,omitempty"`
}

var deadRefsNotCovered = []string{
	"references built at run time: templated entity_id, device_id, area_id, or action values, and template ids outside states(), is_state(), state_attr(), is_state_attr(), has_value(), expand(), and states.<domain>.<id>, or built from parts",
	"ids inside variables, event payloads, and action data below its top level (only their templates are read)",
	"floor and label targets",
	"dashboards (ha-nova get --dashboards checks them), scenes, helpers, and YAML-only integrations",
}

// deadRefHome is what every reference is checked against, read once.
type deadRefHome struct {
	states    map[string]string // entity_id → state
	names     map[string]string // entity_id → friendly_name
	registry  map[string]fullRegistryRow
	byEntryID map[string]string // registry id → entity_id
	devices   map[string]string // device id → disabled_by
	legacy    map[string]bool   // pre-2026.9 merged device ids Home Assistant still resolves
	areas     map[string]bool
	actions   map[string]bool
}

type deadRefHolder struct {
	entityID, domain, configID string
	loaded                     bool
}

func runGetDeadReferences(s *relaySession, env *verbEnvelope) error {
	home, rows, err := readDeadRefHome(s)
	if err != nil {
		return err
	}
	res := &deadRefResult{Scanned: map[string]int{"automation": 0, "script": 0}, Findings: []deadRef{}, NotCovered: deadRefsNotCovered}
	holders := deadRefHolders(home, rows)
	var loaded []map[string]any
	for _, h := range holders {
		if h.loaded {
			loaded = append(loaded, loadedConfigRequest(h.entityID, h.domain))
		}
	}
	s.readBatched(loaded, 0)
	var mu sync.Mutex
	var wg sync.WaitGroup
	reads := make(chan struct{}, 8) // stored configs and blueprints are read one request each
	for _, h := range holders {
		wg.Add(1)
		go func(h deadRefHolder) {
			defer wg.Done()
			reads <- struct{}{}
			cfg, managed, err := readDeadRefConfig(s, h)
			<-reads
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				res.NotScanned = append(res.NotScanned, refGap{Family: h.domain, ID: h.entityID, Reason: err.Error()})
				return
			}
			res.Scanned[h.domain]++
			name, _ := cfg["alias"].(string)
			if name == "" {
				name = home.names[h.entityID]
			}
			found := map[string]*deadRef{}
			walkDeadRefs("", cfg, false, func(kind, ref, path string, template bool) {
				status, entityID := home.status(kind, ref)
				if status == "" {
					return
				}
				key := kind + " " + ref
				f := found[key]
				if f == nil {
					f = &deadRef{Holder: h.entityID, Name: name, ConfigID: h.configID, Managed: managed, Kind: kind, Ref: ref, EntityID: entityID, Status: status}
					found[key] = f
				}
				f.Paths = append(f.Paths, path)
				f.Template = f.Template || template
			})
			for _, f := range found {
				sort.Strings(f.Paths)
				res.Findings = append(res.Findings, *f)
			}
		}(h)
	}
	wg.Wait()
	sort.Slice(res.Findings, func(i, j int) bool {
		a, b := res.Findings[i], res.Findings[j]
		if a.Holder != b.Holder {
			return a.Holder < b.Holder
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Ref < b.Ref
	})
	sort.Slice(res.NotScanned, func(i, j int) bool { return res.NotScanned[i].ID < res.NotScanned[j].ID })
	env.Result = res
	return nil
}

// readDeadRefHome reads every list a verdict needs, together; any failed
// read fails the sweep, because a reference checked against a list that
// was not read would look missing.
func readDeadRefHome(s *relaySession) (*deadRefHome, []fullRegistryRow, error) {
	warm([]func(){
		func() { _, _, _ = s.warmCoreGET("/api/states") },
		func() { _, _, _ = s.warmCoreGET("/api/services") },
		func() { s.warmWS(map[string]any{"type": "config/entity_registry/list"}) },
		func() { s.warmWS(map[string]any{"type": "config/device_registry/list"}) },
		func() { s.warmWS(map[string]any{"type": "config/device_registry/list_composite_splits"}) },
		func() { s.warmWS(map[string]any{"type": "config/area_registry/list"}) },
	})
	home := &deadRefHome{states: map[string]string{}, names: map[string]string{}, registry: map[string]fullRegistryRow{}, byEntryID: map[string]string{},
		devices: map[string]string{}, legacy: map[string]bool{}, areas: map[string]bool{}, actions: map[string]bool{}}
	states, err := loadStates(s)
	if err == nil && states == nil {
		err = errors.New("Home Assistant sent no state list")
	}
	if err != nil {
		return nil, nil, err
	}
	for _, st := range states {
		home.states[st.EntityID], home.names[st.EntityID] = st.State, st.Attributes.FriendlyName
	}
	var rows []fullRegistryRow
	if err := deadRefList(s.wsDecode(map[string]any{"type": "config/entity_registry/list"}, &rows, "the entity registry"), rows == nil, "the entity registry"); err != nil {
		return nil, nil, err
	}
	for _, row := range rows {
		home.registry[row.EntityID], home.byEntryID[row.ID] = row, row.EntityID
	}
	var devices []struct {
		ID         string `json:"id"`
		DisabledBy string `json:"disabled_by"`
	}
	if err := deadRefList(s.wsDecode(map[string]any{"type": "config/device_registry/list"}, &devices, "the device registry"), devices == nil, "the device registry"); err != nil {
		return nil, nil, err
	}
	for _, d := range devices {
		home.devices[d.ID] = d.DisabledBy
	}
	var splits map[string]json.RawMessage
	switch err := s.wsDecode(map[string]any{"type": "config/device_registry/list_composite_splits"}, &splits, "the device splits"); {
	case isUpstreamUnknownCommand(err): // a core before 2026.9 has no merged ids
	case err != nil || splits == nil:
		return nil, nil, deadRefList(err, true, "the device splits")
	}
	for id := range splits {
		home.legacy[id] = true
	}
	var areas []struct {
		AreaID string `json:"area_id"`
	}
	if err := deadRefList(s.wsDecode(map[string]any{"type": "config/area_registry/list"}, &areas, "the area registry"), areas == nil, "the area registry"); err != nil {
		return nil, nil, err
	}
	for _, a := range areas {
		home.areas[a.AreaID] = true
	}
	status, body, err := s.coreGET("/api/services")
	if err == nil && status != http.StatusOK {
		err = fmt.Errorf("Home Assistant answered HTTP %d for /api/services", status)
	}
	var domains []struct {
		Domain   string                     `json:"domain"`
		Services map[string]json.RawMessage `json:"services"`
	}
	if err == nil {
		err = deadRefList(json.Unmarshal(body, &domains), domains == nil, "the action list")
	}
	if err != nil {
		return nil, nil, err
	}
	for _, d := range domains {
		for service := range d.Services {
			home.actions[d.Domain+"."+service] = true
		}
	}
	return home, rows, nil
}

// deadRefList: a list that came back as null is unread, never empty.
func deadRefList(err error, null bool, what string) error {
	if err == nil && null {
		err = fmt.Errorf("Home Assistant sent no list for %s", what)
	}
	return err
}

// deadRefHolders: every automation and script with a state (loaded) or a
// registry row (disabled or not loaded), sorted.
func deadRefHolders(home *deadRefHome, rows []fullRegistryRow) []deadRefHolder {
	seen := map[string]bool{}
	var holders []deadRefHolder
	add := func(entityID, configID string) {
		domain, _ := splitID(entityID)
		if (domain != "automation" && domain != "script") || seen[entityID] {
			return
		}
		seen[entityID] = true
		_, loaded := home.states[entityID]
		holders = append(holders, deadRefHolder{entityID: entityID, domain: domain, configID: configID, loaded: loaded})
	}
	for _, row := range rows {
		if domain, _ := splitID(row.EntityID); row.Platform == domain {
			add(row.EntityID, string(row.UniqueID))
		}
	}
	for id := range home.states {
		add(id, "")
	}
	sort.Slice(holders, func(i, j int) bool { return holders[i].entityID < holders[j].entityID })
	return holders
}

// readDeadRefConfig: the config Home Assistant runs (loaded, blueprints
// already filled in); else the stored one, a blueprint instance expanded.
func readDeadRefConfig(s *relaySession, h deadRefHolder) (map[string]any, string, error) {
	var raw json.RawMessage
	if h.loaded {
		loaded, ok, err := loadedConfig(s, h.entityID, h.domain)
		if err != nil {
			return nil, "", err
		}
		if ok {
			raw = loaded
		}
	}
	if raw == nil {
		if h.configID == "" {
			return nil, "", errors.New("no readable config (a disabled YAML item, or not loaded)")
		}
		stored, err := readStoredConfig(s, h.domain, h.configID)
		var missing *verbFailure
		if errors.As(err, &missing) {
			return nil, "", errors.New("no readable config (a disabled YAML item, or not loaded)")
		}
		if err != nil {
			return nil, "", err
		}
		raw = stored
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil || cfg == nil {
		return nil, "", errors.New("its config is not a JSON object")
	}
	bp, isBlueprint := cfg["use_blueprint"].(map[string]any)
	if !isBlueprint {
		return cfg, "", deadRefShape(h.domain, cfg)
	}
	var sub struct {
		Config map[string]any `json:"substituted_config"`
	}
	err := s.wsDecode(blueprintRequest(h.domain, bp), &sub, "the blueprint substitution")
	if err == nil && sub.Config == nil {
		err = errors.New("the substitution returned no config")
	}
	if err != nil {
		return nil, "", fmt.Errorf("blueprint %v could not be filled in: %v", bp["path"], err)
	}
	filled := withInstanceKeys(h.domain, sub.Config, cfg)
	return filled, "blueprint", deadRefShape(h.domain, filled)
}

// deadRefShape: a config without what Home Assistant runs (an automation's
// triggers and actions, a script's sequence) was not read whole.
func deadRefShape(domain string, cfg map[string]any) error {
	has := func(keys ...string) bool {
		for _, k := range keys {
			if cfg[k] != nil {
				return true
			}
		}
		return false
	}
	if domain == "script" && has("sequence") || domain == "automation" && has("triggers", "trigger") && has("actions", "action") {
		return nil
	}
	return errors.New("its config came back without its triggers, actions, or sequence")
}

// status: "" when the reference works, else why not; for a registry id,
// also the entity it names.
func (h *deadRefHome) status(kind, ref string) (string, string) {
	switch kind {
	case "entity":
		entityID := ref
		if deadRefRegistryPattern.MatchString(ref) {
			if entityID = h.byEntryID[ref]; entityID == "" {
				return "missing", ""
			}
		}
		shown := ""
		if entityID != ref {
			shown = entityID
		}
		state, hasState := h.states[entityID]
		row, registered := h.registry[entityID]
		switch {
		case hasState && state == "unavailable":
			return "unavailable", shown
		case hasState:
			return "", ""
		case registered && row.DisabledBy != "":
			return "disabled", shown
		case registered:
			return "not_loaded", shown
		}
		return "missing", shown
	case "device":
		disabledBy, known := h.devices[ref]
		switch {
		case known && disabledBy != "":
			return "disabled", ""
		case known || h.legacy[ref]:
			return "", ""
		}
		return "missing", ""
	case "area":
		if h.areas[ref] {
			return "", ""
		}
		return "missing", ""
	case "action":
		if h.actions[ref] {
			return "", ""
		}
		return "missing", ""
	}
	return "", ""
}
