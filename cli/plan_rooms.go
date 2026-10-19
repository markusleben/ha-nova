package main

import (
	"cmp"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Rooms plans (onboarding 0.27 spec §7.5): put devices in existing areas,
// previewed first, applied once by plan id. Apply writes each device's area
// in plan order and, when one write fails, rolls the earlier ones back to
// the area the plan recorded for them. The undo record goes on the undo
// stack, so "ha-nova plan revert" restores the previous areas.

const planRoomsUsage = "ha-nova plan rooms"

const (
	roomsUndoDomain   = "device_area"
	deviceAreaRequest = "config/device_registry/update"
)

var roomsRecordPattern = regexp.MustCompile(`^rooms-[0-9a-f]{12}$`)

// roomsItem: one device's area change; "" is no area (an undo can go back
// to none). From is what the device must still have when apply runs.
type roomsItem struct {
	DeviceID string `json:"device_id"`
	From     string `json:"from"`
	To       string `json:"to"`
}

// roomsRequest is what apply recomputes a rooms plan from. Record is the
// undo record's target_id; Undo marks the way back of that record.
type roomsRequest struct {
	Items  []roomsItem `json:"items"`
	Record string      `json:"record"`
	Undo   bool        `json:"undo,omitempty"`
}

func (r roomsRequest) validate() error {
	if len(r.Items) == 0 || len(r.Items) > roomsPlanMaxItems || !roomsRecordPattern.MatchString(r.Record) {
		return errors.New("a rooms plan holds 1 to 25 devices and its undo record id")
	}
	seen := map[string]bool{}
	for _, it := range r.Items {
		if it.DeviceID == "" || seen[it.DeviceID] || it.From == it.To {
			return errors.New("a rooms plan changes each device once, to a different area")
		}
		seen[it.DeviceID] = true
	}
	return nil
}

// roomsChange is one previewed (and, after apply, saved) row.
type roomsChange struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
	FromID   string `json:"from_area_id,omitempty"`
	From     string `json:"from_area,omitempty"`
	AreaID   string `json:"area_id,omitempty"`
	Area     string `json:"area,omitempty"`
}

type roomsResult struct {
	Changes []roomsChange `json:"changes"`
}

func runPlanRooms(paths runtimePaths, args []string) int {
	var opts verbOptions
	fs := flag.NewFlagSet("plan rooms", flag.ContinueOnError)
	addVerbFlags(fs, &opts)
	positional, err := parseVerbArgs(fs, &opts, args, planRoomsUsage+"\n\nRead-only: plans the confident room suggestions of ha-nova map --insights (up to 25 devices, existing rooms only); \"ha-nova apply\" saves them all or none. Undo: ha-nova plan revert --target <the result's undo.target_id>.")
	if err == nil && len(positional) != 0 {
		err = errors.New("Usage: " + planRoomsUsage)
	}
	if err != nil {
		return verbUsageError("plan", err)
	}
	return runReadVerb(paths, "plan", opts, func(s *relaySession, env *verbEnvelope) error {
		home, err := loadHome(s)
		if err != nil {
			return err
		}
		suggestions := indexHome(home, time.Now()).roomSuggestions()
		if len(suggestions) == 0 {
			return refusedWith("NO_ROOM_SUGGESTIONS", "no device without a room has a confident room suggestion", "Don't offer room suggestions; nothing to save.")
		}
		req := roomsRequest{}
		for _, sug := range suggestions {
			req.Items = append(req.Items, roomsItem{DeviceID: sug.DeviceID, To: sug.AreaID})
		}
		return planRooms(paths, s, env, req)
	})
}

// planRooms seals and stores a rooms plan; a new one draws its undo
// record id.
func planRooms(paths runtimePaths, s *relaySession, env *verbEnvelope, req roomsRequest) error {
	if req.Record == "" {
		id := make([]byte, 6)
		if _, err := rand.Read(id); err != nil {
			return fmt.Errorf("cannot draw a rooms record id: %w", err)
		}
		req.Record = "rooms-" + hex.EncodeToString(id)
	}
	_, plan, err := computeRoomsPlan(s, env, req)
	if err != nil {
		return err
	}
	return savePlan(paths, plan, s.transport.Credential)
}

