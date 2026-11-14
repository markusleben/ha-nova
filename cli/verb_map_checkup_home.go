package main

import (
	"cmp"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"
)

// The check-up's integrations, repairs, backup, and updates (verb_map_checkup.go).

// checkIntegrations: config entries that failed to load, and the log facts
// (#1141) at ERROR or worse per integration: a lead, never a verdict.
func (c *checkup) checkIntegrations(logs json.RawMessage, logsErr error) {
	names := map[string]string{}
	var manifests []struct {
		Domain string `json:"domain"`
		Name   string `json:"name"`
	}
	if c.a.s.wsDecode(manifestList, &manifests, "the integration list") == nil {
		for _, m := range manifests {
			names[m.Domain] = sayText(firstNonEmpty(m.Name, m.Domain), false)
		}
	}
	type failing struct {
		Domain string `json:"domain"`
		Name   string `json:"name"`
		State  string `json:"state"`
		Count  int    `json:"count"`
	}
	byKey := map[string]*failing{}
	for _, e := range c.a.x.h.entries {
		if !slices.Contains(failingEntryStates, e.State) || e.DisabledBy != "" || !logFactsDomain.MatchString(e.Domain) {
			continue
		}
		key := e.Domain + " " + e.State
		if byKey[key] == nil {
			byKey[key] = &failing{Domain: e.Domain, Name: firstNonEmpty(names[e.Domain], e.Domain), State: e.State}
		}
		byKey[key].Count++
	}
	rows := []failing{}
	for _, key := range slices.Sorted(maps.Keys(byKey)) {
		rows = append(rows, *byKey[key])
	}
	if len(rows) > 0 {
		c.find("INTEGRATIONS_FAILING", "problem", "integrations", map[string]any{"integrations": rows})
	}
	facts := map[string]any{"failing": len(rows)}
	c.facts["integrations"] = facts
	var parsed logFacts
	if logsErr == nil && json.Unmarshal(logs, &parsed) != nil {
		logsErr = errCheckupShape
	}
	if logsErr != nil {
		c.gap("log_facts", logsErr)
		return
	}
	type logged struct {
		Domain string `json:"domain"`
		Name   string `json:"name"`
		Count  int64  `json:"count"`
		Since  string `json:"since"`
		Last   string `json:"last"`
	}
	byDomain := map[string]*logged{}
	for _, f := range parsed.LogFacts {
		if f.Level != "ERROR" && f.Level != "CRITICAL" {
			continue
		}
		row := byDomain[f.Domain]
		if row == nil {
			row = &logged{Domain: f.Domain, Name: firstNonEmpty(names[f.Domain], f.Domain), Since: f.FirstRetained, Last: f.Last}
			byDomain[f.Domain] = row
		}
		row.Count += f.Count
		row.Since, row.Last = min(row.Since, f.FirstRetained), max(row.Last, f.Last) // RFC 3339 UTC: text order is time order
	}
	ranked := slices.SortedFunc(maps.Values(byDomain), func(a, b *logged) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Domain, b.Domain))
	})
	top := []logged{}
	for _, row := range ranked[:min(len(ranked), 5)] {
		top = append(top, *row)
	}
	facts["logging_errors"] = top
}

// checkRepairs: open repairs, without the Supervisor issues already listed.
func (c *checkup) checkRepairs() {
	if c.repairsE != nil {
		c.gap("repairs", c.repairsE)
		return
	}
	count, alerts, domains := 0, 0, []string{}
	for _, issue := range c.repairs {
		if issue.Domain == "hassio" && c.listed[issue.IssueID] {
			continue
		}
		count++
		if issue.Domain == "homeassistant_alerts" {
			alerts++
		}
		if logFactsDomain.MatchString(issue.Domain) && !slices.Contains(domains, issue.Domain) {
			domains = append(domains, issue.Domain)
		}
	}
	if count > 0 {
		slices.Sort(domains)
		c.find("REPAIRS_OPEN", "problem", "repairs", map[string]any{"count": count, "alerts": alerts, "domains": domains[:min(len(domains), 10)]})
	}
}

// Home Assistant's BackupManagerState words.
var checkupBackupStates = []string{"idle", "create_backup", "blocked", "receive_backup", "restore_backup"}

type checkupBackup struct {
	Date       string `json:"date"`
	HAIncluded bool   `json:"homeassistant_included"`
	DBIncluded bool   `json:"database_included"`
	Agents     map[string]struct {
		Protected bool    `json:"protected"`
		Size      float64 `json:"size"`
	} `json:"agents"`
	FailedAgents  []string `json:"failed_agent_ids"`
	FailedFolders []string `json:"failed_folders"`
	FailedApps    []struct {
		Slug string `json:"slug"`
	} `json:"failed_addons"`
}

// checkupLocations: the integration domains of backup locations (agent ids
// are <domain>.<name>; the name part can be a label someone typed).
func checkupLocations(agents []string) []string {
	out := []string{}
	for _, agent := range agents {
		domain, _, _ := strings.Cut(agent, ".")
		if logFactsDomain.MatchString(domain) && !slices.Contains(out, domain) {
			out = append(out, domain)
		}
	}
	slices.Sort(out)
	return out
}

func checkupTime(raw *string) (time.Time, bool) {
	if raw == nil {
		return time.Time{}, false
	}
	at, err := time.Parse(time.RFC3339Nano, *raw)
	return at, err == nil
}

