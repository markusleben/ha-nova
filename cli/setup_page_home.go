package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// Step 4 of the setup page, "Here's your home, at a glance"
// (docs/work/2026-10-18-onboarding-0.27-spec.md §4, §7.5, contract §7.6).
// The data comes from the typed verbs: `map --insights` reads; `plan rooms`
// and `plan template <id>` store preview plans; `apply --plan <id>` saves
// one. The page shows exactly a stored plan's preview, and a save posts
// that plan's id, which must be one this setup run stored for the slot on
// screen. Nothing changes in the home without that click.

type setupPageHome struct {
	Loading    bool                         `json:"loading,omitempty"`
	Failed     bool                         `json:"failed,omitempty"` // the home couldn't be read
	Stats      map[string]int               `json:"stats,omitempty"`
	Insights   []homeInsight                `json:"insights,omitempty"` // the first 3
	Automation *templateMatch               `json:"automation,omitempty"`
	Previews   map[string]*setupPagePreview `json:"previews,omitempty"` // by slot: rooms, reminder, automation
	Notes      []*setupPageMsg              `json:"notes,omitempty"`    // what step 3 did
	Claude     bool                         `json:"claude,omitempty"`   // offer "Open Claude Code"
	Opened     string                       `json:"opened,omitempty"`   // terminal (Claude Code opened), manual (type it yourself)
}

// setupPagePreview is one plan on the page: loading, preview (PlanID can be
// saved), saved, or problem.
type setupPagePreview struct {
	State    string        `json:"state"`
	Kind     string        `json:"kind,omitempty"` // rooms, undo, template
	PlanID   string        `json:"plan_id,omitempty"`
	Changes  []roomsChange `json:"changes,omitempty"`
	Template string        `json:"template,omitempty"`
	Uses     []templateUse `json:"uses,omitempty"`
	Draft    string        `json:"draft,omitempty"` // the exact automation the save stores
	Problem  *setupPageMsg `json:"problem,omitempty"`
	Notice   *setupPageMsg `json:"notice,omitempty"`
	Undo     bool          `json:"undo,omitempty"`    // "Undo" can be planned
	Evicted  []string      `json:"evicted,omitempty"` // older checkpoints the save pushed off
	undoID   string        // the rooms undo record; never leaves the CLI
}

// Test seam: opens a terminal running Claude Code (setup_page_terminal.go).
var setupPageOpenClaude = openClaudeTerminal

// pageHome shows step 4 until Done, Ctrl+C, or setupPageIdle without a click.
func (f *setupFlow) pageHome() {
	p := f.page
	home := &setupPageHome{Loading: true, Notes: f.pageNotes, Previews: map[string]*setupPagePreview{},
		Claude: slices.Contains(f.selectedClients, "claude")}
	show := func() {
		view := *home
		view.Previews = map[string]*setupPagePreview{}
		for slot, pv := range home.Previews {
			copied := *pv
			view.Previews[slot] = &copied
		}
		p.update(func(v *setupPageState) { v.Screen, v.Home, v.Control = "home", &view, nil })
	}
	show()
	f.pageLoadHome(home)
	for {
		show()
		accepts := []string{"preview", "save", "undo", "done"}
		if home.Claude {
			accepts = append(accepts, "claude")
		}
		action, err := p.wait(accepts...)
		if err != nil || action.Action == "done" {
			return
		}
		switch action.Action {
		case "preview":
			if args := home.previewArgs(action.Value); args != nil {
				home.Previews[action.Value] = &setupPagePreview{State: "loading"}
				show()
				home.Previews[action.Value] = pagePreview(args...)
			}
		case "save":
			// Only a plan this run stored and shows right now; each once.
			for slot, pv := range home.Previews {
				if pv.State == "preview" && pv.PlanID != "" && pv.PlanID == action.Value {
					pv.State = "saving"
					show()
					home.Previews[slot] = f.pageSave(slot, pv, home)
					break
				}
			}
		case "undo":
			if pv := home.Previews[action.Value]; pv != nil && pv.undoID != "" {
				undoID := pv.undoID
				pv.State, pv.Undo, pv.undoID = "loading", false, ""
				show()
				next := pagePreview("plan", "revert", "--target", undoID)
				if next.State == "preview" {
					next.Kind = "undo"
				} else if next.Problem == nil {
					next.Problem = setupPageText("setup.page.home.problem.generic")
				}
				home.Previews[action.Value] = next
			}
		case "claude":
			home.Opened = "manual"
			if setupPageOpenClaude() == nil {
				home.Opened = "terminal"
			}
		}
	}
}

