package main

import (
	"errors"
	"slices"
	"strings"
)

// Fewer asks for everyday things (docs/work/2026-11-02-fewer-asks-spec.md):
// a few owned services and a room's lights run at once; every other rule
// still classifies them.

// ownedOrdinaryServices: owned services that run at once, a media player's
// transport, source, and volume down, and adding or updating a to-do item.
// Muting and other volume changes: ownedOrdinary.
var ownedOrdinaryServices = map[string]bool{
	"media_player.media_play": true, "media_player.media_pause": true, "media_player.media_play_pause": true,
	"media_player.media_stop": true, "media_player.media_next_track": true, "media_player.media_previous_track": true,
	"media_player.select_source": true, "media_player.volume_down": true, "todo.add_item": true, "todo.update_item": true,
}

// The volume jump rule of skills/media/SKILL.md (Safety), in fractions of
// full volume: a level above the cap, or a rise of more than the jump, is
// big. volumeSlack absorbs float error only (0.5 + 0.3), never a margin.
const (
	volumeCap   = 0.8
	volumeJump  = 0.3
	volumeStep  = 0.1 // Home Assistant's default volume_up step
	volumeSlack = 1e-9
)

// tierMaxRoomLights: the most lights a room's lights run at once (Markus,
// 2026-11-02); named ids keep tierMaxExplicit.
const tierMaxRoomLights = 30

// ownedOrdinary: an owned service that runs at once on a read entity state;
// a louder volume only from a known level and only a moderate step. A level
// outside 0..1, or one the player only assumes, is not known.
func ownedOrdinary(service string, data map[string]any, st *entityState) bool {
	if st == nil {
		return false
	}
	current, known := st.attr("volume_level").(float64)
	known = known && current >= 0 && current <= 1 && st.attr("assumed_state") != true
	switch service {
	case "media_player.volume_up":
		return known && current+volumeStep <= volumeCap+volumeSlack
	case "media_player.volume_set":
		level, isNumber := data["volume_level"].(float64)
		return isNumber && known && level <= volumeCap+volumeSlack && level-current <= volumeJump+volumeSlack
	case "media_player.volume_mute":
		muted, isBool := data["is_volume_muted"].(bool)
		return isBool && (muted || known && current <= volumeCap+volumeSlack)
	}
	return ownedOrdinaryServices[service]
}

// shoppingRemoveMax: the most items a shopping-list removal runs at once
// (calmer confirmations item 7).
const shoppingRemoveMax = 5