// checkBackup: would the newest backup that includes Home Assistant bring
// this home back? Age, completeness, copies, the schedule; never a restore.
func (c *checkup) checkBackup() {
	var info struct {
		Backups       *[]checkupBackup `json:"backups"`
		AgentErrors   map[string]any   `json:"agent_errors"`
		LastAttempted *string          `json:"last_attempted_automatic_backup"`
		LastCompleted *string          `json:"last_completed_automatic_backup"`
		Next          json.RawMessage  `json:"next_automatic_backup"`
		State         string           `json:"state"`
	}
	err := c.a.s.wsDecode(backupRequest, &info, "the backup list")
	if err == nil && info.Backups == nil {
		err = errCheckupShape
	}
	if err != nil {
		c.gap("backups", err)
		return
	}
	facts := map[string]any{"restore_tested": false}
	c.facts["backup"] = facts
	automatic := map[string]any{"last_completed": nil, "next": nil}
	facts["automatic"] = automatic
	if at, ok := checkupTime(info.LastCompleted); ok {
		automatic["last_completed"] = at.UTC().Format(time.RFC3339)
	}
	var next *string
	switch {
	case len(info.Next) == 0:
		c.gap("backup_schedule", errCheckupMissing)
	case json.Unmarshal(info.Next, &next) != nil:
		c.gap("backup_schedule", errCheckupShape)
	case next == nil:
		c.find("NO_AUTOMATIC_BACKUP", "notice", "backup", nil)
	default:
		if at, ok := checkupTime(next); ok {
			automatic["next"] = at.UTC().Format(time.RFC3339)
		} else {
			c.gap("backup_schedule", errCheckupShape)
		}
	}
	attempted, tried := checkupTime(info.LastAttempted)
	completed, done := checkupTime(info.LastCompleted)
	switch {
	case !slices.Contains(checkupBackupStates, info.State):
		c.gap("backup_state", errCheckupShape)
	case tried && info.State == "idle" && (!done || attempted.After(completed)):
		c.find("BACKUP_LAST_FAILED", "problem", "backup", map[string]any{"attempted": attempted.UTC().Format(time.RFC3339)})
	}
	if info.LastAttempted != nil && !tried || info.LastCompleted != nil && !done {
		c.gap("backup_runs", errCheckupShape)
	}
	problems := map[string]any{}
	if len(info.AgentErrors) > 0 {
		problems["location_unreachable"] = checkupLocations(slices.Sorted(maps.Keys(info.AgentErrors)))
	}
	var newest *checkupBackup
	var newestAt time.Time
	for i, b := range *info.Backups {
		if at, ok := checkupTime(&b.Date); ok && b.HAIncluded && at.After(newestAt) {
			newest, newestAt = &(*info.Backups)[i], at
		}
	}
	if newest == nil {
		c.find("NO_BACKUP", "problem", "backup", nil)
		c.backupProblems(problems)
		return
	}
	age := c.a.x.now.Sub(newestAt)
	agents := slices.Sorted(maps.Keys(newest.Agents))
	protected, size := false, 0.0
	for _, agent := range newest.Agents {
		protected = protected || agent.Protected
		size = max(size, agent.Size)
	}
	facts["newest"] = map[string]any{"date": newestAt.UTC().Format(time.RFC3339), "age_days": round1(age.Hours() / 24),
		"database_included": newest.DBIncluded, "protected": protected, "locations": checkupLocations(agents),
		"off_device_copies": len(slices.DeleteFunc(slices.Clone(agents), func(a string) bool { return slices.Contains(checkupLocal, a) })), "size_mb": round1(size / (1 << 20))}
	if age > checkupBackupMaxAge {
		c.find("NO_RECENT_BACKUP", "problem", "backup", map[string]any{"age_days": round1(age.Hours() / 24)})
	}
	if len(newest.FailedAgents) > 0 {
		problems["failed_copy"] = checkupLocations(newest.FailedAgents)
	}
	if len(newest.FailedApps) > 0 {
		apps, other := []string{}, 0
		for _, app := range newest.FailedApps {
			if name, ok := c.apps[app.Slug]; ok {
				apps = append(apps, name)
			} else {
				other++
			}
		}
		problems["apps_missing"] = map[string]any{"apps": apps, "other": other}
	}
	if len(newest.FailedFolders) > 0 {
		folders := []string{}
		for _, f := range newest.FailedFolders {
			if !slices.Contains(checkupFolders, f) {
				f = "other"
			}
			folders = append(folders, f)
		}
		problems["folders_missing"] = folders
	}
	c.backupProblems(problems)
	if len(agents) == 0 {
		c.gap("backup_copies", errCheckupMissing)
	}
	if len(agents) > 0 && !slices.ContainsFunc(agents, func(a string) bool { return !slices.Contains(checkupLocal, a) }) {
		c.find("BACKUP_ONLY_ON_DEVICE", "notice", "backup", nil)
	}
}

func (c *checkup) backupProblems(problems map[string]any) {
	if len(problems) > 0 {
		c.find("BACKUP_PROBLEM", "problem", "backup", problems)
	}
}

// updateFacts: pending updates by kind (the update advisor's classes); the
// updates skill installs them.
func (c *checkup) updateFacts() {
	byKind := map[string]int{}
	facts := map[string]any{"pending": len(c.a.rows), "by_kind": byKind,
		"running": slices.ContainsFunc(c.a.ready, func(r map[string]any) bool { return r["code"] == "UPDATE_RUNNING" })}
	for _, row := range c.a.rows {
		byKind[row.Kind]++
		if row.Kind == "core" {
			if _, ok1 := haVersion(row.Installed); ok1 {
				if _, ok2 := haVersion(row.Latest); ok2 {
					facts["core"] = map[string]any{"installed": row.Installed, "latest": row.Latest}
				}
			}
		}
	}
	c.facts["updates"] = facts
}
