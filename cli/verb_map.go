package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// ha-nova map: exact, CLI-computed home facts (home-map spec
// docs/work/2026-10-16-home-map-spec.md). Read-only; the model never counts.

const mapUsage = "ha-nova map [--section areas|integrations|automations|unavailable|batteries] [--entity <entity_id>] [--first-look] [--limit N]"

var mapSections = map[string]bool{"areas": true, "integrations": true, "automations": true, "unavailable": true, "batteries": true}

func runMapCommand(paths runtimePaths, args []string) int {
	var opts verbOptions
	fs := flag.NewFlagSet("map", flag.ContinueOnError)
	addVerbFlags(fs, &opts)
	section := fs.String("section", "", "full list of one part: areas, integrations, automations, unavailable, batteries")
	entity := fs.String("entity", "", "where-used of one entity_id (automations, scripts, scenes, groups)")
	firstLook := fs.Bool("first-look", false, "the onboarding first look: one-line counts, up to 2 observations, prompt seeds")
	limit := fs.Int("limit", 0, "rows per list for --section (default 50) and --entity (default 20), 1-200; totals stay exact")
	positional, err := parseVerbArgs(fs, &opts, args, mapUsage)
	modes := 0
	for _, on := range []bool{*section != "", *entity != "", *firstLook} {
		if on {
			modes++
		}
	}
	limitSet := false
	fs.Visit(func(f *flag.Flag) { limitSet = limitSet || f.Name == "limit" })
	switch {
	case err != nil:
	case len(positional) > 0:
		err = errors.New("Usage: " + mapUsage)
	case modes > 1:
		err = errors.New("use only one of --section, --entity, --first-look")
	case *section != "" && !mapSections[*section]:
		err = fmt.Errorf("unknown --section %q (areas, integrations, automations, unavailable, batteries)", *section)
	case *entity != "" && !entityIDPattern.MatchString(*entity):
		err = fmt.Errorf("%q is not an entity_id (domain.object_id)", *entity)
	case limitSet && (*section == "" && *entity == ""):
		err = errors.New("--limit applies only to --section and --entity")
	case limitSet && (*limit < 1 || *limit > 200):
		err = errors.New("--limit must be between 1 and 200")
	}
	if err != nil {
		return verbUsageError("map", err)
	}
	return runReadVerb(paths, "map", opts, func(s *relaySession, env *verbEnvelope) error {
		home, err := loadHome(s)
		if err != nil {
			return err
		}
		idx := indexHome(home, time.Now())
		switch {
		case *firstLook:
			env.Result = idx.firstLook()
		case *section != "":
			env.Result = idx.section(*section, limitOr(*limit, 50))
		case *entity != "":
			env.Result, err = idx.whereUsed(s, *entity, limitOr(*limit, 20))
		default:
			env.Result = idx.summary()
		}
		return err
	})
}

func limitOr(limit, fallback int) int {
	if limit == 0 {
		return fallback
	}
	return limit
}

type homeData struct {
	floors []floorRow
	areas  []struct {
		AreaID  string `json:"area_id"`
		Name    string `json:"name"`
		FloorID string `json:"floor_id"`
	}
	devices []struct {
		ID            string   `json:"id"`
		AreaID        string   `json:"area_id"`
		Parent        string   `json:"parent_device_id"`
		DisabledBy    string   `json:"disabled_by"`
		ConfigEntry   string   `json:"config_entry_id"`
		PrimaryEntry  string   `json:"primary_config_entry"`
		ConfigEntries []string `json:"config_entries"`
		Name          string   `json:"name"`
		NameByUser    string   `json:"name_by_user"`
		EntryType     string   `json:"entry_type"` // "service": a cloud/service device, never in a room
	}
	entities []struct {
		EntityID     string `json:"entity_id"`
		Name         string `json:"name"`
		OriginalName string `json:"original_name"`
		AreaID       string `json:"area_id"`
		DeviceID     string `json:"device_id"`
		DisabledBy   string `json:"disabled_by"`
		Platform     string `json:"platform"`
		Category     string `json:"entity_category"`
		HiddenBy     string `json:"hidden_by"`
		ConfigEntry  string `json:"config_entry_id"`
	}
	entries, helperEntries []struct {
		EntryID    string `json:"entry_id"`
		Domain     string `json:"domain"`
		Source     string `json:"source"`
		State      string `json:"state"`
		DisabledBy string `json:"disabled_by"`
	}
	states []mapState
}

