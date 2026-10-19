package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"slices"
)

var errClaudeSettingsShape = errors.New("the settings file is not valid JSON in the shape Claude Code uses")

var jsonIndentPattern = regexp.MustCompile(`\n([ \t]+)\S`)

// editClaudeAllow adds the missing rules to permissions.allow, or removes
// one copy of each; it returns the rules it changed and, on add, the key it
// had to create. On remove, prune ("allow" or "permissions": the key setup
// created) drops that key again once it is empty. Key order, every other key,
// deny and ask rules, and every other string's bytes stay; whitespace is
// re-indented with the file's own indent.
func editClaudeAllow(data []byte, rules []string, add bool, prune string) ([]byte, []string, string, error) {
	fail := func(err error) ([]byte, []string, string, error) { return nil, nil, "", err }
	if len(bytes.TrimSpace(data)) == 0 {
		data = []byte("{}\n")
	}
	data, err := strictJSONBytes(data, "Claude Code settings")
	if err != nil {
		return fail(errClaudeSettingsShape)
	}
	root, err := parseOrderedJSONObject(data)
	if err != nil {
		return fail(err)
	}
	created := "permissions"
	perms := &orderedJSONObject{vals: map[string]json.RawMessage{}}
	if raw, ok := root.vals["permissions"]; ok {
		if perms, err = parseOrderedJSONObject(raw); err != nil {
			return fail(err)
		}
		created = "allow"
	}
	var allow []json.RawMessage
	if raw, ok := perms.vals["allow"]; ok {
		if json.Unmarshal(raw, &allow) != nil || allow == nil {
			return fail(errClaudeSettingsShape)
		}
		created = ""
	}
	var changed []string
	for _, rule := range rules {
		at := slices.IndexFunc(allow, func(r json.RawMessage) bool {
			var s string
			return json.Unmarshal(r, &s) == nil && s == rule
		})
		switch {
		case add == (at >= 0):
			continue
		case add:
			var b bytes.Buffer
			enc := json.NewEncoder(&b)
			enc.SetEscapeHTML(false)
			if err := enc.Encode(rule); err != nil {
				return fail(err)
			}
			allow = append(allow, bytes.TrimSpace(b.Bytes()))
		default:
			allow = slices.Delete(allow, at, at+1)
		}
		changed = append(changed, rule)
	}
	if len(changed) == 0 {
		return data, nil, "", nil
	}
	if !add {
		created = ""
	}
	list := []byte("[")
	for i, r := range allow {
		if i > 0 {
			list = append(list, ',')
		}
		list = append(list, r...)
	}
	if !add && len(allow) == 0 && (prune == "allow" || prune == "permissions") {
		perms.remove("allow")
	} else {
		perms.set("allow", append(list, ']'))
	}
	if !add && len(perms.keys) == 0 && prune == "permissions" {
		root.remove("permissions")
	} else {
		root.set("permissions", perms.encode())
	}
	indent := "  "
	if m := jsonIndentPattern.FindSubmatch(data); m != nil {
		indent = string(m[1])
	}
	var compact, out bytes.Buffer
	if err := json.Compact(&compact, root.encode()); err != nil {
		return fail(err)
	}
	if err := json.Indent(&out, compact.Bytes(), "", indent); err != nil {
		return fail(err)
	}
	return append(out.Bytes(), '\n'), changed, created, nil
}

// orderedJSONObject keeps a JSON object's key order and raw values.
type orderedJSONObject struct {
	keys []string
	vals map[string]json.RawMessage
}

func parseOrderedJSONObject(data []byte) (*orderedJSONObject, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errClaudeSettingsShape
	}
	o := &orderedJSONObject{vals: map[string]json.RawMessage{}}
	for dec.More() {
		tok, err := dec.Token()
		key, isKey := tok.(string)
		var raw json.RawMessage
		if err != nil || !isKey || dec.Decode(&raw) != nil {
			return nil, errClaudeSettingsShape
		}
		if _, dup := o.vals[key]; dup { // which one Claude Code reads is not ours to guess
			return nil, errClaudeSettingsShape
		}
		o.keys, o.vals[key] = append(o.keys, key), raw
	}
	if _, err := dec.Token(); err != nil {
		return nil, errClaudeSettingsShape
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errClaudeSettingsShape
	}
	return o, nil
}

func (o *orderedJSONObject) set(key string, raw []byte) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = raw
}

func (o *orderedJSONObject) remove(key string) {
	if _, ok := o.vals[key]; ok {
		delete(o.vals, key)
		o.keys = slices.DeleteFunc(o.keys, func(k string) bool { return k == key })
	}
}

func (o *orderedJSONObject) encode() []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	b.WriteByte('{')
	for i, key := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		_ = enc.Encode(key) // a string always encodes; Encode's newline is JSON whitespace
		b.WriteByte(':')
		b.Write(o.vals[key])
	}
	b.WriteByte('}')
	return b.Bytes()
}
