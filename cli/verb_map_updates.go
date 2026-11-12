package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// ha-nova map --section updates: the update advisor's check before installs
// (docs/work/2026-11-12-update-advisor-spec.md). Read-only facts and a
// default order; every install keeps its own confirmation (updates skill).
// A check that could not run is listed in not_checked and keeps ready from
// "yes": the advisor never reports a check it did not make as passed.

type updateRow struct {
	EntityID   string `json:"entity_id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"` // supervisor, os, core, app, integration, firmware
	Installed  string `json:"installed"`
	Latest     string `json:"latest"`
	AutoUpdate bool   `json:"auto_update,omitempty"`
	InProgress bool   `json:"in_progress,omitempty"`
	AfterCore  bool   `json:"after_core,omitempty"` // its App store minimum needs the new Core
	Blocked    bool   `json:"blocked,omitempty"`    // APP_NEEDS_NEWER_CORE: not in the order
	slug       string
	device     string
}

type updateAdvisor struct {
	s                 *relaySession
	x                 *homeIndex
	rows              []updateRow // pending
	coreFrom, coreTo  string
	hassio            bool
	domains           map[string]bool // loaded integrations: config entries and components
	ready, gaps, risk []map[string]any
	backupStale       bool
}

var updateKindRank = map[string]int{"supervisor": 0, "os": 1, "core": 2, "app": 3, "integration": 4, "firmware": 5}

// updateKind classifies by platform and unique_id, never by name (updates
// skill → Overview).
func updateKind(platform, uniqueID string) (kind, slug string) {
	switch platform {
	case "hacs", "marketplace":
		return "integration", ""
	case "hassio":
		switch uniqueID {
		case "home_assistant_core_version_latest":
			return "core", ""
		case "home_assistant_os_version_latest":
			return "os", ""
		case "home_assistant_supervisor_version_latest":
			return "supervisor", ""
		}
		if slug, ok := strings.CutSuffix(uniqueID, "_version_latest"); ok {
			return "app", slug
		}
	}
	return "firmware", ""
}

var (
	repairsRequest = map[string]any{"type": "repairs/list_issues"}
	backupRequest  = map[string]any{"type": "backup/info"}
	manifestList   = map[string]any{"type": "manifest/list"}
)

func adviseUpdates(s *relaySession, x *homeIndex) (map[string]any, error) {
	prefetch([]func(){
		func() { _, _, _ = s.coreGET("/api/config") },
		func() { _, _ = s.ws(repairsRequest) },
		func() { _, _ = s.ws(backupRequest) },
		func() { _, _ = s.ws(manifestList) },
	})
	a := &updateAdvisor{s: s, x: x, domains: map[string]bool{}, ready: []map[string]any{}, gaps: []map[string]any{}, risk: []map[string]any{}}
	if err := a.loadPending(); err != nil {
		return nil, err
	}
	if a.hassio { // Supervisor reads only where Supervisor runs
		paths := []string{"/resolution/info", "/host/info"}
		for _, row := range a.rows {
			if row.Kind == "app" {
				paths = append(paths, "/store/addons/"+row.slug)
			}
		}
		var reads []func()
		for _, path := range paths {
			reads = append(reads, func() { _, _ = s.read("supervisor", supervisorGet(path)) })
		}
		prefetch(reads)
	}
	a.checkSupervisor()
	a.checkBackups()
	a.checkRepairs()
	a.checkAppMinimums()
	ready := "yes"
	if len(a.gaps) > 0 || slices.ContainsFunc(a.ready, func(r map[string]any) bool { return r["level"] == "warning" }) {
		ready = "check"
	}
	if slices.ContainsFunc(a.ready, func(r map[string]any) bool { return r["level"] == "blocker" }) {
		ready = "no"
	}
	return map[string]any{"section": "updates", "ready": ready, "readiness": a.ready, "not_checked": a.gaps,
		"core": map[string]any{"installed": a.coreFrom, "latest": a.coreTo}, "pending": a.rows,
		"order": a.order(), "order_basis": "default", "risks": a.risk}, nil
}

func supervisorGet(path string) map[string]any {
	return map[string]any{"method": http.MethodGet, "path": path}
}

