package main

import (
	"net/url"
	"regexp"
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

// Text that is no state object still carries the token when it prints one:
// a template's output ({{ states.camera.door }}, an attributes | to_json), a
// media browser thumbnail. In such text the token of a camera or image proxy
// URL and every access_token pair are hidden.
var (
	streamProxyToken = regexp.MustCompile(`(?i)(/api/(?:camera_proxy(?:_stream)?|image_proxy)/[^\s"'<>?]*\?(?:[^\s"'<>]*?[&;])?token=)[A-Za-z0-9._~%+=-]+`)
	streamTokenPair  = regexp.MustCompile(`(?i)(access_token\\?["']?\s*[:=]\s*\\?["']?)[A-Za-z0-9._~%+/=-]+`)
)

// hideStreamTokenText hides those tokens in text that names a camera or an
// image entity or proxy.
func hideStreamTokenText(text string) (string, bool) {
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "token") || !strings.Contains(lower, "_proxy/") && !strings.Contains(lower, "camera.") && !strings.Contains(lower, "image.") {
		return text, false
	}
	hidden := streamProxyToken.ReplaceAllString(text, "${1}"+secretHidden)
	hidden = streamTokenPair.ReplaceAllString(hidden, "${1}"+secretHidden)
	return hidden, hidden != text
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
