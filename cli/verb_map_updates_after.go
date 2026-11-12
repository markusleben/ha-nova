package main

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"
)

// ha-nova map --section after_update: what changed since the update
// advisor's check (update-advisor spec → Phase 2). The check saves a
// baseline per server and keeps one younger than baselineKeep, so a second
// check during an update session keeps the pre-update evidence.

const baselineKeep = 12 * time.Hour

type updateBaseline struct {
	Server      string               `json:"server"`
	TakenAt     time.Time            `json:"taken_at"`
	Versions    map[string][2]string `json:"versions"` // update entity → installed, offered
	Repairs     []string             `json:"repairs"`
	Resolution  []string             `json:"resolution"` // Supervisor issue uuids; nil when unread
	Failing     map[string]string    `json:"failing"`    // config entry → state
	Unavailable []string             `json:"unavailable"`
}

var failingEntryStates = []string{"setup_error", "setup_retry", "migration_error", "failed_unload"}

func baselineFile(paths runtimePaths, server string) string {
	return filepath.Join(paths.CacheDir, "updates-baseline-"+server+".json")
}

func loadBaseline(file, server string) *updateBaseline {
	raw, err := os.ReadFile(file)
	var b updateBaseline
	if err != nil || json.Unmarshal(raw, &b) != nil || b.Server != server || b.TakenAt.IsZero() {
		return nil
	}
	return &b
}

// snapshot: the state the after-update check compares with. Repairs
// and Resolution stay nil when unread.
func (a *updateAdvisor) snapshot() updateBaseline {
	b := updateBaseline{Server: a.s.server, TakenAt: a.x.now.UTC(), Versions: a.versions, Resolution: a.resolution, Failing: map[string]string{}, Unavailable: []string{}}
	if issues, err := a.repairs(); err == nil {
		b.Repairs = []string{}
		for _, issue := range issues {
			b.Repairs = append(b.Repairs, issue.Domain+"/"+issue.IssueID)
		}
	}
	for _, e := range a.x.h.entries {
		if slices.Contains(failingEntryStates, e.State) {
			b.Failing[e.EntryID] = e.State
		}
	}
	for _, id := range a.x.enabled {
		if st := a.x.state[id]; st != nil && st.State == "unavailable" {
			b.Unavailable = append(b.Unavailable, id)
		}
	}
	return b
}

// saveBaseline keeps a baseline younger than baselineKeep for this server,
// and saves none while an update runs (its state is half done).
func (a *updateAdvisor) saveBaseline(file string) map[string]any {
	old := loadBaseline(file, a.s.server)
	if old != nil && !old.TakenAt.After(a.x.now) && a.x.now.Sub(old.TakenAt) < baselineKeep {
		return map[string]any{"state": "kept", "taken_at": old.TakenAt}
	}
	if slices.ContainsFunc(a.ready, func(r map[string]any) bool { return r["code"] == "UPDATE_RUNNING" }) {
		return map[string]any{"state": "not_saved", "reason": "an update is running"}
	}
	b := a.snapshot()
	raw, err := json.Marshal(b)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(file), 0o700)
	}
	if err == nil {
		err = writeFileAtomic(file, raw, 0o600)
	}
	if err != nil {
		return map[string]any{"state": "not_saved", "reason": err.Error()}
	}
	return map[string]any{"state": "saved", "taken_at": b.TakenAt}
}

