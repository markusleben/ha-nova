package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// Dashboard plans (dashboard-write spec): a full-config save of a storage
// dashboard (optionally creating its shell), a revert of the last verified
// save, and a dashboard delete. Reads only; apply executes.

// dashboardPathPattern: a dashboard url_path as the CLI addresses it.
var dashboardPathPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

// defaultDashboard is the url_path of the default dashboard; when it is not
// listed (auto-generated, or a core before the default-dashboard migration)
// Home Assistant addresses it by omitting url_path.
const defaultDashboard = "lovelace"

type dashboardEntry struct {
	ID      string `json:"id"`
	URLPath string `json:"url_path"`
	Title   string `json:"title"`
	Mode    string `json:"mode"`
	raw     json.RawMessage
}

type dashboardPlanResult struct {
	Title  string           `json:"title,omitempty"`
	Issues []dashboardIssue `json:"issues,omitempty"`
}

func (req configRequest) validateDashboard() error {
	switch {
	case req.Op == "restore":
		return errors.New("a deleted dashboard comes back through plan write --domain dashboard --create with its snapshot's shell and config")
	case !dashboardPathPattern.MatchString(req.ConfigID):
		return fmt.Errorf("%q is not a dashboard url_path (--id)", req.ConfigID)
	case req.Create != (req.Title != "") || (req.Icon != "" && !req.Create):
		return errors.New("--create needs --title; --title and --icon only go with --create")
	case req.Create && req.Op != "write":
		return errors.New("--create only goes with plan write")
	case req.Create && !strings.Contains(req.ConfigID, "-"):
		return errors.New("a new dashboard's url_path needs a hyphen (-), as Home Assistant requires")
	case req.Icon != "" && !strings.Contains(req.Icon, ":"):
		return fmt.Errorf("%q is not an icon (mdi:name)", req.Icon)
	case req.Op == "write" || req.Op == "revert":
		_, err := configObjectFromBytes(req.Draft)
		return err
	}
	return nil
}

func readDashboards(s *relaySession) ([]dashboardEntry, error) {
	var raws []json.RawMessage
	if err := s.wsDecode(map[string]any{"type": "lovelace/dashboards/list"}, &raws, "the dashboard list"); err != nil {
		return nil, err
	}
	entries := make([]dashboardEntry, 0, len(raws))
	for _, raw := range raws {
		var entry dashboardEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, fmt.Errorf("cannot parse the dashboard list: %w", err)
		}
		entry.raw = raw
		entries = append(entries, entry)
	}
	return entries, nil
}

func findDashboard(entries []dashboardEntry, urlPath string) *dashboardEntry {
	for i := range entries {
		if entries[i].URLPath == urlPath {
			return &entries[i]
		}
	}
	return nil
}

// dashboardConfigPayload addresses a dashboard; the unlisted default omits
// url_path (the field takes a string, an explicit null is refused).
func dashboardConfigPayload(wsType, urlPath string, unlistedDefault bool) map[string]any {
	payload := map[string]any{"type": wsType}
	if !unlistedDefault {
		payload["url_path"] = urlPath
	}
	return payload
}