// shoppingItemsListed: a todo.remove_item on the built-in shopping list
// names 1 to shoppingRemoveMax distinct items (uid or summary, as Home
// Assistant matches them), each on the list as read now, and nothing else.
// Any other shape, a missing item, or a failed read keeps the preview.
func (e *tierEngine) shoppingItemsListed(id string, data map[string]any) bool {
	if e.reg[id].pl != "shopping_list" || len(data) != 1 {
		return false
	}
	var refs []string
	switch item := data["item"].(type) {
	case string:
		refs = []string{item}
	case []any:
		for _, v := range item {
			ref, isText := v.(string)
			if !isText {
				return false
			}
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 || len(refs) > shoppingRemoveMax {
		return false
	}
	type listed struct {
		UID     string `json:"uid"`
		Summary string `json:"summary"`
	}
	var reply struct {
		Items []listed `json:"items"`
	}
	if e.s.wsDecode(map[string]any{"type": "todo/item/list", "entity_id": id}, &reply, "to-do items") != nil {
		return false
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		onList := slices.ContainsFunc(reply.Items, func(it listed) bool { return it.UID == ref || it.Summary == ref })
		if ref == "" || seen[ref] || !onList {
			return false
		}
		seen[ref] = true
	}
	return true
}

// commandPlayers: media player platforms whose controls run commands or
// scripts the user wrote.
var commandPlayers = map[string]bool{"universal": true, "media_player_template": true}

// ownedChecks: what an owned call's own class can't see. A shopping list
// write fires shopping_list_updated, so its listeners are classified. An
// ordinary owned call keeps its preview (OWNED_MEMBER, as before) when a
// commandPlayers player's user-written commands may run instead, its
// speaker group plays along (group_members names another player), a stored
// step changes the volume (the plan-time level isn't the level at that
// step), or its device family carries a lock, alarm panel, or access cover.
func (e *tierEngine) ownedChecks(id, eff, via string, depth int, level tierLevel, st *entityState) tierLevel {
	entry := e.reg[id]
	if entry.pl == "shopping_list" && strings.HasPrefix(eff, "todo.") && !readOnlyResponseServices[eff] {
		e.checkEvents("shopping_list_updated", id)
	}
	if level != tierOrdinary || ownerSkill(eff) == "" {
		return level
	}
	members, isList := st.attr("group_members").([]any)
	grouped := !isList && st.attr("group_members") != nil || slices.ContainsFunc(members, func(member any) bool { return member != id })
	if !commandPlayers[entry.pl] && !grouped && !storedVolumeStep(eff, depth) && e.deviceAccessEntity(entry.di, true) == "" {
		return level
	}
	e.raise(tierPreview, tierReason{Code: reasonOwnedMember, EntityID: id, Via: via})
	return tierPreview
}

// targetless: a call without entities (classifyTargetless). Every
// shopping_list service fires shopping_list_updated, so its listeners are
// classified too.
func (e *tierEngine) targetless(service string, data map[string]any) (tierLevel, string) {
	if domain, _ := splitID(service); domain == "shopping_list" {
		e.checkEvents("shopping_list_updated", "")
	}
	return classifyTargetless(service, data)
}

// rawShoppingList: the tier a raw item reaches through its listeners. A
// todo/item/move on a shopping list reorders it and fires
// shopping_list_updated, so its listeners are classified (with the
// registry, as a call's are). An unreadable registry makes every move
// typed. A list's class is walked once and reused (done), since a read that
// failed is cached and would not raise a later move's class again.
func (e *tierEngine) rawShoppingList(endpoint string, payload map[string]any) tierLevel {
	if endpoint != "ws" || payload["type"] != "todo/item/move" {
		return tierOrdinary
	}
	id, _ := payload["entity_id"].(string)
	key := "listeners shopping_list_updated " + strings.ToLower(id)
	if class, walked := e.done[key]; walked {
		return class
	}
	class := tierOrdinary
	e.scopes = append(e.scopes, &class)
	if len(e.reg) == 0 && !e.regFailed {
		e.loadRegistry()
	}
	if e.reg[strings.ToLower(id)].pl == "shopping_list" {
		e.checkEvents("shopping_list_updated", strings.ToLower(id))
	}
	e.scopes = e.scopes[:len(e.scopes)-1]
	if e.regFailed {
		class = tierTyped
	}
	e.done[key] = class
	return class
}

// rawMovesAlone: a raw plan with a todo/item/move holds only moves.
// rawShoppingList reads the registry before any item runs, so an earlier
// item that renames or enables a list would send a move whose listeners
// were never classified (#928).
func rawMovesAlone(items []rawRequest) error {
	moves := 0
	for _, item := range items {
		if payload, _ := decodeObject(item.Payload); item.Endpoint == "ws" && payload["type"] == "todo/item/move" {
			moves++
		}
	}
	if moves == 0 || moves == len(items) {
		return nil
	}
	return errors.New("a to-do reorder (todo/item/move) goes in a plan of reorders only: an earlier step could rename or turn on a list, and the reorder would skip the check of what listens to it; plan the other changes first, then the reorders on their own")
}

// storedVolumeStep: a stored step that raises or sets a volume or unmutes;
// visit keeps it apart from the direct call, even with the same data.
func storedVolumeStep(service string, depth int) bool {
	return depth > 0 && strings.HasPrefix(service, "media_player.volume_") && service != "media_player.volume_down"
}

// roomLights: light.turn_on, turn_off, or toggle on an area, or on one group
// named by its entity_id, that reaches lights only, at most
// tierMaxRoomLights, none skipped and none on a device with a lock, alarm
// panel, or access cover, acts like naming each light; every other rule
// still classifies each one. Only Home Assistant's own groups and Hue's
// grouped lights count: another platform's entity_id list (a template
// light) does not prove what switching it runs.
func (e *tierEngine) roomLights(req planCallRequest, targets []verbTarget) bool {
	domain, name := splitID(req.Service)
	byArea := req.Area != "" && len(req.EntityIDs) == 0
	byGroup := req.Area == "" && len(req.EntityIDs) == 1 && len(e.groups) > 0
	if domain != "light" || (name != "turn_on" && name != "turn_off" && name != "toggle") || !byArea && !byGroup || req.Match != "" || req.Device != "" ||
		len(targets) > tierMaxRoomLights || !e.complete || slices.Contains(e.notes, noteSkippedTargets) {
		return false
	}
	for _, group := range e.groups {
		if pl := e.reg[group].pl; pl != "group" && (pl != "hue" || e.states[group].attr("is_hue_group") != true) {
			return false
		}
	}
	for _, t := range targets {
		if entityDomain, _ := splitID(t.EntityID); entityDomain != "light" || e.deviceAccessEntity(e.reg[t.EntityID].di, true) != "" {
			return false
		}
	}
	return true
}
