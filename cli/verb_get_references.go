package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// get --references (device-replace spec §1): one read-only scan for every
// place that names an entity, its device, or the device's other entities —
// the consumers search/related does not index (dashboards, energy prefs,
// templates, device-level ids, notify services, helper source attributes).

type referencesResult struct {
	Device        *refDevice     `json:"device,omitempty"`
	Entities      []refEntity    `json:"entities"`
	NotifyService string         `json:"notify_service,omitempty"`
	Hits          []refHit       `json:"hits"`
	Scanned       map[string]int `json:"scanned"`
	NotScanned    []refGap       `json:"not_scanned,omitempty"`
	NotCovered    []string       `json:"not_covered"`
}

type refDevice struct {
	ID           string `json:"id"`
	Name         string `json:"name,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty"`
	AreaID       string `json:"area_id,omitempty"`
}

// refEntity carries the mapping hints a swap matches old to new entities by.
type refEntity struct {
	EntityID       string `json:"entity_id"`
	RegistryID     string `json:"registry_id,omitempty"`
	Platform       string `json:"platform,omitempty"`
	Name           string `json:"name,omitempty"`
	OriginalName   string `json:"original_name,omitempty"`
	TranslationKey string `json:"translation_key,omitempty"`
	DeviceClass    string `json:"device_class,omitempty"`
	Unit           string `json:"unit,omitempty"`
	State          string `json:"state,omitempty"`
	Disabled       bool   `json:"disabled,omitempty"`
}

// refHit: one holder naming one needle. Family is automation, script,
// dashboard, energy, or entity (a state attribute of another entity:
// scenes, groups, group helpers, persons, min_max, source-based helpers).
type refHit struct {
	Family   string   `json:"family"`
	ID       string   `json:"id"`
	ConfigID string   `json:"config_id,omitempty"`
	Managed  string   `json:"managed,omitempty"` // "yaml": not editable through plan write or the dashboard editor
	Needle   string   `json:"needle"`
	Paths    []string `json:"paths"`
	Template bool     `json:"template,omitempty"`
}

type refGap struct {
	Family string `json:"family"`
	ID     string `json:"id,omitempty"`
	Reason string `json:"reason"`
}

var referencesNotCovered = []string{
	"template helpers and YAML template entities (their templates are not readable through the API)",
	"helpers that show no source attribute (switch_as_x, statistics, filter, trend, history_stats, proximity)",
	"YAML-only integrations (alert, notify groups, packages outside automations/scripts)",
	"Node-RED, AppDaemon, and other App or HACS consumer managers",
	"protocol-level groups and bindings inside ZHA or Zigbee2MQTT",
}

type refRegistryRow struct {
	ID             string `json:"id"`
	EntityID       string `json:"entity_id"`
	UniqueID       string `json:"unique_id"`
	Platform       string `json:"platform"`
	DeviceID       string `json:"device_id"`
	DisabledBy     string `json:"disabled_by"`
	Name           string `json:"name"`
	OriginalName   string `json:"original_name"`
	TranslationKey string `json:"translation_key"`
}

type refState struct {
	EntityID   string         `json:"entity_id"`
	State      string         `json:"state"`
	Attributes map[string]any `json:"attributes"`
}

