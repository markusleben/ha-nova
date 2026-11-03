package main

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The URLs a raw WS write stores (Home Assistant's own, a blueprint's source,
// a person's or area's picture) never carry a password from the AI, as a
// setup step's or an App's setting doesn't: a URL with one, also one Go's
// URL parser can't read or reads without it (secretFlowValue), is refused in
// checkRawWS, so ha-nova relay ws, a raw plan, and a chat app's plan refuse
// it alike.

// rawURLFields: per WS type, the fields Home Assistant stores as a URL.
var rawURLFields = map[string][]string{
	"config/core/update": {"external_url", "internal_url"}, "blueprint/save": {"source_url"},
	"person/create": {"picture"}, "person/update": {"picture"},
	"config/area_registry/create": {"picture"}, "config/area_registry/update": {"picture"},
}

// rawYAMLSourceURL: a source_url key in a blueprint's YAML (a tag or anchor
// before it included), which Home Assistant stores like the message's own
// (blueprint: source_url: …), its value on the same line or a later one,
// after a tag, anchor, block header, or comment. rawYAMLBreaks: the line
// breaks YAML reads besides \n. rawYAMLComment: where a comment starts.
var (
	rawYAMLSourceURL = regexp.MustCompile(`(?m)(?:[!&]\S*[ \t]+)*["']?\bsource_url["']?[ \t]*:\s*(?:(?:[!&]\S*|[|>][-+0-9]*|#.*)\s+)*(.*)$`)
	rawYAMLBreaks    = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\u0085", "\n", "\u2028", "\n", "\u2029", "\n")
	rawYAMLComment   = regexp.MustCompile(`(^|[ \t])#`)
)

// rawURLSecret refuses a WS write that sets one of its URL fields, or its
// blueprint YAML's source_url, to a URL with a password.
func rawURLSecret(wsType string, message map[string]any) error {
	refusal := func(field string) error {
		return rawRefusal("WS "+wsType+" that sets "+strconv.Quote(field)+" (a password or other secret)", "secrets don't belong in a chat; "+rawNextUI)
	}
	for _, field := range rawURLFields[wsType] {
		if secretFlowValue(field, message[field]) {
			return refusal(field)
		}
	}
	if wsType != "blueprint/save" {
		return nil
	}
	yaml, _ := message["yaml"].(string)
	yaml = rawYAMLBreaks.Replace(yaml)
	for at := 0; ; {
		m := rawYAMLSourceURL.FindStringSubmatchIndex(yaml[at:])
		if m == nil {
			return nil
		}
		start, value := at+m[0], at+m[2]
		line := strings.LastIndex(yaml[:start], "\n") + 1
		flow := strings.TrimLeft(yaml[line:start], " \t-") != "" // inside a flow mapping: {…, source_url: …}
		if secretFlowValue("source_url", rawYAMLScalar(yaml[value:], start-line, flow)) {
			return refusal("source_url")
		}
		at = max(value, start+1) // a value read as empty may hold the next key
	}
}

// rawYAMLScalar: the scalar text starts with, read closely enough to how
// Home Assistant's YAML loader reads it to find a URL's password, and never
// narrower: a quoted one to its closing quote (rawYAMLQuoted), any other, a
// block scalar too, over the lines indented more than its key's column, up
// to a comment, and in a flow mapping up to its next entry; lines join
// without breaks, which urlsplit drops.
func rawYAMLScalar(text string, column int, flow bool) string {
	if strings.HasPrefix(text, `"`) || strings.HasPrefix(text, "'") {
		return rawYAMLQuoted(text)
	}
	var value strings.Builder
	for i, line := range strings.Split(text, "\n") {
		if i > 0 && strings.TrimSpace(line) != "" && len(line)-len(strings.TrimLeft(line, " ")) <= column {
			break
		}
		line = strings.Trim(line, " \t")
		if at := rawYAMLComment.FindStringIndex(line); at != nil {
			value.WriteString(line[:at[0]])
			break
		}
		value.WriteString(line)
	}
	if end := strings.IndexAny(value.String(), ",[]{}"); flow && end >= 0 {
		return value.String()[:end]
	}
	return value.String()
}

// rawYAMLQuoted: a quoted scalar's text, escapes decoded as the YAML loader
// does (a doubled ', and \x, \u, \U, \t, \n, \r, an escaped line break,
// and any other escaped character as itself), a line break and the
// whitespace around it dropped.
func rawYAMLQuoted(text string) string {
	quote := text[0]
	var value []byte
	for i := 1; i < len(text); i++ {
		switch c := text[i]; {
		case c == quote && quote == '\'' && strings.HasPrefix(text[i+1:], "'"):
			i++
			value = append(value, c)
		case c == quote:
			return string(value)
		case c == '\n' || c == '\\' && quote == '"' && strings.HasPrefix(text[i+1:], "\n"): // a line break, escaped or not
			if c == '\\' {
				i++
			}
			value = bytes.TrimRight(value, " \t")
			for i+1 < len(text) && (text[i+1] == ' ' || text[i+1] == '\t') {
				i++
			}
		case c == '\\' && quote == '"' && i+1 < len(text):
			i++
			digits := map[byte]int{'x': 2, 'u': 4, 'U': 8}[text[i]]
			hex := text[i+1 : min(i+1+digits, len(text))]
			if r, err := strconv.ParseUint(hex, 16, 32); digits > 0 && len(hex) == digits && err == nil {
				value, i = utf8.AppendRune(value, rune(r)), i+digits
			} else if control, ok := map[byte]byte{'t': '\t', 'n': '\n', 'r': '\r', '0': 0}[text[i]]; ok {
				value = append(value, control)
			} else {
				value = append(value, text[i])
			}
		default:
			value = append(value, c)
		}
	}
	return string(value)
}