type floorRow struct {
	FloorID string `json:"floor_id"`
	Name    string `json:"name"`
	Level   *int   `json:"level"`
}

type mapState struct {
	EntityID    string `json:"entity_id"`
	State       string `json:"state"`
	LastChanged string `json:"last_changed"`
	Attributes  struct {
		FriendlyName  string  `json:"friendly_name"`
		DeviceClass   any     `json:"device_class"`
		LastTriggered *string `json:"last_triggered"`
		Members       any     `json:"entity_id"`    // a list on group entities
		Restored      bool    `json:"restored"`     // registry row kept, integration no longer provides it
		HueGroup      bool    `json:"is_hue_group"` // a Hue room or zone
		CodeFormat    any     `json:"code_format"`  // a keypad lock: HA asks for its own code
	} `json:"attributes"`
}

func (st *mapState) deviceClass() string { s, _ := st.Attributes.DeviceClass.(string); return s }

func (st *mapState) isGroup() bool {
	domain, _ := splitID(st.EntityID)
	_, list := st.Attributes.Members.([]any)
	return (list || st.Attributes.HueGroup) && !notGroupDomains[domain]
}

var (
	floorsRequest  = map[string]any{"type": "config/floor_registry/list"}
	entriesRequest = map[string]any{"type": "config_entries/get"}
	helpersRequest = map[string]any{"type": "config_entries/get", "type_filter": []string{"helper"}}
)

// loadHome runs every read in parallel (session slots bound it), then
// decodes the memoized answers. Any failed read fails the map: counts are
// never computed from part of the home. A core without floors (before 2024.4)
// has none.
func loadHome(s *relaySession) (*homeData, error) {
	ws := []map[string]any{floorsRequest, {"type": "config/area_registry/list"}, {"type": "config/device_registry/list"},
		{"type": "config/entity_registry/list"}, entriesRequest, helpersRequest}
	reads := []func(){func() { _, _, _ = s.coreGET("/api/states") }}
	for _, request := range ws {
		reads = append(reads, func() { _, _ = s.ws(request) })
	}
	prefetch(reads)
	h := &homeData{floors: []floorRow{}}
	if err := s.wsDecode(floorsRequest, &h.floors, "the floor registry"); err != nil && !isUpstreamUnknownCommand(err) {
		return nil, err
	}
	for _, read := range []struct {
		request map[string]any
		into    any
		what    string
	}{{ws[1], &h.areas, "the area registry"}, {ws[2], &h.devices, "the device registry"}, {ws[3], &h.entities, "the entity registry"},
		{entriesRequest, &h.entries, "the config entries"}, {helpersRequest, &h.helperEntries, "the helper config entries"}} {
		if err := s.wsDecode(read.request, read.into, read.what); err != nil {
			return nil, err
		}
	}
	status, body, err := s.coreGET("/api/states")
	if err == nil && status != http.StatusOK {
		err = fmt.Errorf("Home Assistant answered HTTP %d for /api/states", status)
	}
	if err == nil {
		if err = json.Unmarshal(body, &h.states); err != nil {
			err = fmt.Errorf("cannot parse /api/states: %w", err)
		}
	}
	return h, err
}

// homeIndex joins the reads once; every count reads from it.
type homeIndex struct {
	h            *homeData
	now          time.Time
	areaName     map[string]string
	floorName    map[string]string
	deviceArea   map[string]string // effective, enabled and disabled devices
	deviceOn     map[string]bool   // device not disabled
	service      map[string]bool   // entry_type service: no room to be in
	children     map[string][]string
	deviceDomain map[string]string // owning integration domain
	entryDomain  map[string]string
	helperEntry  map[string]bool
	state        map[string]*mapState
	registered   map[string]bool // any registry row, disabled included
	platform     map[string]string
	category     map[string]string // entity_category: config, diagnostic
	hidden       map[string]bool
	entityArea   map[string]string // enabled entities: effective area_id
	entityDevice map[string]string
	regName      map[string]string
	enabled      []string // enabled registry rows + unregistered states, sorted
	disabled     []string // registry-disabled entity ids, sorted
}

