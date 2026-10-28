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
	return hideStreamTokenAttrs(attrs.vals)
}

// hideStreamTokenAttrs hides the token in a camera's or image entity's
// attributes.
func hideStreamTokenAttrs(attrs map[string]any) bool {
	changed := hideSet(attrs, "access_token")
	switch picture := attrs["entity_picture"].(type) {
	case nil:
	case string:
		if kept, did := dropTokenParam(picture); did {
			attrs["entity_picture"], changed = kept, true
		}
	default:
		changed = hideSet(attrs, "entity_picture") || changed
	}
	return changed
}

// Text that is no state object still carries the token when it prints one:
// a template's output ({{ states.camera.door }}, an attributes | to_json), a
// media browser thumbnail. In such text the token of a camera or image proxy
// URL and every access_token pair with a token's value (Home Assistant's
// are hex, so a template's source such as {% set access_token = … %} stays).
var (
	streamProxyURL  = regexp.MustCompile(`(?i)/api/(?:camera_proxy(?:_stream)?|image_proxy)/[^\s"'<>?]*\?[A-Za-z0-9._~%+=&;-]*`)
	streamTokenPair = regexp.MustCompile(`(?i)(access_token\\?["']?\s*[:=]\s*\\?["']?)[0-9a-f]{32,}`)
)

// hideStreamTokenText hides those tokens in text that names a camera or an
// image entity or proxy.
func hideStreamTokenText(text string) (string, bool) {
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "token") || !strings.Contains(lower, "_proxy/") && !strings.Contains(lower, "camera.") && !strings.Contains(lower, "image.") {
		return text, false
	}
	hidden := streamProxyURL.ReplaceAllStringFunc(text, func(link string) string {
		path, query, _ := strings.Cut(link, "?")
		var b strings.Builder
		b.WriteString(path + "?")
		start := 0
		for i := 0; i <= len(query); i++ {
			if i < len(query) && query[i] != '&' && query[i] != ';' {
				continue
			}
			param := query[start:i]
			if name, value, _ := strings.Cut(param, "="); value != "" { // an empty one, as a template builds it, holds nothing
				if decoded, err := url.QueryUnescape(name); err != nil || strings.EqualFold(strings.TrimSpace(decoded), "token") {
					param = name + "=" + secretHidden
				}
			}
			b.WriteString(param)
			if i < len(query) {
				b.WriteByte(query[i])
			}
			start = i + 1
		}
		return b.String()
	})
	hidden = streamTokenPair.ReplaceAllString(hidden, "${1}"+secretHidden)
	return hidden, hidden != text
}

// streamProxyPicture: a picture served by a camera's or image entity's proxy.
func streamProxyPicture(picture string) bool {
	lower := strings.ToLower(picture)
	return strings.Contains(lower, "/api/camera_proxy") || strings.Contains(lower, "/api/image_proxy")
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
