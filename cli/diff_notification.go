package main

import "strings"

// diffNotificationCopyInCommonItems keeps user-authored notification copy visible
// even when a structural action/sequence edit would otherwise collapse the array
// diff to "Actions: N -> M items".
func diffNotificationCopyInCommonItems(segs []segment, before, after []interface{}, changes *[]configChange) {
	if !isActionLikePath(segs) {
		return
	}
	limit := len(before)
	if len(after) < limit {
		limit = len(after)
	}
	for i := 0; i < limit; i++ {
		bm, bok := before[i].(map[string]interface{})
		am, aok := after[i].(map[string]interface{})
		if !bok || !aok || !isNotificationCall(bm) || !isNotificationCall(am) {
			continue
		}
		itemPath := appendSegment(segs, segment{index: i, isIndex: true, anchor: itemAnchorFor(segs, before[i])})
		diffNotificationDataField(itemPath, bm, am, "title", changes)
		diffNotificationDataField(itemPath, bm, am, "message", changes)
		diffNotificationDataField(itemPath, bm, am, "data", changes)
	}
	diffSingleMovedNotificationCopy(segs, before, after, changes)
}

func isActionLikePath(segs []segment) bool {
	if len(segs) == 0 {
		return false
	}
	last := segs[len(segs)-1]
	return !last.isIndex && (last.key == "actions" || last.key == "sequence")
}

func isNotificationCall(m map[string]interface{}) bool {
	action, _ := m["action"].(string)
	if action == "" {
		action, _ = m["service"].(string)
	}
	return strings.HasPrefix(action, "notify.") || strings.HasPrefix(action, "persistent_notification.")
}

func diffNotificationDataField(itemPath []segment, before, after map[string]interface{}, key string, changes *[]configChange) {
	bv, bok := notificationDataValue(before, key)
	av, aok := notificationDataValue(after, key)
	switch {
	case !bok && !aok:
		return
	case bok && !aok:
		if !isEmptyValue(bv) {
			*changes = append(*changes, makeChange(notificationFieldPath(itemPath, key), bv, nil))
		}
	case !bok && aok:
		if !isEmptyValue(av) {
			*changes = append(*changes, makeChange(notificationFieldPath(itemPath, key), nil, av))
		}
	case !valuesEqual(bv, av):
		*changes = append(*changes, makeChange(notificationFieldPath(itemPath, key), bv, av))
	}
}

func diffSingleMovedNotificationCopy(segs []segment, before, after []interface{}, changes *[]configChange) {
	beforeIndex, beforeItem, beforeOK := singleNotificationItem(before)
	afterIndex, afterItem, afterOK := singleNotificationItem(after)
	if !beforeOK || !afterOK || beforeIndex == afterIndex {
		return
	}
	itemPath := appendSegment(segs, segment{index: afterIndex, isIndex: true, anchor: itemAnchorFor(segs, beforeItem)})
	diffNotificationDataField(itemPath, beforeItem, afterItem, "title", changes)
	diffNotificationDataField(itemPath, beforeItem, afterItem, "message", changes)
	diffNotificationDataField(itemPath, beforeItem, afterItem, "data", changes)
}

func singleNotificationItem(items []interface{}) (int, map[string]interface{}, bool) {
	found := -1
	var item map[string]interface{}
	for i, raw := range items {
		m, ok := raw.(map[string]interface{})
		if !ok || !isNotificationCall(m) {
			continue
		}
		if found >= 0 {
			return 0, nil, false
		}
		found = i
		item = m
	}
	return found, item, found >= 0
}

func notificationDataValue(m map[string]interface{}, key string) (interface{}, bool) {
	data, ok := m["data"].(map[string]interface{})
	if !ok {
		return nil, false
	}
	v, exists := data[key]
	return v, exists
}

func notificationFieldPath(itemPath []segment, key string) []segment {
	return appendSegment(appendSegment(itemPath, segment{key: "data"}), segment{key: key})
}
