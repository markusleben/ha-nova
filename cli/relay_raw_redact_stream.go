package main

import (
	"net/url"
	"strings"
)

// streamTokenEntity: a camera or image entity, whose access_token (also in
// its entity_picture) opens /api/camera_proxy_stream (live video) or the
// image for minutes without a login (redactSecrets).
func streamTokenEntity(id string) bool {
	return strings.HasPrefix(id, "camera.") || strings.HasPrefix(id, "image.")
}

// hideStreamToken hides the access token in a camera's or image entity's
// attributes m[key]; attributes or a picture of another shape are hidden
// whole.
func hideStreamToken(m map[string]any, key string) bool {
	attrs, isObject := m[key].(*jsonObject)
	if !isObject {
		return hideSet(m, key)
	}
	changed := hideSet(attrs.vals, "access_token")
	switch picture := attrs.vals["entity_picture"].(type) {
	case nil:
	case string:
		if kept, did := dropTokenParam(picture); did {
			attrs.vals["entity_picture"], changed = kept, true
		}
	default:
		changed = hideSet(attrs.vals, "entity_picture") || changed
	}
	return changed
}

// dropTokenParam drops every token parameter from a picture URL and keeps
// its path; a parameter whose name does not decode goes too.
func dropTokenParam(picture string) (string, bool) {
	path, query, found := strings.Cut(picture, "?")
	if !found {
		return picture, false
	}
	var kept []string
	for _, param := range strings.FieldsFunc(query, func(r rune) bool { return r == '&' || r == ';' }) {
		name, _, _ := strings.Cut(param, "=")
		if name, err := url.QueryUnescape(name); err == nil && !strings.EqualFold(strings.TrimSpace(name), "token") {
			kept = append(kept, param)
		}
	}
	if len(kept) > 0 {
		path += "?" + strings.Join(kept, "&")
	}
	return path, path != picture
}