// pageLoadHome reads the stats, insights, and the automation made for this
// home.
func (f *setupFlow) pageLoadHome(home *setupPageHome) {
	home.Loading = false
	env, exit, err := setupPageVerb("map", "--insights")
	var result insightsResult
	if err != nil || exit != 0 || json.Unmarshal(env.Result, &result) != nil || result.Stats == nil {
		home.Failed = true
		return
	}
	home.Stats = result.Stats
	for i, in := range result.Insights {
		if i == 3 {
			break
		}
		in.Name, in.Area, in.Value, in.Suggestions = terminalSafeName(in.Name), terminalSafeName(in.Area), terminalSafeName(in.Value), nil
		home.Insights = append(home.Insights, in)
	}
	if m := result.Automation; m != nil && slices.Contains(automationTemplates, m.Template) {
		m.Uses = safeTemplateUses(m.Uses)
		home.Automation = m
	}
}

// previewArgs: the plan command behind a slot the page shows, or nil.
func (home *setupPageHome) previewArgs(slot string) []string {
	for _, in := range home.Insights {
		switch {
		case slot == "rooms" && in.Kind == "no_room" && in.Action == "rooms":
			return []string{"plan", "rooms"}
		case slot == "reminder" && in.Kind == "battery_low" && in.Action == templateBatteryReminder:
			return []string{"plan", "template", templateBatteryReminder}
		}
	}
	if slot == "automation" && home.Automation != nil {
		return []string{"plan", "template", home.Automation.Template}
	}
	return nil
}

// pagePreview stores a preview plan and turns its envelope into what the
// page shows. Only a preview-tier plan is ever offered.
func pagePreview(args ...string) *setupPagePreview {
	env, exit, err := setupPageVerb(args...)
	pv := &setupPagePreview{State: "problem"}
	switch {
	case err != nil:
		pv.Problem = setupPageText("setup.page.home.problem.generic")
		return pv
	case env.Error != nil:
		pv.Problem = pageRefusal(env.Error.Code)
		return pv
	case exit != verbExitConfirmation || env.Plan == nil || env.Tier != "preview" || env.Plan.Confirm != "natural":
		pv.Problem = setupPageText("setup.page.home.problem.generic")
		return pv
	}
	pv.State, pv.PlanID = "preview", env.Plan.ID
	if env.Config != nil { // a template: an automation create
		var m templateMatch
		if json.Unmarshal(env.Result, &m) != nil || len(env.Config.Body) == 0 {
			return &setupPagePreview{State: "problem", Problem: setupPageText("setup.page.home.problem.generic")}
		}
		pv.Kind, pv.Template, pv.Uses, pv.Draft = "template", m.Template, safeTemplateUses(m.Uses), pageDraft(env.Config.Body)
		return pv
	}
	var rooms roomsResult
	if json.Unmarshal(env.Result, &rooms) != nil || len(rooms.Changes) == 0 {
		return &setupPagePreview{State: "problem", Problem: setupPageText("setup.page.home.problem.generic")}
	}
	pv.Kind, pv.Changes = "rooms", safeRoomsChanges(rooms.Changes)
	return pv
}

