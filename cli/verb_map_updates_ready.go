package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"
)

// The update advisor's readiness checks (verb_map_updates.go).

// supervisorData reads one Supervisor GET through the Relay and decodes its
// data object.
func (a *updateAdvisor) supervisorData(path string, into any) error {
	data, err := a.s.read("supervisor", supervisorGet(path))
	if err != nil {
		return err
	}
	var reply struct {
		Status int `json:"status"`
		Body   struct {
			Result string          `json:"result"`
			Data   json.RawMessage `json:"data"`
		} `json:"body"`
	}
	if err := json.Unmarshal(data, &reply); err != nil || reply.Status != http.StatusOK || reply.Body.Result != "ok" {
		return fmt.Errorf("the Supervisor answered %d for %s", reply.Status, path)
	}
	return json.Unmarshal(reply.Body.Data, into)
}

func (a *updateAdvisor) gap(check, reason string) {
	a.gaps = append(a.gaps, map[string]any{"check": check, "reason": reason})
}

func (a *updateAdvisor) flag(code, level string, facts map[string]any) {
	row := map[string]any{"code": code, "level": level}
	for k, v := range facts {
		row[k] = v
	}
	a.ready = append(a.ready, row)
}

// checkSupervisor: Supervisor refuses Core and OS updates while unhealthy and
// any update below its free-space floor (MINIMUM_FREE_SPACE_THRESHOLD, 2 GB).
// A home without Supervisor is told apart from one whose reads failed.
func (a *updateAdvisor) checkSupervisor() {
	if !a.hassio {
		a.flag("SUPERVISOR_NOT_APPLICABLE", "info", nil)
		return
	}
	var resolution struct {
		Unhealthy *[]string `json:"unhealthy"`
	}
	if err := a.supervisorData("/resolution/info", &resolution); err != nil || resolution.Unhealthy == nil {
		a.gap("supervisor_health", errText(err, "no unhealthy list"))
	} else if len(*resolution.Unhealthy) > 0 {
		a.flag("SUPERVISOR_UNHEALTHY", "blocker", map[string]any{"reasons": *resolution.Unhealthy})
	}
	var host struct {
		DiskFree *float64 `json:"disk_free"`
	}
	switch err := a.supervisorData("/host/info", &host); {
	case err != nil || host.DiskFree == nil:
		a.gap("disk", errText(err, "no disk_free"))
	case *host.DiskFree < 2:
		a.flag("DISK_LOW", "blocker", map[string]any{"free_gb": *host.DiskFree})
	}
}

func errText(err error, missing string) string {
	if err != nil {
		return err.Error()
	}
	return missing
}

func (a *updateAdvisor) checkBackups() {
	var info struct {
		Backups *[]struct {
			Date   string         `json:"date"`
			Agents map[string]any `json:"agents"`
		} `json:"backups"`
		State string `json:"state"`
	}
	if err := a.s.wsDecode(backupRequest, &info, "the backup list"); err != nil || info.Backups == nil {
		a.gap("backups", errText(err, "no backup list"))
		a.backupStale = true
		return
	}
	switch info.State {
	case "":
		a.gap("backup_running", "the backup list names no state")
	case "idle":
	default:
		a.flag("BACKUP_RUNNING", "warning", map[string]any{"state": info.State})
	}
	var newest time.Time
	var agents map[string]any
	for _, b := range *info.Backups {
		if at, err := time.Parse(time.RFC3339Nano, b.Date); err == nil && at.After(newest) {
			newest, agents = at, b.Agents
		}
	}
	if newest.IsZero() || a.x.now.Sub(newest) > 24*time.Hour {
		a.backupStale = true
		facts := map[string]any{"newest_age_hours": nil}
		if !newest.IsZero() {
			facts["newest_age_hours"] = int(a.x.now.Sub(newest).Hours())
		}
		a.flag("NO_RECENT_BACKUP", "warning", facts)
		return
	}
	if len(agents) == 0 {
		a.gap("backup_copy", "the backup list names no storage locations")
		return
	}
	for agent := range agents {
		if agent != "backup.local" && agent != "hassio.local" {
			return
		}
	}
	a.flag("BACKUP_ONLY_ON_DEVICE", "warning", nil)
}

type repairIssue struct {
	Domain       string         `json:"domain"`
	IssueID      string         `json:"issue_id"`
	Ignored      bool           `json:"ignored"`
	Created      string         `json:"created"`
	BreaksIn     string         `json:"breaks_in_ha_version"`
	Placeholders map[string]any `json:"translation_placeholders"`
}

func (a *updateAdvisor) repairs() ([]repairIssue, error) {
	var reply struct {
		Issues *[]repairIssue `json:"issues"`
	}
	if err := a.s.wsDecode(repairsRequest, &reply, "the repairs list"); err != nil || reply.Issues == nil {
		return nil, fmt.Errorf("%s", errText(err, "no repairs list"))
	}
	return slices.DeleteFunc(*reply.Issues, func(i repairIssue) bool { return i.Ignored }), nil
}

// checkRepairs: open repairs, with Home Assistant's known-issue alerts for
// this home's integrations named (their titles are data, defanged).
func (a *updateAdvisor) checkRepairs() {
	issues, err := a.repairs()
	if err != nil {
		a.gap("repairs", err.Error())
		return
	}
	if len(issues) == 0 {
		return
	}
	alerts := []string{}
	for _, issue := range issues {
		if issue.Domain == "homeassistant_alerts" {
			title, _ := issue.Placeholders["title"].(string)
			alerts = append(alerts, sayTextUpTo(firstNonEmpty(title, issue.IssueID), 120, false))
		}
	}
	a.flag("REPAIRS_OPEN", "warning", map[string]any{"count": len(issues), "alerts": alerts})
}

// checkAppMinimums: an App whose store minimum needs a newer Core than the
// one offered can't be installed now; one that needs the offered Core goes
// after it.
func (a *updateAdvisor) checkAppMinimums() {
	for i := range a.rows {
		row := &a.rows[i]
		if row.Kind != "app" {
			continue
		}
		var store map[string]json.RawMessage
		err := a.supervisorData("/store/addons/"+row.slug, &store)
		raw, listed := store["homeassistant"]
		if err == nil && !listed {
			err = errors.New("the store entry names no minimum")
		}
		if err != nil {
			a.gap("app_minimum", row.Name+": "+err.Error())
			continue
		}
		var needs string // null: no minimum
		if json.Unmarshal(raw, &needs) != nil || needs == "" {
			continue
		}
		aboveLatest, ok1 := haVersionLess(a.coreTo, needs)
		aboveInstalled, ok2 := haVersionLess(a.coreFrom, needs)
		switch {
		case !ok1 || !ok2:
			a.gap("app_minimum", row.Name+": unreadable version "+sayText(needs, false))
		case aboveLatest:
			row.Blocked = true
			a.flag("APP_NEEDS_NEWER_CORE", "blocker", map[string]any{"app": row.Name, "needs": needs, "core_offered": a.coreTo})
		case aboveInstalled:
			row.AfterCore = true
		}
	}
}