func indexHome(h *homeData, now time.Time) *homeIndex {
	x := &homeIndex{h: h, now: now, service: map[string]bool{}, children: map[string][]string{}, areaName: map[string]string{}, floorName: map[string]string{}, deviceOn: map[string]bool{},
		deviceDomain: map[string]string{}, entryDomain: map[string]string{}, helperEntry: map[string]bool{}, state: map[string]*mapState{},
		registered: map[string]bool{}, platform: map[string]string{}, category: map[string]string{}, hidden: map[string]bool{}, entityArea: map[string]string{}, entityDevice: map[string]string{}, regName: map[string]string{}}
	for _, f := range h.floors {
		x.floorName[f.FloorID] = f.Name
	}
	for _, a := range h.areas {
		x.areaName[a.AreaID] = a.Name
	}
	for _, e := range h.entries {
		x.entryDomain[e.EntryID] = e.Domain
	}
	for _, e := range h.helperEntries {
		x.helperEntry[e.EntryID] = true
	}
	own, parent := map[string]string{}, map[string]string{}
	for _, d := range h.devices {
		own[d.ID], parent[d.ID] = d.AreaID, d.Parent
		x.deviceOn[d.ID] = d.DisabledBy == ""
		x.service[d.ID] = d.EntryType == "service"
		if d.Parent != "" {
			x.children[d.Parent] = append(x.children[d.Parent], d.ID)
		}
		entry := d.ConfigEntry
		if entry == "" {
			entry = d.PrimaryEntry
		}
		if entry == "" && len(d.ConfigEntries) > 0 {
			entry = d.ConfigEntries[0]
		}
		x.deviceDomain[d.ID] = x.entryDomain[entry]
	}
	x.deviceArea = effectiveDeviceAreas(own, parent)
	for i := range h.states {
		x.state[h.states[i].EntityID] = &h.states[i]
	}
	for _, e := range h.entities {
		x.registered[e.EntityID] = true
		x.platform[e.EntityID] = e.Platform
		x.regName[e.EntityID] = firstNonEmpty(e.Name, e.OriginalName)
		x.entityDevice[e.EntityID] = e.DeviceID
		x.category[e.EntityID], x.hidden[e.EntityID] = e.Category, e.HiddenBy != ""
		if e.DisabledBy != "" {
			x.disabled = append(x.disabled, e.EntityID)
			continue
		}
		x.enabled = append(x.enabled, e.EntityID)
		area := e.AreaID
		if area == "" {
			area = x.deviceArea[e.DeviceID]
		}
		if area != "" {
			x.entityArea[e.EntityID] = area
		}
	}
	for _, st := range h.states {
		if !x.registered[st.EntityID] {
			x.enabled = append(x.enabled, st.EntityID)
		}
	}
	sort.Strings(x.enabled)
	sort.Strings(x.disabled)
	return x
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// name: the state's friendly_name (Home Assistant's display name), else the
// registry name, else the entity_id.
func (x *homeIndex) name(id string) string {
	friendly := ""
	if st := x.state[id]; st != nil {
		friendly = st.Attributes.FriendlyName
	}
	return firstNonEmpty(friendly, x.regName[id], id)
}

func (x *homeIndex) area(id string) string { return x.areaName[x.entityArea[id]] }

// age is the time since the current state row began: a lower bound of how
// long the entity has been in it, never the outage length.
func (x *homeIndex) age(st *mapState) (time.Duration, bool) {
	changed, err := time.Parse(time.RFC3339Nano, st.LastChanged)
	if err != nil {
		return 0, false
	}
	return x.now.Sub(changed), true
}

func shortAge(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dm", max(0, int(d/time.Minute)))
}

func inDomain(id, domain string) bool { return strings.HasPrefix(id, domain+".") }