func afterUpdate(s *relaySession, x *homeIndex, file string) (map[string]any, error) {
	prefetch([]func(){
		func() { _, _, _ = s.coreGET("/api/config") },
		func() { _, _ = s.ws(repairsRequest) },
		func() { _, _ = s.ws(tracesRequest) },
	})
	a := &updateAdvisor{s: s, x: x, domains: map[string]bool{}, ready: []map[string]any{}, gaps: []map[string]any{}}
	if err := a.loadPending(); err != nil {
		return nil, err
	}
	if a.hassio {
		_, _ = a.readResolution() // unread: rollback says unknown
	}
	if a.coreState != "" && a.coreState != "RUNNING" {
		a.gap("started", "Home Assistant is still starting ("+a.coreState+"): integrations and devices may still come up; check again in a few minutes")
	}
	now := a.snapshot()
	base := loadBaseline(file, s.server)
	if base != nil && base.TakenAt.After(x.now) {
		base = nil // a clock that went back: no usable comparison
	}
	out := map[string]any{"section": "after_update", "baseline": nil, "still_pending": a.rows, "rollback": a.rollback(base)}
	if base == nil {
		a.gap("since_check", "no check before the update: run --section updates before the next install")
		failing := []map[string]any{}
		for _, e := range x.h.entries {
			if state, bad := now.Failing[e.EntryID]; bad {
				failing = append(failing, map[string]any{"integration": e.Domain, "state": state})
			}
		}
		out["failing_integrations_now"], out["not_checked"] = failing, a.gaps
		return out, nil
	}
	out["baseline"] = map[string]any{"taken_at": base.TakenAt}
	updated := []map[string]any{}
	for _, id := range slices.Sorted(maps.Keys(now.Versions)) {
		before, ok := base.Versions[id]
		if ok && before[0] != now.Versions[id][0] {
			updated = append(updated, map[string]any{"entity_id": id, "name": x.name(id), "from": before[0], "to": now.Versions[id][0], "reached": now.Versions[id][0] == before[1]})
		}
	}
	failing := []map[string]any{}
	for _, e := range x.h.entries {
		if state, bad := now.Failing[e.EntryID]; bad && base.Failing[e.EntryID] != state {
			failing = append(failing, map[string]any{"integration": e.Domain, "state": state})
		}
	}
	out["new_repairs"] = nil
	if now.Repairs == nil || base.Repairs == nil {
		a.gap("repairs", "the repairs list was not read before and after the update")
	} else {
		newRepairs := []string{}
		for _, key := range now.Repairs {
			if !slices.Contains(base.Repairs, key) {
				newRepairs = append(newRepairs, sayText(key, false))
			}
		}
		out["new_repairs"] = newRepairs
	}
	gone, back := []string{}, 0
	for _, id := range now.Unavailable {
		if !slices.Contains(base.Unavailable, id) {
			gone = append(gone, x.name(id))
		}
	}
	for _, id := range base.Unavailable {
		if !slices.Contains(now.Unavailable, id) {
			back++
		}
	}
	out["updated"], out["new_failing_integrations"] = updated, failing
	out["newly_unavailable"] = map[string]any{"count": len(gone), "names": gone[:min(len(gone), 20)]}
	out["available_again"] = back
	out["automation_errors"], out["traces_read"] = a.automationErrors(base.TakenAt)
	out["not_checked"] = a.gaps
	return out, nil
}

// rollback: Supervisor's update_rollback issues (Core went back to the
// version before) and update_failed issues. One the check before already
// listed counts again when it names a version offered then that is still
// not installed (Supervisor may keep one issue for a repeated failure).
// Unreadable Supervisor: unknown, never "none".
func (a *updateAdvisor) rollback(base *updateBaseline) any {
	if !a.hassio {
		return "not_applicable"
	}
	if a.resolution == nil || base != nil && base.Resolution == nil {
		a.gap("rollback", "the Supervisor's issue list was not read before and after the update")
		return "unknown"
	}
	retried := func(version string) bool {
		for id, v := range base.Versions {
			if v[1] == version && a.versions[id][0] != version {
				return true
			}
		}
		return false
	}
	out := map[string]any{"rolled_back": []string{}, "failed": []string{}}
	for _, issue := range a.resolutionIssues {
		if base != nil && slices.Contains(base.Resolution, issue.UUID) && !(issue.Type == "update_rollback" && retried(issue.Reference)) {
			continue
		}
		switch issue.Type {
		case "update_rollback":
			out["rolled_back"] = append(out["rolled_back"].([]string), sayText(issue.Reference, false))
		case "update_failed":
			out["failed"] = append(out["failed"].([]string), sayText(issue.Context, false))
		}
	}
	return out
}

var tracesRequest = map[string]any{"type": "trace/list", "domain": "automation"}

// automationErrors: automations whose stored runs ended with an error since
// the check. Home Assistant keeps only a few runs per automation.
func (a *updateAdvisor) automationErrors(since time.Time) ([]map[string]any, int) {
	var traces []struct {
		ItemID    string `json:"item_id"`
		Error     string `json:"error"`
		Timestamp struct {
			Start time.Time `json:"start"`
		} `json:"timestamp"`
	}
	if err := a.s.wsDecode(tracesRequest, &traces, "the automation runs"); err != nil {
		a.gap("automation_errors", err.Error())
		return nil, 0
	}
	var states []struct {
		EntityID   string `json:"entity_id"`
		Attributes struct {
			ID string `json:"id"`
		} `json:"attributes"`
	}
	_, body, _ := a.s.coreGET("/api/states")
	_ = json.Unmarshal(body, &states)
	entity := map[string]string{}
	for _, st := range states {
		if inDomain(st.EntityID, "automation") && st.Attributes.ID != "" {
			entity[st.Attributes.ID] = st.EntityID
		}
	}
	counts, last := map[string]int{}, map[string]string{}
	for _, t := range traces {
		if t.Error == "" || !t.Timestamp.Start.After(since) {
			continue
		}
		name := a.x.name(firstNonEmpty(entity[t.ItemID], "automation."+t.ItemID))
		counts[name]++
		last[name] = sayTextUpTo(t.Error, 200, false)
	}
	out := []map[string]any{}
	for _, name := range slices.Sorted(maps.Keys(counts)) {
		out = append(out, map[string]any{"name": name, "errors": counts[name], "last_error": last[name]})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i]["errors"].(int) > out[j]["errors"].(int) })
	return out, len(traces)
}
