package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
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
	ID           string   `json:"id"`
	Name         string   `json:"name,omitempty"`
	Manufacturer string   `json:"manufacturer,omitempty"`
	Model        string   `json:"model,omitempty"`
	AreaID       string   `json:"area_id,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	// LegacyIDs: pre-2026.9 merged device ids that Home Assistant still
	// resolves to this device (config/device_registry/list_composite_splits).
	LegacyIDs []string `json:"legacy_ids,omitempty"`
}

// refEntity carries the mapping hints a swap matches old to new entities by.
type refEntity struct {
	EntityID       string   `json:"entity_id"`
	RegistryID     string   `json:"registry_id,omitempty"`
	Platform       string   `json:"platform,omitempty"`
	Name           string   `json:"name,omitempty"`
	OriginalName   string   `json:"original_name,omitempty"`
	TranslationKey string   `json:"translation_key,omitempty"`
	DeviceClass    string   `json:"device_class,omitempty"`
	Unit           string   `json:"unit,omitempty"`
	State          string   `json:"state,omitempty"`
	AreaID         string   `json:"area_id,omitempty"`
	Labels         []string `json:"labels,omitempty"`
	Disabled       bool     `json:"disabled,omitempty"`
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
	"helpers that show no source attribute (switch_as_x, statistics, filter, trend, history_stats, proximity, generic_thermostat, generic_hygrostat, mold_indicator, bayesian)",
	"targets by area, floor, label, or a parent device: they reach the device by membership, not by id",
	"integration options that list entities (HomeKit Bridge filters)",
	"YAML-only integrations (alert, notify groups, packages outside automations/scripts)",
	"Node-RED, AppDaemon, and other App or HACS consumer managers",
	"protocol-level groups and bindings inside ZHA or Zigbee2MQTT",
}

// sourceHelperPlatforms show their sources as state attributes, and only
// while available.
var sourceHelperPlatforms = map[string]bool{"group": true, "min_max": true, "utility_meter": true, "derivative": true,
	"integration": true, "threshold": true}

type refRegistryRow struct {
	ID             string   `json:"id"`
	EntityID       string   `json:"entity_id"`
	UniqueID       string   `json:"unique_id"`
	Platform       string   `json:"platform"`
	DeviceID       string   `json:"device_id"`
	ConfigEntryID  string   `json:"config_entry_id"`
	AreaID         string   `json:"area_id"`
	Labels         []string `json:"labels"`
	DisabledBy     string   `json:"disabled_by"`
	Name           string   `json:"name"`
	OriginalName   string   `json:"original_name"`
	TranslationKey string   `json:"translation_key"`
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
	platform := map[string]string{}
	for _, row := range registry {
		platform[row.EntityID] = row.Platform
	}
	for _, st := range states {
		if own[st.EntityID] {
			continue // an entity's own attributes never re-point it
		}
		if st.State == "unavailable" && sourceHelperPlatforms[platform[st.EntityID]] {
			// Home Assistant drops state attributes while an entity is
			// unavailable: a helper fed only by a dead device hides its source.
			sc.gap("entity", st.EntityID, "unavailable: its source attributes are hidden until it recovers")
			continue
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
		var devices []struct {
			ID            string   `json:"id"`
			Name          string   `json:"name"`
			NameByUser    *string  `json:"name_by_user"`
			Manufacturer  string   `json:"manufacturer"`
			Model         string   `json:"model"`
			AreaID        string   `json:"area_id"`
			Labels        []string `json:"labels"`
			ConfigEntries []string `json:"config_entries"`
			Owner         string   `json:"config_entry_id"`      // HA 2026.8+: the one owning entry
			Primary       string   `json:"primary_config_entry"` // before: the primary of several
		}
		if err := s.wsDecode(map[string]any{"type": "config/device_registry/list"}, &devices, "the device registry"); err != nil {
			return nil, nil, err
		}
		res.Device = &refDevice{ID: self.DeviceID}
		entries := map[string]bool{}
		name := ""
		for _, d := range devices {
			if d.ID == self.DeviceID {
				res.Device = &refDevice{ID: d.ID, Name: d.Name, Manufacturer: d.Manufacturer, Model: d.Model, AreaID: d.AreaID, Labels: d.Labels}
				if d.NameByUser != nil && *d.NameByUser != "" {
					res.Device.Name = *d.NameByUser
				}
				owner := d.Owner
				if owner == "" {
					owner = d.Primary
				}
				for _, e := range append(d.ConfigEntries, owner) {
					entries[e] = e != "" && (owner == "" || e == owner)
				}
				name = d.Name
			}
		}
		// A helper linked to its source device (utility meter, derivative,
		// switch_as_x …) belongs to another config entry: a consumer, never
		// one of the device's own entities.
		rows = nil
		for _, row := range registry {
			if row.DeviceID == self.DeviceID && (len(entries) == 0 || row.ConfigEntryID == "" || entries[row.ConfigEntryID]) {
				rows = append(rows, row)
			}
		}
		res.notifyService(s, name, rows)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].EntityID < rows[j].EntityID })
	needles, own := []string{}, map[string]bool{}
	for _, row := range rows {
		st := states[row.EntityID]
		e := refEntity{EntityID: row.EntityID, RegistryID: row.ID, Platform: row.Platform, Name: row.Name, OriginalName: row.OriginalName,
			TranslationKey: row.TranslationKey, State: st.State, AreaID: row.AreaID, Labels: row.Labels, Disabled: row.DisabledBy != ""}
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
		res.legacyDeviceIDs(s)
		needles = append(needles, res.Device.LegacyIDs...)
	}
	if res.NotifyService != "" {
		needles = append(needles, strings.TrimPrefix(res.NotifyService, "notify."))
	}
	return needles, own, nil
}

// legacyDeviceIDs: every merged device id whose split list names this
// device; stored targets can still use it. A core without the command has
// none; any other failure is a gap, never a silent miss.
func (res *referencesResult) legacyDeviceIDs(s *relaySession) {
	var rows map[string]struct {
		SplitIDs []string `json:"split_ids"`
	}
	err := s.wsDecode(map[string]any{"type": "config/device_registry/list_composite_splits"}, &rows, "the device splits")
	switch {
	case isUpstreamUnknownCommand(err):
	case err != nil:
		res.NotScanned = append(res.NotScanned, refGap{Family: "device", ID: res.Device.ID, Reason: "merged (pre-2026.9) device ids could not be read: " + err.Error()})
	}
	for id, row := range rows {
		if slices.Contains(row.SplitIDs, res.Device.ID) {
			res.Device.LegacyIDs = append(res.Device.LegacyIDs, id)
		}
	}
	sort.Strings(res.Device.LegacyIDs)
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

type refFound struct {
	paths    []string
	template bool
}

// scan records every needle the holder names, with the JSON paths.
func (sc *refScanner) scan(family, id, configID, managed string, doc any) {
	sc.record(family, id, configID, managed, sc.find(doc))
}

func (sc *refScanner) find(doc any) map[string]*refFound {
	hits := map[string]*refFound{}
	add := func(needle, path string, template bool) {
		f := hits[needle]
		if f == nil {
			f = &refFound{}
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
	return hits
}

// record appends one hit per needle found.
func (sc *refScanner) record(family, id, configID, managed string, hits map[string]*refFound) {
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
