package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"maps"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"
)

// ha-nova map --section checkup (docs/work/2026-11-12-system-checkup-spec.md):
// is this Home Assistant OK, why is it slow, would its backup work. Every
// verdict is a fixed rule here. Every string in the result is a fixed code, a
// checked domain or enum word, a number, a time HA NOVA formats, or an App or
// integration name through sayText: no error text, reference, entry title,
// backup name, or location label from Home Assistant passes.

const (
	checkupBackupMaxAge = 7 * 24 * time.Hour
	checkupDiskFloorGB  = 2.0 // Supervisor refuses updates and backups below it
	checkupStatsApps    = 40
	checkupMemoryHigh   = 90.0
)

var (
	checkupWord       = regexp.MustCompile(`^[a-z_]{1,64}$`)
	checkupFolders    = []string{"share", "addons/local", "ssl", "media"}
	checkupLocal      = []string{"backup.local", "hassio.local"}
	errCheckupShape   = errors.New("unknown shape")
	errCheckupMissing = errors.New("not reported")
	errCheckupMany    = errors.New("too many Apps")
)

type checkup struct {
	a        *updateAdvisor
	findings []map[string]any
	gaps     []map[string]any
	facts    map[string]any
	apps     map[string]string // slug → name, from the Supervisor's App list
	started  []string
	fixable  map[string]bool // hassio repair issue_id → is_fixable
	listed   map[string]bool // hassio repairs shown under SYSTEM_ISSUES
	repairs  []repairIssue
	repairsE error
}

func (c *checkup) find(code, level, area string, facts map[string]any) {
	row := map[string]any{"code": code, "level": level, "area": area}
	maps.Copy(row, facts)
	c.findings = append(c.findings, row)
}

func (c *checkup) gap(check string, err error) {
	c.gaps = append(c.gaps, map[string]any{"check": check, "reason": checkupReason(err)})
}

// checkupReason: a fixed word for a failed read, never its message (Home
// Assistant's or the Supervisor's text could quote anything).
func checkupReason(err error) string {
	var call *relayCallError
	switch {
	case errors.Is(err, errSupervisorViaCloud):
		return "VIA_HOME_ASSISTANT_LINK"
	case errors.Is(err, errCheckupShape):
		return "UNKNOWN_SHAPE"
	case errors.Is(err, errCheckupMissing):
		return "NOT_REPORTED"
	case errors.Is(err, errCheckupMany):
		return "TOO_MANY_APPS"
	case errors.As(err, &call) && logFactsCode.MatchString(call.code):
		return call.code
	}
	return "READ_FAILED"
}

func checkupHome(s *relaySession, x *homeIndex) (map[string]any, error) {
	var logs json.RawMessage
	var logsErr error
	prefetch([]func(){
		func() { _, _, _ = s.coreGET("/api/config") },
		func() { _, _ = s.ws(repairsRequest) },
		func() { _, _ = s.ws(backupRequest) },
		func() { _, _ = s.ws(manifestList) },
		func() {
			if logsErr = mcpDemoRefusal(s.cfg.demoToken != "", "ws", []byte(logFactsBody)); logsErr == nil {
				logs, logsErr = readLogFacts(s)
			}
		},
	})
	a := &updateAdvisor{s: s, x: x, domains: map[string]bool{}, ready: []map[string]any{}, gaps: []map[string]any{}}
	if err := a.loadPending(); err != nil {
		return nil, err
	}
	c := &checkup{a: a, findings: []map[string]any{}, gaps: []map[string]any{}, facts: map[string]any{}, apps: map[string]string{}, fixable: map[string]bool{}, listed: map[string]bool{}}
	c.repairs, c.repairsE = a.repairs()
	for _, issue := range c.repairs {
		if issue.Domain == "hassio" {
			c.fixable[issue.IssueID] = issue.IsFixable
		}
	}
	c.facts["supervisor"] = "not_applicable"
	if a.hassio {
		c.facts["supervisor"] = "running"
		prefetch([]func(){
			func() { _, _ = s.read("supervisor", supervisorGet("/resolution/info")) },
			func() { _, _ = s.read("supervisor", supervisorGet("/host/info")) },
			func() { _, _ = s.read("supervisor", supervisorGet("/addons")) },
		})
		c.readApps()
		c.checkSystem()
		c.checkDisk()
		c.checkResources()
	}
	c.checkIntegrations(logs, logsErr)
	c.checkRepairs()
	c.checkBackup()
	c.updateFacts()
	status := "ok"
	if len(c.gaps) > 0 {
		status = "limited"
	}
	if slices.ContainsFunc(c.findings, func(f map[string]any) bool { return f["level"] == "problem" }) {
		status = "attention"
	}
	return map[string]any{"section": "checkup", "status": status, "checked_at": x.now.UTC().Format(time.RFC3339),
		"findings": c.findings, "not_checked": c.gaps, "facts": c.facts}, nil
}

