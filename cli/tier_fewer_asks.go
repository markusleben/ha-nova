package main

import (
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

// ownedOrdinary: an owned service that runs at once on a read entity state;
// a louder volume only from a known level and only a moderate step.
func ownedOrdinary(service string, data map[string]any, st *entityState) bool {
	if st == nil {
		return false
	}
	current, known := st.attr("volume_level").(float64)
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

// ownedChecks: what an owned call's own class can't see. A shopping list
// write fires shopping_list_updated, so its listeners are classified. An
// ordinary owned call keeps its preview (OWNED_MEMBER, as before) when a
// universal player's user-written commands may run instead, its speaker
// group plays along (group_members), a stored step changes the volume (the
// plan-time level isn't the level at that step), or its device family
// carries a lock, alarm panel, or access cover.
func (e *tierEngine) ownedChecks(id, eff, via string, depth int, level tierLevel, st *entityState) tierLevel {
	entry := e.reg[id]
	if entry.pl == "shopping_list" && strings.HasPrefix(eff, "todo.") && !readOnlyResponseServices[eff] {
		e.checkEvents("shopping_list_updated", id)
	}
	if level != tierOrdinary || ownerSkill(eff) == "" {
		return level
	}
	grouped, _ := st.attr("group_members").([]any)
	if entry.pl != "universal" && len(grouped) < 2 && !storedVolumeStep(eff, depth) && e.deviceAccessEntity(entry.di, true) == "" {
		return level
	}
	e.raise(tierPreview, tierReason{Code: reasonOwnedMember, EntityID: id, Via: via})
	return tierPreview
}

// storedVolumeStep: a stored step that raises or sets a volume or unmutes;
// visit keeps it apart from the direct call, even with the same data.
func storedVolumeStep(service string, depth int) bool {
	return depth > 0 && strings.HasPrefix(service, "media_player.volume_") && service != "media_player.volume_down"
}

// roomLights: light.turn_on, turn_off, or toggle on an area, or on one group
// named by its entity_id, that reaches lights only, at most tierMaxExplicit,
// none skipped and none on a device with a lock, alarm panel, or access
// cover, acts like naming each light; every other rule still classifies
// each one. Only Home Assistant's own groups count: another platform's
// entity_id list (a template light) does not prove what switching it runs.
func (e *tierEngine) roomLights(req planCallRequest, targets []verbTarget) bool {
	domain, name := splitID(req.Service)
	byArea := req.Area != "" && len(req.EntityIDs) == 0
	byGroup := req.Area == "" && len(req.EntityIDs) == 1 && len(e.groups) > 0
	if domain != "light" || (name != "turn_on" && name != "turn_off" && name != "toggle") || !byArea && !byGroup || req.Match != "" || req.Device != "" ||
		len(targets) > tierMaxExplicit || !e.complete || slices.Contains(e.notes, noteSkippedTargets) {
		return false
	}
	for _, group := range e.groups {
		if e.reg[group].pl != "group" {
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
