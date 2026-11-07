package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Renames that put themselves back (calmer confirmations item 9): a plan
// that only renames one entity (name, aliases) or one device (name_by_user)
// outside every lock, alarm panel, and access-cover family runs without a
// preview, its way back saved right before the write. Entity ids, areas,
// and labels stay previewed, and so does anything the CLI can't read.

const namesUndoOp = "names"

// rawNamesTarget: the registry and canonical id of a plan that is one name
// update and nothing else; ok is false for anything more.
func rawNamesTarget(items []rawRequest) (domain, id string, payload map[string]any, ok bool) {
	if len(items) != 1 || items[0].Endpoint != "ws" || items[0].Typed {
		return "", "", nil, false
	}
	payload, _ = decodeObject(items[0].Payload)
	text := func(v any) bool { _, isText := v.(string); return v == nil || isText }
	idKey, named := "", false
	switch payload["type"] {
	case "config/entity_registry/update":
		domain, idKey = "entity_registry", "entity_id"
	case "config/device_registry/update":
		domain, idKey = "device_registry", "device_id"
	default:
		return "", "", nil, false
	}
	id, _ = payload[idKey].(string)
	for key, value := range payload {
		switch {
		case key == "type" || key == idKey:
		case key == "name" && domain == "entity_registry" || key == "name_by_user" && domain == "device_registry":
			if !text(value) {
				return "", "", nil, false
			}
			named = true
		case key == "aliases" && domain == "entity_registry":
			list, isList := value.([]any)
			for _, alias := range list {
				isList = isList && text(alias)
			}
			if !isList {
				return "", "", nil, false
			}
			named = true
		default:
			return "", "", nil, false
		}
	}
	if domain == "entity_registry" {
		id = strings.ToLower(id) // Home Assistant lowercases it
	}
	return domain, id, payload, named && id != ""
}

// readNames reads the registry entry a rename changes, live; nil when
// Home Assistant doesn't hold it.
func readNames(s *relaySession, domain, id string) (map[string]any, error) {
	if domain == "device_registry" {
		return rawDeviceUpdate.read(s, id)
	}
	live, err := rawEntityUpdate.read(s, id)
	if isUpstreamNotFound(err) {
		return nil, nil
	}
	return live, err
}

// calmNames: a rename plan runs without a preview only when the write is
// its one reason, its target reads live and sits in no access family
// (accessEntity, accessDevice: a failed read counts as one), and its way
// back can be added without replacing or evicting another (checkpointFits).
func (e *tierEngine) calmNames(n *rawNamer, items []rawRequest) bool {
	domain, id, _, ok := rawNamesTarget(items)
	if !ok || e.level != tierPreview || len(e.reasons) != 1 || e.reasons[0].Code != reasonRawWrite ||
		len(e.notes) > 0 || len(e.skipped) > 0 || len(e.lost) > 0 {
		return false
	}
	if live, err := readNames(e.s, domain, id); err != nil || live == nil {
		return false
	}
	if domain == "entity_registry" && n.accessEntity(id) || domain == "device_registry" && n.accessDevice(id) {
		return false
	}
	stack, err := loadUndoSnapshotStack(e.s.paths)
	return err == nil && checkpointFits(stack, e.s.server, domain, id)
}

// saveNamesUndo saves the way back of a rename that runs without a preview
// (plan.confirm none) right before its write: the live value of exactly
// what it sends, nulls kept. Nothing is sent when that fails.
func saveNamesUndo(paths runtimePaths, s *relaySession, env *verbEnvelope, items []rawRequest) (*snapshotSaveReceipt, error) {
	domain, id, payload, ok := rawNamesTarget(items)
	if !ok || env.Plan == nil || env.Plan.Confirm != "none" {
		return nil, nil
	}
	failed := func(why string) error {
		return &verbFailure{exit: verbExitError, code: "UNDO_SAVE_FAILED",
			message: fmt.Sprintf("the way to put it back could not be saved first (%s); nothing was sent", why),
			next:    "Tell the user nothing changed, because its way back could not be saved. Plan it again only if they still want it."}
	}
	live, err := readNames(s, domain, id)
	if err != nil || live == nil {
		return nil, failed("its current name could not be read")
	}
	before, after := map[string]any{}, map[string]any{}
	for key, value := range payload {
		if key != "type" && key != "entity_id" && key != "device_id" {
			before[key], after[key] = live[key], value
		}
	}
	if aliases, sent := before["aliases"]; sent && aliases == nil {
		before["aliases"] = []any{}
	}
	name, _ := live["name"].(string)
	if domain == "entity_registry" && name == "" {
		name, _ = live["original_name"].(string)
	}
	if byUser, _ := live["name_by_user"].(string); domain == "device_registry" && byUser != "" {
		name = byUser
	}
	record, _ := json.Marshal(map[string]any{"op": namesUndoOp, "domain": domain, "target_id": id, "name": strings.TrimSpace(name),
		"server": s.server, "before_config": before, "expected_after": after})
	receipt, err := addUndoSnapshot(paths, record)
	if err != nil {
		return nil, failed(err.Error())
	}
	return &receipt, nil
}