// readApps: the installed Apps' names (sayText) and which run.
func (c *checkup) readApps() {
	var list struct {
		Addons *[]struct {
			Name  string `json:"name"`
			Slug  string `json:"slug"`
			State string `json:"state"`
		} `json:"addons"`
	}
	err := c.a.supervisorData("/addons", &list)
	if err == nil && list.Addons == nil {
		err = errCheckupShape
	}
	if err != nil {
		c.gap("apps", err)
		return
	}
	for _, app := range *list.Addons {
		if kind, slug := rawSupervisorMatch("GET", "/addons/"+app.Slug+"/stats"); kind != "read" || slug != app.Slug {
			continue
		}
		c.apps[app.Slug] = sayText(firstNonEmpty(app.Name, app.Slug), false)
		if app.State == "started" {
			c.started = append(c.started, app.Slug)
		}
	}
	slices.Sort(c.started)
}

// checkSystem: Supervisor's unhealthy reasons and its issues; an issue Home
// Assistant shows as a fixable repair is fixed through the health skill's
// repair flow (with its confirmation), any other one in Home Assistant.
func (c *checkup) checkSystem() {
	var res struct {
		Unhealthy *[]string          `json:"unhealthy"`
		Issues    *[]resolutionIssue `json:"issues"`
	}
	err := c.a.supervisorData("/resolution/info", &res)
	if err == nil && (res.Unhealthy == nil || res.Issues == nil) {
		err = errCheckupShape
	}
	if err != nil {
		c.gap("system", err)
		return
	}
	if len(*res.Unhealthy) > 0 {
		c.find("SUPERVISOR_UNHEALTHY", "problem", "system", map[string]any{"reasons": checkupWords(*res.Unhealthy)})
	}
	rows := []map[string]any{}
	for _, issue := range *res.Issues {
		row := map[string]any{"type": checkupWords([]string{issue.Type})[0], "context": checkupWords([]string{issue.Context})[0], "fix": "ui"}
		if name, ok := c.apps[issue.Reference]; ok && issue.Context == "addon" {
			row["app"] = name
		}
		id := strings.ToLower(strings.ReplaceAll(issue.UUID, "-", ""))
		if fixable, mirrored := c.fixable[id]; mirrored && id != "" {
			c.listed[id] = true
			if fixable {
				row["fix"] = "repair"
			}
		}
		rows = append(rows, row)
	}
	if len(rows) > 0 {
		c.find("SYSTEM_ISSUES", "problem", "system", map[string]any{"count": len(rows), "issues": rows[:min(len(rows), 20)]})
	}
}

// checkupWords: plain enum words pass; anything else is "other".
func checkupWords(words []string) []string {
	out := []string{}
	for _, w := range words {
		if checkupWord.MatchString(w) {
			out = append(out, w)
		} else {
			out = append(out, "other")
		}
	}
	return out
}