// planRoomsUndo plans the way back of a rooms undo record: every device to
// the area it had before, while it still has the area the record left.
func planRoomsUndo(paths runtimePaths, s *relaySession, env *verbEnvelope, snap undoSnapshot) error {
	var before, after map[string]string
	if json.Unmarshal(snap.BeforeConfig, &before) != nil || json.Unmarshal(snap.ExpectedAfter, &after) != nil {
		return refused("NO_UNDO_SNAPSHOT", "the rooms undo record cannot be read")
	}
	if snap.Domain != roomsUndoDomain || !roomsRecordPattern.MatchString(snap.TargetID) {
		return refused("NO_UNDO_SNAPSHOT", "the rooms undo record is not one HA NOVA saved")
	}
	reg, err := readRoomsRegistry(s, false)
	if err != nil {
		return err
	}
	req := roomsRequest{Record: snap.TargetID, Undo: true}
	for device, to := range before {
		if d, seen := reg.devices[device]; seen && d.area == to {
			continue // already back (a late write, or moved by hand): nothing to undo
		}
		req.Items = append(req.Items, roomsItem{DeviceID: device, From: after[device], To: to})
	}
	if len(req.Items) == 0 {
		return refusedWith("NOTHING_TO_UNDO", "every device of this room change is already back in its previous room", "Tell the user nothing needs undoing.")
	}
	sort.Slice(req.Items, func(i, j int) bool { return req.Items[i].DeviceID < req.Items[j].DeviceID })
	if err := req.validate(); err != nil {
		return refused("NO_UNDO_SNAPSHOT", "the rooms undo record is not valid: "+err.Error())
	}
	return planRooms(paths, s, env, req)
}

type roomsRegistry struct {
	areas     map[string]string
	devices   map[string]struct{ area, name, disabled string }
	effective map[string]string // own area, else the nearest parent's
	parent    map[string]string
}

// readRoomsRegistry reads the area and device registries; fresh bypasses
// the session memo (the read-back after the writes).
func readRoomsRegistry(s *relaySession, fresh bool) (roomsRegistry, error) {
	var areas []struct {
		AreaID string `json:"area_id"`
		Name   string `json:"name"`
	}
	var devices []struct {
		ID         string `json:"id"`
		AreaID     string `json:"area_id"`
		Name       string `json:"name"`
		NameByUser string `json:"name_by_user"`
		DisabledBy string `json:"disabled_by"`
		Parent     string `json:"parent_device_id"`
	}
	r := roomsRegistry{areas: map[string]string{}, devices: map[string]struct{ area, name, disabled string }{}}
	for _, read := range []struct {
		request string
		into    any
	}{{"config/area_registry/list", &areas}, {"config/device_registry/list", &devices}} {
		payload := map[string]any{"type": read.request}
		var raw json.RawMessage
		var err error
		if fresh {
			raw, err = s.readFresh("ws", payload, relaySessionRequestTimeout)
		} else {
			raw, err = s.ws(payload)
		}
		if err == nil {
			err = json.Unmarshal(raw, read.into)
		}
		if err != nil {
			return r, fmt.Errorf("cannot read %s: %w", read.request, err)
		}
	}
	for _, a := range areas {
		r.areas[a.AreaID] = a.Name
	}
	own, parent := map[string]string{}, map[string]string{}
	for _, d := range devices {
		r.devices[d.ID] = struct{ area, name, disabled string }{d.AreaID, firstNonEmpty(d.NameByUser, d.Name, d.ID), d.DisabledBy}
		own[d.ID], parent[d.ID] = d.AreaID, d.Parent
	}
	r.effective, r.parent = effectiveDeviceAreas(own, parent), parent
	return r, nil
}

// computeRoomsPlan checks every item against the live registries and seals
// the plan (preview tier). Names and area names are in the basis, so a
// rename after the preview is drift.
func computeRoomsPlan(s *relaySession, env *verbEnvelope, req roomsRequest) (*tierEngine, storedPlan, error) {
	if err := req.validate(); err != nil {
		return nil, storedPlan{}, refused("PLAN_INVALID", err.Error())
	}
	reg, err := readRoomsRegistry(s, false)
	if err != nil {
		return nil, storedPlan{}, err
	}
	names, areaNames, effective := map[string]string{}, map[string]string{}, map[string]string{}
	result := roomsResult{}
	for _, it := range req.Items {
		d, ok := reg.devices[it.DeviceID]
		switch {
		case !ok || d.disabled != "":
			return nil, storedPlan{}, &verbFailure{exit: verbExitNotFound, code: "DEVICE_NOT_FOUND", message: "device " + it.DeviceID + " is gone or disabled; nothing was planned", next: roomsAgain(req)}
		case d.area != it.From:
			return nil, storedPlan{}, &verbFailure{exit: verbExitDrift, status: "drift", code: "ROOMS_CHANGED",
				message: sayText(d.name, true) + " is in a different room than the plan expects; nothing was sent", next: roomsAgain(req)}
		case it.To != "" && reg.areas[it.To] == "":
			return nil, storedPlan{}, refusedWith("AREA_NOT_FOUND", "a room in this plan no longer exists", roomsAgain(req))
		}
		// Where it ends up: no own area means its parent's room, if any.
		lands := it.To
		if lands == "" {
			lands = reg.effective[reg.parent[it.DeviceID]]
		}
		names[it.DeviceID], areaNames[it.From], areaNames[lands] = d.name, reg.areas[it.From], reg.areas[lands]
		effective[it.DeviceID] = reg.effective[it.DeviceID] + ">" + lands // a parent moved since the preview is drift too
		result.Changes = append(result.Changes, roomsChange{DeviceID: it.DeviceID, Name: d.name, FromID: it.From, From: reg.areas[it.From], AreaID: lands, Area: reg.areas[lands]})
	}
	e := newTierEngine(s)
	e.raise(tierPreview, tierReason{Code: reasonAreaAssign, Via: roomsUndoDomain})
	plan := storedPlan{Kind: "rooms", RoomsRequest: &req, Basis: map[string]any{"names": names, "areas": areaNames, "effective": effective}}
	plan, err = sealPlan(e, env, plan, req)
	env.Result = result
	env.Say = roomsSay(result.Changes, req.Undo) + " Nothing changes until you say so. Save these rooms?"
	return e, plan, err
}