// pageSave applies the shown plan by its id, the normal apply path.
func (f *setupFlow) pageSave(slot string, shown *setupPagePreview, home *setupPageHome) *setupPagePreview {
	env, exit, err := setupPageVerb("apply", "--plan", shown.PlanID)
	pv := &setupPagePreview{State: "problem", Kind: shown.Kind, Changes: shown.Changes, Template: shown.Template, Uses: shown.Uses}
	if env.Undo != nil && env.Undo.TargetID != "" && shown.Kind == "rooms" {
		pv.undoID, pv.Undo = env.Undo.TargetID, true
		for _, gone := range env.Undo.Evicted {
			pv.Evicted = append(pv.Evicted, terminalSafeName(cmp.Or(gone.Name, gone.TargetID)))
		}
	}
	switch {
	case err != nil:
		pv.Problem = setupPageText("setup.page.home.problem.unknown")
	case exit == verbExitOK && env.Status == "verified":
		pv.State = "saved"
		var rooms roomsResult
		if shown.Kind != "template" && json.Unmarshal(env.Result, &rooms) == nil && len(rooms.Changes) > 0 {
			pv.Changes = safeRoomsChanges(rooms.Changes)
		}
	case env.Status == "drift":
		// The home changed since the preview: show the new one; the old
		// plan is never saved unseen.
		again := &setupPagePreview{State: "problem"}
		if args := home.previewArgs(slot); shown.Kind != "undo" && args != nil {
			again = pagePreview(args...)
		}
		if again.State == "preview" {
			again.Notice = setupPageText("setup.page.home.drift")
		} else if again.Problem == nil {
			again.Problem = setupPageText("setup.page.home.problem.drift")
		}
		return again
	case exit == verbExitUnknown:
		pv.Problem = setupPageText("setup.page.home.problem.unknown")
	case env.Error != nil:
		pv.Problem = pageRefusal(env.Error.Code)
	default: // saved, but the read-back didn't match
		pv.State, pv.Problem = "saved", setupPageText("setup.page.home.problem.unverified")
	}
	if shown.Kind == "undo" {
		pv.Undo, pv.undoID, pv.Evicted = false, "", nil // an undo of an undo is a new suggestion
	}
	return pv
}

// pageRefusal: one plain line per refusal code (spec §7.6).
func pageRefusal(code string) *setupPageMsg {
	switch code {
	case "NO_ROOM_SUGGESTIONS":
		return setupPageText("setup.page.home.problem.no_rooms")
	case "TEMPLATE_NO_MATCH", "TEMPLATE_NOT_OFFERED":
		return setupPageText("setup.page.home.problem.not_offered")
	case "ROOMS_NOT_SAVED":
		return setupPageText("setup.page.home.problem.rooms_not_saved")
	case "ROOMS_PARTIAL":
		return setupPageText("setup.page.home.problem.rooms_partial")
	case "NOTHING_TO_UNDO":
		return setupPageText("setup.page.home.problem.nothing_to_undo")
	case "PLAN_EXPIRED", "PLAN_NOT_FOUND", "PLAN_CLAIMED", "PLAN_INVALIDATED":
		return setupPageText("setup.page.home.problem.expired")
	}
	return setupPageText("setup.page.home.problem.generic")
}

func safeRoomsChanges(changes []roomsChange) []roomsChange {
	out := slices.Clone(changes)
	for i := range out {
		out[i].Name, out[i].From, out[i].Area = terminalSafeName(out[i].Name), terminalSafeName(out[i].From), terminalSafeName(out[i].Area)
	}
	return out
}

func safeTemplateUses(uses []templateUse) []templateUse {
	out := slices.Clone(uses)
	for i := range out {
		out[i].Name = terminalSafeName(out[i].Name)
	}
	return out
}

// pageDraft shows the exact body the save stores, indented; invisible
// format characters (a bidi override in a name) show as \u escapes.
func pageDraft(body json.RawMessage) string {
	var pretty bytes.Buffer
	if json.Indent(&pretty, body, "", "  ") != nil {
		pretty.Reset()
		pretty.Write(body)
	}
	var out strings.Builder
	for _, r := range pretty.String() {
		if r != '\n' && (unicode.IsControl(r) || unicode.Is(unicode.Cf, r)) {
			fmt.Fprintf(&out, `\u%04X`, r)
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}