func (c *checkup) checkDisk() {
	var host struct {
		Free  *float64 `json:"disk_free"`
		Total *float64 `json:"disk_total"`
	}
	err := c.a.supervisorData("/host/info", &host)
	if err == nil && (host.Free == nil || host.Total == nil || *host.Total <= 0) {
		err = errCheckupMissing
	}
	if err != nil {
		c.gap("disk", err)
		return
	}
	free, total := *host.Free, *host.Total
	c.facts["disk"] = map[string]any{"free_gb": round1(free), "total_gb": round1(total), "used_percent": round1(100 - free/total*100)}
	switch {
	case free < checkupDiskFloorGB:
		c.find("DISK_LOW", "problem", "disk", map[string]any{"free_gb": round1(free)})
	case free < total/10:
		c.find("DISK_LOW", "notice", "disk", map[string]any{"free_gb": round1(free), "free_percent": round1(free / total * 100)})
	}
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

type checkupUsage struct {
	Name   string   `json:"name"`
	Kind   string   `json:"kind"`
	CPU    *float64 `json:"cpu_percent,omitempty"`
	Memory float64  `json:"memory_mb"`
	limit  float64
}

// checkResources: CPU and memory of Home Assistant, the Supervisor, and the
// running Apps, as the Supervisor measures them at this moment. The memory
// share is computed only when every container reports the same limit (none
// has its own, so it is the machine's memory).
func (c *checkup) checkResources() {
	type source struct{ path, name, kind string }
	sources := []source{{"/core/stats", "Home Assistant", "core"}, {"/supervisor/stats", "Supervisor", "supervisor"}}
	for _, slug := range c.started[:min(len(c.started), checkupStatsApps)] {
		sources = append(sources, source{"/addons/" + slug + "/stats", c.apps[slug], "app"})
	}
	var reads []func()
	for _, src := range sources {
		reads = append(reads, func() { _, _ = c.a.s.read("supervisor", supervisorGet(src.path)) })
	}
	prefetch(reads)
	usage := []checkupUsage{}
	for _, src := range sources {
		var st struct {
			CPU   *float64 `json:"cpu_percent"`
			Usage *float64 `json:"memory_usage"`
			Limit *float64 `json:"memory_limit"`
		}
		err := c.a.supervisorData(src.path, &st)
		if err == nil && (st.Usage == nil || st.Limit == nil) {
			err = errCheckupMissing
		}
		if err != nil {
			c.gap("resources", err)
			return
		}
		row := checkupUsage{Name: src.name, Kind: src.kind, Memory: round1(*st.Usage / (1 << 20)), limit: *st.Limit}
		if st.CPU != nil {
			cpu := round1(*st.CPU)
			row.CPU = &cpu
		}
		usage = append(usage, row)
	}
	if len(c.started) > checkupStatsApps {
		c.gap("resources", errCheckupMany)
	}
	facts := map[string]any{}
	var cpu, memory float64
	cpuKnown, sameLimit := true, usage[0].limit > 0
	for _, u := range usage {
		memory += u.Memory
		cpuKnown = cpuKnown && u.CPU != nil
		if u.CPU != nil {
			cpu += *u.CPU
		}
		sameLimit = sameLimit && u.limit == usage[0].limit
	}
	facts["memory_mb"] = round1(memory)
	top := func(by func(a, b checkupUsage) int) []checkupUsage {
		sorted := slices.Clone(usage)
		slices.SortStableFunc(sorted, by)
		return sorted[:min(len(sorted), 3)]
	}
	facts["top_memory"] = top(func(a, b checkupUsage) int { return cmp.Compare(b.Memory, a.Memory) })
	if cpuKnown {
		facts["cpu_percent"] = round1(cpu)
		facts["top_cpu"] = top(func(a, b checkupUsage) int { return cmp.Compare(*b.CPU, *a.CPU) })
	} else {
		c.gap("cpu", errCheckupMissing)
	}
	if sameLimit {
		share := round1(memory * (1 << 20) / usage[0].limit * 100)
		facts["memory_percent"] = share
		if share >= checkupMemoryHigh {
			c.find("HIGH_MEMORY", "notice", "resources", map[string]any{"memory_percent": share})
		}
	}
	c.facts["resources"] = facts
}