// roomsAgain: what to do when a device changed since the preview.
func roomsAgain(req roomsRequest) string {
	if req.Undo {
		return "Nothing was reverted: a device changed since. Tell the user; they can move devices in Home Assistant's own device settings."
	}
	return "Nothing was sent. Plan again with ha-nova plan rooms, show its preview, and get confirmation again."
}

// roomsSay: Kitchen ← “A” and “B”; Bedroom ← “C”; no room ← “D”.
func roomsSay(changes []roomsChange, undo bool) string {
	var order []string
	by := map[string][]string{}
	for _, c := range changes {
		room := sayText(c.Area, false)
		if c.AreaID == "" {
			room = "no room"
		}
		if by[room] == nil {
			order = append(order, room)
		}
		by[room] = append(by[room], sayText(c.Name, true))
	}
	var parts []string
	for _, room := range order {
		parts = append(parts, room+" ← "+sayList(by[room]))
	}
	if undo {
		return fmt.Sprintf("Put %s back in their previous rooms: %s.", plural(len(changes), "device"), strings.Join(parts, "; "))
	}
	return fmt.Sprintf("Put %s in rooms: %s.", plural(len(changes), "device"), strings.Join(parts, "; "))
}

func deviceAreaWrite(s *relaySession, device, area string) error {
	payload := map[string]any{"type": deviceAreaRequest, "device_id": device, "area_id": nil}
	if area != "" {
		payload["area_id"] = area
	}
	_, err := s.write("ws", payload)
	return err
}

// executeRooms writes the items in order. A failed write rolls back every
// earlier one (and the failed one when its outcome is unknown), newest
// first, to its recorded From; then a fresh read decides what the result
// says (failRooms).
func executeRooms(paths runtimePaths, s *relaySession, env *verbEnvelope, req roomsRequest, changes []roomsChange) (err error) {
	sent := false
	defer afterSend(&sent, env, &err)
	s.renewBudget()
	for i, it := range req.Items {
		sent = true
		if werr := deviceAreaWrite(s, it.DeviceID, it.To); werr != nil {
			// back: devices known to be on their From (a rejected write, a
			// rollback Home Assistant confirmed); failRooms reads the rest.
			back, stuck := map[string]bool{}, map[string]bool{} // stuck: a rollback Home Assistant refused
			var unknown *relayOutcomeUnknownError
			done := i
			if errors.As(werr, &unknown) {
				done = i + 1
			} else {
				back[it.DeviceID] = true
			}
			for j := done - 1; j >= 0; j-- {
				rerr := deviceAreaWrite(s, req.Items[j].DeviceID, req.Items[j].From)
				var lost *relayOutcomeUnknownError
				back[req.Items[j].DeviceID] = rerr == nil
				stuck[req.Items[j].DeviceID] = rerr != nil && !errors.As(rerr, &lost) && j < i // its own write landed
			}
			return failRooms(paths, s, env, req, changes, i+1, back, stuck, werr)
		}
	}
	saveRoomsRecord(paths, s, env, req, req.Items)
	s.renewBudget() // 25 writes may outlast the budget the loop started with
	reg, rerr := readRoomsRegistry(s, true)
	env.Status, env.exit = "verified", verbExitOK
	for _, it := range req.Items {
		if d, seen := reg.devices[it.DeviceID]; rerr != nil || !seen || d.area != it.To {
			env.Status, env.exit = "unverified", verbExitUnverified
		}
	}
	env.Result = roomsResult{Changes: changes}
	return nil
}