// scanReferences builds the needles for entityID (registry row or none) and
// scans every readable holder for them.
func scanReferences(s *relaySession, entityID string) (*referencesResult, error) {
	var registry []refRegistryRow
	if err := s.wsDecode(map[string]any{"type": "config/entity_registry/list"}, &registry, "the entity registry"); err != nil {
		return nil, err
	}
	status, body, err := s.coreGET("/api/states")
	if err == nil && status != http.StatusOK {
		err = fmt.Errorf("Home Assistant answered HTTP %d for /api/states", status)
	}
	var states []refState
	if err == nil {
		err = json.Unmarshal(body, &states)
	}
	if err != nil {
		return nil, err
	}
	stateByID := map[string]refState{}
	for _, st := range states {
		stateByID[st.EntityID] = st
	}
	res := &referencesResult{Scanned: map[string]int{}, NotCovered: referencesNotCovered, Hits: []refHit{}}
	needles, own, err := res.collectNeedles(s, entityID, registry, stateByID)
	if err != nil {
		return nil, err
	}
	sc := &refScanner{needles: needles, res: res}
	sc.scanConfigs(s, registry, states)
	sc.scanDashboards(s)
	sc.scanEnergy(s)
	for _, st := range states {
		if own[st.EntityID] {
			continue // an entity's own attributes never re-point it
		}
		res.Scanned["entity"]++
		sc.scan("entity", st.EntityID, "", "", map[string]any{"attributes": st.Attributes})
	}
	sort.SliceStable(res.Hits, func(i, j int) bool {
		a, b := res.Hits[i], res.Hits[j]
		if a.Family != b.Family {
			return a.Family < b.Family
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Needle < b.Needle
	})
	return res, nil
}

// collectNeedles: every entity of the device (or the entity alone), each
// registry id, the device id, and a mobile_app device's notify service.
func (res *referencesResult) collectNeedles(s *relaySession, entityID string, registry []refRegistryRow, states map[string]refState) ([]string, map[string]bool, error) {
	var self *refRegistryRow
	for i := range registry {
		if registry[i].EntityID == entityID {
			self = &registry[i]
		}
	}
	rows := []refRegistryRow{{EntityID: entityID}}
	if self != nil {
		rows = []refRegistryRow{*self}
	}
	if self != nil && self.DeviceID != "" {
		rows = nil
		for _, row := range registry {
			if row.DeviceID == self.DeviceID {
				rows = append(rows, row)
			}
		}
		var devices []struct {
			ID           string  `json:"id"`
			Name         string  `json:"name"`
			NameByUser   *string `json:"name_by_user"`
			Manufacturer string  `json:"manufacturer"`
			Model        string  `json:"model"`
			AreaID       string  `json:"area_id"`
		}
		if err := s.wsDecode(map[string]any{"type": "config/device_registry/list"}, &devices, "the device registry"); err != nil {
			return nil, nil, err
		}
		res.Device = &refDevice{ID: self.DeviceID}
		for _, d := range devices {
			if d.ID == self.DeviceID {
				res.Device = &refDevice{ID: d.ID, Name: d.Name, Manufacturer: d.Manufacturer, Model: d.Model, AreaID: d.AreaID}
				if d.NameByUser != nil && *d.NameByUser != "" {
					res.Device.Name = *d.NameByUser
				}
				res.notifyService(s, d.Name, rows)
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].EntityID < rows[j].EntityID })
	needles, own := []string{}, map[string]bool{}
	for _, row := range rows {
		st := states[row.EntityID]
		e := refEntity{EntityID: row.EntityID, RegistryID: row.ID, Platform: row.Platform, Name: row.Name, OriginalName: row.OriginalName,
			TranslationKey: row.TranslationKey, State: st.State, Disabled: row.DisabledBy != ""}
		e.DeviceClass, _ = st.Attributes["device_class"].(string)
		e.Unit, _ = st.Attributes["unit_of_measurement"].(string)
		res.Entities = append(res.Entities, e)
		own[row.EntityID] = true
		needles = append(needles, row.EntityID)
		if row.ID != "" {
			needles = append(needles, row.ID)
		}
	}
	if res.Device != nil {
		needles = append(needles, res.Device.ID)
	}
	if res.NotifyService != "" {
		needles = append(needles, strings.TrimPrefix(res.NotifyService, "notify."))
	}
	return needles, own, nil
}

// notifyService resolves a mobile_app device's legacy notify service: the
// registration name (the registry `name`) slugged, checked against the
// live services — never guessed.
func (res *referencesResult) notifyService(s *relaySession, deviceName string, rows []refRegistryRow) {
	mobile := false
	for _, row := range rows {
		mobile = mobile || row.Platform == "mobile_app"
	}
	if !mobile {
		return
	}
	candidate := "mobile_app_" + scriptKey(deviceName)
	status, body, err := s.coreGET("/api/services")
	var domains []struct {
		Domain   string                     `json:"domain"`
		Services map[string]json.RawMessage `json:"services"`
	}
	if err == nil && status == http.StatusOK {
		err = json.Unmarshal(body, &domains)
	} else if err == nil {
		err = fmt.Errorf("Home Assistant answered HTTP %d for /api/services", status)
	}
	if err != nil {
		res.NotScanned = append(res.NotScanned, refGap{Family: "notify", Reason: "the notify services could not be read: " + err.Error()})
		return
	}
	for _, d := range domains {
		if _, ok := d.Services[candidate]; ok && d.Domain == "notify" {
			res.NotifyService = "notify." + candidate
			return
		}
	}
	res.NotScanned = append(res.NotScanned, refGap{Family: "notify", Reason: fmt.Sprintf("no notify.%s service matches the device name; find the phone's service with ha-nova:notify", candidate)})
}

type refScanner struct {
	needles []string
	res     *referencesResult
	mu      sync.Mutex
}

func (sc *refScanner) gap(family, id, reason string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.res.NotScanned = append(sc.res.NotScanned, refGap{Family: family, ID: id, Reason: reason})
}

// scan records every needle the holder names, with the JSON paths.
func (sc *refScanner) scan(family, id, configID, managed string, doc any) {
	type found struct {
		paths    []string
		template bool
	}
	hits := map[string]*found{}
	add := func(needle, path string, template bool) {
		f := hits[needle]
		if f == nil {
			f = &found{}
			hits[needle] = f
		}
		f.paths = append(f.paths, path)
		f.template = f.template || template
	}
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch t := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				child := joinRefPath(path, k)
				for _, n := range matchNeedles(k, sc.needles) {
					add(n, child, false)
				}
				walk(child, t[k])
			}
		case []any:
			for i, item := range t {
				walk(fmt.Sprintf("%s[%d]", path, i), item)
			}
		case string:
			for _, n := range matchNeedles(t, sc.needles) {
				add(n, path, isTemplated(t))
			}
		}
	}
	walk("", doc)
	sc.mu.Lock()
	defer sc.mu.Unlock()
	for needle, f := range hits {
		sc.res.Hits = append(sc.res.Hits, refHit{Family: family, ID: id, ConfigID: configID, Managed: managed, Needle: needle, Paths: f.paths, Template: f.template})
	}
}

func joinRefPath(path, key string) string {
	plain := key != ""
	for _, r := range key {
		plain = plain && (r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	}
	switch {
	case !plain:
		quoted, _ := json.Marshal(key)
		return path + "[" + string(quoted) + "]"
	case path == "":
		return key
	}
	return path + "." + key
}

// matchNeedles returns the needles s names as whole ids: the characters
// around a match are not [A-Za-z0-9_], so sensor.temp never matches
// sensor.temp_2 or binary_sensor.temp, while states.sensor.temp.state does.
func matchNeedles(s string, needles []string) []string {
	var out []string
	for _, n := range needles {
		for start := 0; start < len(s); {
			i := strings.Index(s[start:], n)
			if i < 0 {
				break
			}
			pos, end := start+i, start+i+len(n)
			if (pos == 0 || !idChar(s[pos-1])) && (end == len(s) || !idChar(s[end])) {
				out = append(out, n)
				break
			}
			start = pos + 1
		}
	}
	return out
}

func idChar(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

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
			sc.scan(it.domain, it.entityID, it.configID, managed, doc)
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
		case isUpstreamNotFound(err): // auto-generated
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