// settleNamesUndo drops the saved way back when the rename never landed,
// and keeps it for any other answer.
func settleNamesUndo(paths runtimePaths, s *relaySession, env *verbEnvelope, saved *snapshotSaveReceipt, status string) {
	switch {
	case saved == nil:
	case status == "rejected" || status == "not_sent":
		_ = dropUndoSnapshot(paths, s.server, *saved)
	default:
		env.Undo, env.undoable = saved, status == "done"
	}
}

// planNamesUndo plans putting a rename back (plan revert): the saved
// values, as a raw plan, previewed. Nothing is planned when the entry is
// already back, or changed again since (REVERT_DRIFT).
func planNamesUndo(paths runtimePaths, s *relaySession, env *verbEnvelope, snap undoSnapshot) error {
	var before, after map[string]any
	if json.Unmarshal(snap.BeforeConfig, &before) != nil || json.Unmarshal(snap.ExpectedAfter, &after) != nil {
		return refused("NO_UNDO_SNAPSHOT", "the rename's undo record cannot be read")
	}
	idKey := map[string]string{"entity_registry": "entity_id", "device_registry": "device_id"}[snap.Domain]
	payload := map[string]any{"type": "config/" + snap.Domain + "/update", idKey: snap.TargetID}
	for key, value := range before {
		payload[key] = value
	}
	body, _ := json.Marshal(payload)
	items := []rawRequest{{Endpoint: "ws", Payload: body}}
	if domain, id, _, ok := rawNamesTarget(items); idKey == "" || !ok || domain != snap.Domain || id != snap.TargetID || len(after) != len(before) {
		return refused("NO_UNDO_SNAPSHOT", "the rename's undo record is not one HA NOVA saved")
	}
	live, err := readNames(s, snap.Domain, snap.TargetID)
	if err != nil {
		return err
	}
	if live == nil {
		return refusedWith("NOTHING_TO_UNDO", "it is no longer in Home Assistant", "Tell the user it can't be put back: it is gone.")
	}
	same := func(key string, a, b any) bool {
		if key == "aliases" {
			return len(listDrops(a, b, "", strings.TrimSpace)) == 0 && len(listDrops(b, a, "", strings.TrimSpace)) == 0
		}
		return jsonEqual(a, b)
	}
	back, changed := true, false
	for key := range before {
		back = back && same(key, live[key], before[key])
		changed = changed || !same(key, live[key], after[key])
	}
	if back {
		return refusedWith("NOTHING_TO_UNDO", "its name is already back to what it was", "Tell the user nothing needs undoing.")
	}
	if changed {
		return &verbFailure{exit: verbExitDrift, status: "drift", code: "REVERT_DRIFT",
			message: "its name changed again since HA NOVA renamed it, so nothing was planned",
			next:    "Show the user its current name and ask what they want it to be; that is a new rename."}
	}
	if len(rawEntityUpdate.dropped(live, payload)) > 0 { // putting back removes an alias: its copy first (helperKept)
		copied, _ := json.Marshal(map[string]any{"action": "save", "category": rawEntityUpdate.category, "name": autoName(snap.TargetID), "data": live})
		items = append([]rawRequest{{Endpoint: "backups", Payload: copied}}, items...)
	}
	_, plan, err := computeRawPlan(s, env, items)
	if err != nil {
		return err
	}
	return savePlan(paths, plan, s.transport.Credential)
}