// failRooms reads the registry back after a rollback. "Nothing was saved"
// only when the read shows every attempted device on its From; otherwise
// the result names each device still changed (when the read fails: every
// attempted one not known to be back, possibly changed, exit 7) and the
// undo record holds what is still to undo.
func failRooms(paths runtimePaths, s *relaySession, env *verbEnvelope, req roomsRequest, changes []roomsChange, attempted int, back, stuck map[string]bool, cause error) error {
	s.renewBudget()
	reg, rerr := readRoomsRegistry(s, true)
	onFrom := func(it roomsItem) bool {
		if rerr != nil {
			return back[it.DeviceID]
		}
		d, seen := reg.devices[it.DeviceID]
		return seen && d.area == it.From
	}
	var moved []roomsItem
	for _, it := range req.Items[:attempted] {
		if !onFrom(it) {
			moved = append(moved, it)
		}
	}
	if len(moved) == 0 {
		return &verbFailure{exit: verbExitUnverified, status: "failed", code: "ROOMS_NOT_SAVED",
			message: "a room change failed (" + cause.Error() + "); every earlier change was undone, so nothing was saved",
			next:    "Nothing changed. Tell the user; plan again only if they still want it."}
	}
	// A rooms change: undo puts the moved devices back. A failed undo: the
	// record keeps every device not yet back where it was, so the original
	// way back stays complete.
	record := moved
	if req.Undo {
		record = nil
		for i, it := range req.Items {
			atTo := reg.devices[it.DeviceID].area == it.To
			if rerr != nil {
				atTo = stuck[it.DeviceID]
			}
			if i >= attempted || onFrom(it) || !atTo {
				record = append(record, roomsItem{DeviceID: it.DeviceID, From: it.To, To: it.From})
			}
		}
	}
	if len(record) > 0 {
		saveRoomsRecord(paths, s, env, req, record)
	}
	var named []string
	for _, it := range moved {
		area := it.To
		if rerr == nil {
			area = reg.devices[it.DeviceID].area
		}
		for _, c := range changes {
			if c.DeviceID == it.DeviceID {
				named = append(named, sayText(c.Name, true)+" → "+cmp.Or(sayText(reg.areas[area], false), sayText(c.Area, false), "no room"))
			}
		}
	}
	failure := &verbFailure{exit: verbExitUnverified, status: "unverified", code: "ROOMS_PARTIAL",
		message: "a room change failed (" + cause.Error() + ") and putting the others back did not fully work; changed now: " + strings.Join(named, ", "),
		next:    "Tell the user exactly which devices changed room. They can move them back in Home Assistant's own device settings."}
	if rerr != nil {
		env.OutcomeUnknown = true
		failure.exit = verbExitUnknown
		failure.message = "a room change failed (" + cause.Error() + ") and the rooms could not be read back; possibly changed: " + strings.Join(named, ", ")
	}
	switch {
	case req.Undo && env.Undo != nil:
		failure.message = "a room change failed (" + cause.Error() + "); back in their previous room now: " + strings.Join(named, ", ") + "; the others were not undone"
		failure.next = "Tell the user which devices are back and that the others were not undone. To finish: ha-nova plan revert --target " + req.Record + " (a new preview)."
	case req.Undo:
		failure.message = "a room change failed (" + cause.Error() + "); back in their previous room now: " + strings.Join(named, ", ")
		failure.next = "Tell the user which devices are back in their previous room."
	case env.Undo != nil:
		failure.next = "Tell the user exactly which devices changed room. To put them back: ha-nova plan revert --target " + req.Record + " (a new preview), or move them in Home Assistant."
	}
	return failure
}

// saveRoomsRecord puts items on the undo stack under the plan's record id:
// before = From, after = To.
func saveRoomsRecord(paths runtimePaths, s *relaySession, env *verbEnvelope, req roomsRequest, items []roomsItem) {
	before, after := map[string]string{}, map[string]string{}
	for _, it := range items {
		before[it.DeviceID], after[it.DeviceID] = it.From, it.To
	}
	record, _ := json.Marshal(map[string]any{"op": "rooms", "domain": roomsUndoDomain, "target_id": req.Record, "server": s.server,
		"name": fmt.Sprintf("Rooms for %s", plural(len(items), "device")), "before_config": before, "expected_after": after})
	if receipt, err := saveUndoSnapshotBytes(paths, record); err == nil {
		env.Undo = &receipt
	} else {
		env.Notices = append(env.Notices, verbNotice{Kind: "undo", Text: "the room changes are not revertible: " + err.Error()})
	}
}