// readDashboardConfig returns the stored config, nil when the dashboard has
// none yet (config_not_found: a new shell, an auto-generated default).
func readDashboardConfig(s *relaySession, urlPath string, unlistedDefault, fresh bool) (json.RawMessage, error) {
	payload := dashboardConfigPayload("lovelace/config", urlPath, unlistedDefault)
	var data json.RawMessage
	var err error
	if fresh {
		data, err = s.readFresh("ws", payload, relaySessionRequestTimeout)
	} else {
		data, err = s.ws(payload)
	}
	var call *relayCallError
	if errors.As(err, &call) && strings.Contains(call.message, "config_not_found") {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := configObjectFromBytes(data); err != nil {
		return nil, fmt.Errorf("the dashboard config is not a JSON object: %w", err)
	}
	return data, nil
}

func dashboardNotFound(urlPath string, entries []dashboardEntry) error {
	failure := &verbFailure{exit: verbExitNotFound, code: "NOT_FOUND", message: "no dashboard with url_path " + urlPath,
		next: "Pick one of the listed dashboards (url_path), or create a new one with --create --title."}
	for _, entry := range entries {
		failure.candidates = append(failure.candidates, verbTarget{EntityID: entry.URLPath, Name: entry.Title, By: "dashboard"})
	}
	return failure
}

// computeDashboardPlan resolves the dashboard, reads its live config, checks
// the draft, renders the diff, and builds the plan (not stored).
func computeDashboardPlan(paths runtimePaths, s *relaySession, env *verbEnvelope, req configRequest) (*tierEngine, storedPlan, configContext, error) {
	var ctx configContext
	fail := func(err error) (*tierEngine, storedPlan, configContext, error) { return nil, storedPlan{}, ctx, err }
	e := newTierEngine(s)
	var snap undoSnapshot
	if req.Op == "revert" {
		var err error
		if snap, err = selectServerUndoSnapshot(paths, s.server, req.ConfigID, req.Domain); err != nil {
			return fail(refused("NO_UNDO_SNAPSHOT", err.Error()))
		}
		if req.Draft, _, err = exactConfig(snap.BeforeConfig); err != nil {
			return fail(err)
		}
	}
	entries, err := readDashboards(s)
	if err != nil {
		return fail(err)
	}
	entry := findDashboard(entries, req.ConfigID)
	unlistedDefault := entry == nil && req.ConfigID == defaultDashboard && !req.Create
	switch {
	case req.Create && entry != nil:
		return fail(refusedWith("CONFIG_ID_IN_USE", "a dashboard with url_path "+req.ConfigID+" already exists; a create never overwrites",
			"It may be the one an earlier apply created — check `ha-nova get --dashboard "+req.ConfigID+"`; otherwise pick another url_path, or plan write without --create to replace its content."))
	case entry == nil && !req.Create && !unlistedDefault:
		return fail(dashboardNotFound(req.ConfigID, entries))
	case entry != nil && entry.Mode != "storage":
		return fail(refusedWith("DASHBOARD_NOT_STORAGE", "dashboard "+req.ConfigID+" is in "+entry.Mode+" mode: its config lives in a YAML file and is read-only here",
			"Explain that this dashboard is edited in its YAML file (ha-nova:yaml-config), or take a copy into a new storage dashboard with --create."))
	case unlistedDefault && req.Op == "delete":
		return fail(refusedWith("DASHBOARD_NOT_DELETABLE", "the default dashboard is not a listed dashboard and cannot be deleted",
			"Only its content can change: plan write a new config for it instead."))
	}
	var current json.RawMessage
	if !req.Create {
		if current, err = readDashboardConfig(s, req.ConfigID, unlistedDefault, false); err != nil {
			return fail(err)
		}
	}
	if req.Op == "revert" {
		if current == nil {
			return fail(revertDrift(req.ConfigID + " has no stored config any more"))
		} else if matches, err := snapshotMatchesLive(snap, current); err != nil || !matches {
			return fail(revertDrift("dashboard " + req.ConfigID + " changed since its last verified save"))
		}
	}
	ctx.current = current
	action := configAction{Op: req.Op, Domain: "dashboard", ConfigID: req.ConfigID, Path: "lovelace/config/save", Default: unlistedDefault}
	result := dashboardPlanResult{Title: req.Title}
	if entry != nil {
		result.Title = entry.Title
	}
	ctx.alias = result.Title
	if req.Op == "delete" {
		action.Path, action.Shell = "lovelace/dashboards/delete", entry.raw
		e.raise(tierTyped, tierReason{Code: reasonConfigDelete, Via: "dashboard." + req.ConfigID})
	} else {
		action.Body = req.Draft
		if req.Create {
			shell := map[string]any{"url_path": req.ConfigID, "title": req.Title}
			if req.Icon != "" {
				shell["icon"] = req.Icon
			}
			action.Shell, _ = json.Marshal(shell)
		}
		e.raise(tierPreview, tierReason{Code: map[string]string{"write": reasonConfigWrite, "revert": reasonConfigRevert}[req.Op], Via: "dashboard." + req.ConfigID})
		if result.Issues, err = draftIssues(e, current, req.Draft); err != nil {
			return fail(err)
		}
		before, _ := configObjectFromBytes(current)
		if before == nil {
			before = map[string]any{}
		}
		after, _ := configObjectFromBytes(req.Draft)
		env.Diff = dashboardDiff(before, after)
	}
	basis := map[string]any{"current": digestOf(current), "draft": digestOf(req.Draft), "entry": digestOf(json.RawMessage(nil))}
	if entry != nil {
		basis["entry"] = digestOf(entry.raw)
	}
	plan := storedPlan{Kind: req.Op, ConfigRequest: &req, ConfigAction: &action, Basis: basis}
	plan, err = sealPlan(e, env, plan, action)
	shown := action
	shown.Body, shown.Shell = nil, nil // the draft is the caller's file; never echo a whole dashboard
	env.Config, env.Result = &shown, result
	if env.Status == "needs_confirmation" {
		env.Say = confirmationSay(e, env, plan, result.Title)
	}
	return e, plan, ctx, err
}

func digestOf(raw json.RawMessage) string {
	sum := sha256.Sum256([]byte(canonicalJSON(raw)))
	return hex.EncodeToString(sum[:])
}

// draftIssues checks the draft against the live home: an entity the draft
// adds that Home Assistant does not know at all is refused (a guessed id);
// everything else is reported, never blocking.
func draftIssues(e *tierEngine, current, draft json.RawMessage) ([]dashboardIssue, error) {
	states, err := loadStates(e.s)
	if err != nil {
		return nil, err
	}
	rows, err := e.fullRegistry()
	if err != nil {
		return nil, err
	}
	resources, err := readResources(e.s)
	if err != nil {
		return nil, err
	}
	known, knownTypes := map[string]bool{}, map[string]bool{}
	if cfg, _ := configObjectFromBytes(current); cfg != nil {
		dashboardRefs(cfg, false, known, knownTypes)
	}
	cfg, err := configObjectFromBytes(draft)
	if err != nil {
		return nil, err
	}
	issues := dashboardIssues(cfg, presenceFrom(states, rows), resources)
	unknown := map[string]bool{}
	for i, issue := range issues {
		issues[i].New = (issue.EntityID != "" && !known[issue.EntityID]) || (issue.CardType != "" && !knownTypes[issue.CardType])
		if issues[i].New && issue.Detail == "not in Home Assistant" {
			unknown[issue.EntityID] = true
		}
	}
	if len(unknown) > 0 {
		return nil, refusedWith("UNKNOWN_ENTITY", "the draft adds entities Home Assistant does not know: "+strings.Join(slices.Sorted(maps.Keys(unknown)), ", "),
			"Resolve each one with `ha-nova find`, fix the draft, and plan again; never guess an entity_id.")
	}
	return issues, nil
}