func (a *updateAdvisor) loadPending() error {
	status, body, err := a.s.coreGET("/api/config")
	if err == nil && status != http.StatusOK {
		err = fmt.Errorf("Home Assistant answered HTTP %d for /api/config", status)
	}
	var config struct {
		Version    string   `json:"version"`
		Components []string `json:"components"`
	}
	if err == nil {
		err = json.Unmarshal(body, &config)
	}
	if err != nil {
		return fmt.Errorf("cannot read /api/config: %w", err)
	}
	for _, component := range config.Components {
		for _, part := range strings.Split(component, ".") {
			a.domains[part] = true
		}
	}
	for _, e := range a.x.h.entries {
		a.domains[e.Domain] = true
	}
	a.hassio, a.coreFrom, a.coreTo = a.domains["hassio"], config.Version, config.Version
	var states []struct {
		EntityID   string `json:"entity_id"`
		State      string `json:"state"`
		Attributes struct {
			Installed  string `json:"installed_version"`
			Latest     string `json:"latest_version"`
			InProgress any    `json:"in_progress"` // a bool; a percentage on older cores
			AutoUpdate bool   `json:"auto_update"`
		} `json:"attributes"`
	}
	_, body, _ = a.s.coreGET("/api/states") // read and checked by loadHome
	if err := json.Unmarshal(body, &states); err != nil {
		return fmt.Errorf("cannot parse /api/states: %w", err)
	}
	reg := map[string]int{}
	for i, e := range a.x.h.entities {
		reg[e.EntityID] = i
	}
	var running []string
	for _, st := range states {
		if !inDomain(st.EntityID, "update") {
			continue
		}
		at := st.Attributes
		inProgress := at.InProgress == true
		if _, percent := at.InProgress.(float64); percent {
			inProgress = true
		}
		if inProgress {
			running = append(running, a.x.name(st.EntityID))
		}
		if st.State != "on" {
			continue
		}
		row := updateRow{EntityID: st.EntityID, Name: a.x.name(st.EntityID), Kind: "firmware", Installed: at.Installed, Latest: at.Latest, AutoUpdate: at.AutoUpdate, InProgress: inProgress}
		if i, ok := reg[st.EntityID]; ok {
			e := a.x.h.entities[i]
			row.Kind, row.slug = updateKind(e.Platform, e.UniqueID)
			row.device = e.DeviceID
		}
		if row.Kind == "core" {
			a.coreFrom, a.coreTo = firstNonEmpty(at.Installed, config.Version), at.Latest
		}
		a.rows = append(a.rows, row)
	}
	if a.rows == nil {
		a.rows = []updateRow{}
	}
	if len(running) > 0 {
		a.flag("UPDATE_RUNNING", "blocker", map[string]any{"updates": running})
	}
	return nil
}

// order: the default dependency order, Supervisor → OS → Core → Apps (the
// NOVA App last: it restarts the connection) → integrations (one restart) →
// firmware; a backup first when Core, OS or an App is pending without a
// backup from the last 24 hours. Blocked Apps stay out.
func (a *updateAdvisor) order() []string {
	rows := slices.Clone(a.rows)
	sort.SliceStable(rows, func(i, j int) bool {
		ri, rj := updateKindRank[rows[i].Kind], updateKindRank[rows[j].Kind]
		if ri != rj {
			return ri < rj
		}
		if ni, nj := rows[i].slug == haNovaRelayAppSlug, rows[j].slug == haNovaRelayAppSlug; ni != nj {
			return nj
		}
		return rows[i].Name < rows[j].Name
	})
	order := []string{}
	if a.backupStale && slices.ContainsFunc(rows, func(r updateRow) bool { return r.Kind == "core" || r.Kind == "os" || r.Kind == "app" }) {
		order = append(order, "backup")
	}
	for _, row := range rows {
		if !row.Blocked {
			order = append(order, row.EntityID)
		}
	}
	return order
}

var haVersionPattern = regexp.MustCompile(`^(\d+)\.(\d+)(?:\.(\d+))?(?:(?:b|\.dev)(\d+))?$`)

// haVersion: year, month, patch, then a beta (or dev) before its release.
func haVersion(v string) ([4]int, bool) {
	m := haVersionPattern.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return [4]int{}, false
	}
	var out [4]int
	for i, part := range m[1:4] {
		out[i], _ = strconv.Atoi(part)
	}
	out[3] = 1 << 30
	if m[4] != "" {
		out[3], _ = strconv.Atoi(m[4])
	}
	return out, true
}

func haVersionLess(a, b string) (less, ok bool) {
	va, okA := haVersion(a)
	vb, okB := haVersion(b)
	return slices.Compare(va[:], vb[:]) < 0, okA && okB
}
