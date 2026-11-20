package main

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"path"
	"slices"
	"strings"
	"time"
	"unicode"

	"go.yaml.in/yaml/v3"
)

// YAML generic thermostats and hygrostats, and the YAML template switches
// they drive (MAR-245, docs/work/2026-11-20-yaml-helper-read-spec.md). The
// relay session's pseudo-endpoint yaml_helpers (payload exactly {}) reads
// /config through the Relay's files endpoint and answers a projection: each
// YAML helper's switched entity and each template switch's turn_on/turn_off
// calls, or one failure code. No path, content, or error text leaves it.
// It answers only what Home Assistant provably runs: every file it used is
// older (ctime) than Home Assistant's start, and every directory a dir
// include walked was unchanged (mtime and ctime) since then.

const yamlHelpersEndpoint = "yaml_helpers"

// Failure codes, the only thing about a failed read that leaves the session.
const (
	yamlFilesOff = "files_off"
	yamlUnread   = "yaml_unread"
	yamlChanged  = "yaml_changed"
)

// Limits on what one projection reads.
const (
	yamlMaxDirs    = 50
	yamlMaxEntries = 2000
	yamlMaxFiles   = 200
	yamlMaxBytes   = 8 << 20
	yamlMaxDepth   = 8
	yamlMaxNodes   = 200000
)

type yamlStep struct {
	Service string   `json:"service"`
	Targets []string `json:"targets"`
}

// yamlSwitch: the template switches with one registry unique id.
type yamlSwitch struct {
	Count int        `json:"count"` // entries with this unique id, legacy ones included
	Plain bool       `json:"plain"` // a modern entry outside a trigger block whose actions are plain calls
	Steps []yamlStep `json:"steps,omitempty"`
}

type yamlProjection struct {
	Failure    string                `json:"failure,omitempty"`
	Helpers    map[string][]string   `json:"helpers,omitempty"`    // integration → each YAML entry's switched entity
	Switches   map[string]yamlSwitch `json:"switches,omitempty"`   // registry unique id → its entries
	Unreadable bool                  `json:"unreadable,omitempty"` // a template switch whose unique id isn't a plain string, or a template blueprint
}

type yamlFail string

func (f yamlFail) Error() string { return string(f) }

type yamlEntry struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Size     *int64  `json:"size"`
	Modified *string `json:"modified"`
	Changed  *string `json:"changed"`
}

type yamlReader struct {
	s       *relaySession
	dirs    map[string][]yamlEntry // first listings
	used    map[string]bool        // path → it must be older than the start (a file read, or a walked directory)
	walked  map[string]bool        // directories a dir include walked: their whole listing must hold
	incDir  map[*yaml.Node]string  // an include tag → its file's directory
	files   int
	bytes   int
	nodes   int
	entries int
}

var yamlIncludes = map[string]bool{"!include": true, "!include_dir_list": true, "!include_dir_named": true, "!include_dir_merge_list": true, "!include_dir_merge_named": true}

var yamlCoreTags = map[string]bool{"!!str": true, "!!int": true, "!!float": true, "!!bool": true, "!!null": true, "!!map": true, "!!seq": true, "!!timestamp": true, "!!merge": true, "!secret": true, "!env_var": true}

// yamlHelpers answers the pseudo-endpoint. A read the session budget cuts is
// an error, as for any read; every other gap is a failure code.
func (s *relaySession) yamlHelpers(body []byte) (json.RawMessage, error) {
	if string(body) != "{}" {
		return nil, errors.New("yaml helpers take an empty payload; nothing was sent")
	}
	y := &yamlReader{s: s, dirs: map[string][]yamlEntry{}, used: map[string]bool{}, walked: map[string]bool{}, incDir: map[*yaml.Node]string{}}
	proj, err := y.project()
	if fail := yamlFail(""); errors.As(err, &fail) {
		proj, err = yamlProjection{Failure: string(fail)}, nil
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(proj)
}

func (y *yamlReader) project() (yamlProjection, error) {
	proj := yamlProjection{Helpers: map[string][]string{}, Switches: map[string]yamlSwitch{}}
	start, err := y.start()
	if err != nil {
		return proj, err
	}
	root, err := y.load("/config/configuration.yaml", 0)
	if err != nil {
		return proj, err
	}
	values, err := y.domainValues(root)
	if err != nil {
		return proj, err
	}
	for domain, list := range values {
		for _, value := range list {
			if value, err = y.resolve(value, 0); err != nil {
				return proj, err
			}
			if err = projectDomain(&proj, domain, value); err != nil {
				return proj, err
			}
		}
	}
	for _, list := range proj.Helpers {
		slices.Sort(list)
	}
	return proj, y.proven(start)
}

// start: Home Assistant's own run lock, written at every start before the
// configuration loads; its ctime must agree with the start it records.
func (y *yamlReader) start() (time.Time, error) {
	content, err := y.content(yamlLock)
	if err != nil {
		return time.Time{}, err
	}
	var lock struct {
		StartTS float64 `json:"start_ts"`
	}
	entry, _ := y.entry(yamlLock, "file")
	changed, known := yamlTime(entry.Changed)
	err = json.Unmarshal([]byte(content), &lock)
	start := time.Unix(0, int64(lock.StartTS*1e9))
	if err != nil || !known || lock.StartTS < 946684800 || math.Abs(changed.Sub(start).Seconds()) > 120 {
		return time.Time{}, yamlFail(yamlChanged)
	}
	return start, nil
}

const yamlLock = "/config/.ha_run.lock"

// domainValues: the values of the keys the projection reads, from the root
// and every package (a superset of what Home Assistant merges).
func (y *yamlReader) domainValues(root *yaml.Node) (map[string][]*yaml.Node, error) {
	out := map[string][]*yaml.Node{}
	collect := func(n *yaml.Node) error {
		m, err := yamlMapping(n)
		for key, value := range m {
			for _, domain := range []string{"climate", "generic_hygrostat", "template", "switch"} {
				words := strings.Fields(key)
				switch {
				case len(words) == 0 || words[0] != domain:
				case key == domain:
					out[domain] = append(out[domain], value)
				case domain == "generic_hygrostat": // Home Assistant reads only the bare key
				case strings.HasPrefix(key, domain+" ") && len(key) > len(domain)+1 && !strings.ContainsFunc(key, unicode.IsControl):
					out[domain] = append(out[domain], value)
				default:
					return yamlFail(yamlUnread) // a spelling Home Assistant may split differently
				}
			}
		}
		return err
	}
	core, err := y.value(root, "homeassistant")
	if err != nil {
		return nil, err
	}
	packages, err := y.value(core, "packages")
	if err != nil {
		return nil, err
	}
	pkgs, err := yamlMapping(packages)
	if err == nil {
		err = collect(root)
	}
	for _, pkg := range pkgs {
		if err == nil {
			pkg, err = y.include(pkg, 0)
		}
		if err == nil {
			err = collect(pkg)
		}
	}
	return out, err
}

// value: key's value in mapping n, its include resolved.
func (y *yamlReader) value(n *yaml.Node, key string) (*yaml.Node, error) {
	m, err := yamlMapping(n)
	if err != nil {
		return nil, err
	}
	return y.include(m[key], 0)
}

// resolve: n with every include in its subtree resolved; an alias or a
// merge key anywhere is unknown.
func (y *yamlReader) resolve(n *yaml.Node, depth int) (*yaml.Node, error) {
	if _, tagged := y.incDir[n]; tagged {
		depth++ // what an include loads counts toward the depth below it
	}
	n, err := y.include(n, depth)
	if err != nil || n == nil {
		return n, err
	}
	if n.Kind == yaml.AliasNode {
		return nil, yamlFail(yamlUnread)
	}
	for i, child := range n.Content {
		if n.Kind == yaml.MappingNode && i%2 == 0 {
			if child.ShortTag() == "!!merge" || y.incDir[child] != "" {
				return nil, yamlFail(yamlUnread)
			}
			continue
		}
		if n.Content[i], err = y.resolve(child, depth); err != nil {
			return nil, err
		}
	}
	return n, nil
}

// include resolves an include tag on n itself (and on what it loads).
func (y *yamlReader) include(n *yaml.Node, depth int) (*yaml.Node, error) {
	dir, tagged := y.incDir[n]
	if !tagged {
		return n, nil
	}
	if depth >= yamlMaxDepth {
		return nil, yamlFail(yamlUnread)
	}
	target, err := yamlIncludePath(dir, n.Value)
	if err != nil {
		return nil, err
	}
	if n.Tag == "!include" {
		out, err := y.load(target, depth+1)
		if out == nil && err == nil {
			out = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}
		return out, err
	}
	files, err := y.walk(target)
	if err != nil {
		return nil, err
	}
	contents, err := y.contents(files)
	if err != nil {
		return nil, err
	}
	out := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	if strings.HasSuffix(n.Tag, "named") {
		out = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	seen := map[string]bool{}
	for i, file := range files {
		doc, err := y.parse(contents[i], path.Dir(file))
		if err == nil {
			doc, err = y.include(doc, depth+1)
		}
		if err != nil {
			return nil, err
		}
		var pairs []*yaml.Node
		switch n.Tag {
		case "!include_dir_list":
			if doc != nil {
				out.Content = append(out.Content, doc)
			}
		case "!include_dir_merge_list":
			if doc != nil && doc.Kind == yaml.SequenceNode {
				out.Content = append(out.Content, doc.Content...)
			}
		case "!include_dir_named":
			if doc == nil {
				doc = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			}
			pairs = []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Style: yaml.DoubleQuotedStyle, Value: strings.TrimSuffix(path.Base(file), ".yaml")}, doc}
		case "!include_dir_merge_named":
			if doc != nil && doc.Kind == yaml.MappingNode {
				pairs = doc.Content
			}
		}
		for j := 0; j+1 < len(pairs); j += 2 {
			key, ok := yamlString(pairs[j])
			if !ok || seen[key] {
				return nil, yamlFail(yamlUnread) // a collision: which one Home Assistant keeps isn't proven
			}
			seen[key] = true
			out.Content = append(out.Content, pairs[j], pairs[j+1])
		}
	}
	return out, nil
}

// yamlIncludePath: a relative path that stays under its directory (call
// refuses any spelling the Relay would read differently).
func yamlIncludePath(dir, value string) (string, error) {
	if value == "" || strings.HasPrefix(value, "/") || slices.Contains(strings.Split(value, "/"), "..") {
		return "", yamlFail(yamlUnread)
	}
	return path.Join(dir, value), nil
}

// load reads and parses one file reached by !include (or the root).
func (y *yamlReader) load(file string, depth int) (*yaml.Node, error) {
	content, err := y.content(file)
	if err != nil {
		return nil, err
	}
	doc, err := y.parse(content, path.Dir(file))
	if err != nil {
		return nil, err
	}
	return y.include(doc, depth)
}

// parse: exactly one document (nil for an empty file) whose tags are all
// known; include tags are recorded with the directory they resolve from.
func (y *yamlReader) parse(content, dir string) (*yaml.Node, error) {
	dec := yaml.NewDecoder(strings.NewReader(content))
	var doc, extra yaml.Node
	if err := dec.Decode(&doc); errors.Is(err, io.EOF) || err == nil && len(doc.Content) == 0 {
		return nil, nil // empty, or only comments
	} else if err != nil || !errors.Is(dec.Decode(&extra), io.EOF) || len(doc.Content) != 1 {
		return nil, yamlFail(yamlUnread)
	}
	var scan func(n *yaml.Node) error
	scan = func(n *yaml.Node) error {
		if y.nodes++; y.nodes > yamlMaxNodes {
			return yamlFail(yamlUnread)
		}
		switch tag := n.ShortTag(); {
		case n.Kind == yaml.AliasNode:
			return nil
		case yamlIncludes[tag] && n.Kind == yaml.ScalarNode && n.Value != "":
			y.incDir[n] = dir
		case !yamlCoreTags[tag]:
			return yamlFail(yamlUnread)
		}
		for _, child := range n.Content {
			if err := scan(child); err != nil {
				return err
			}
		}
		return nil
	}
	return doc.Content[0], scan(doc.Content[0])
}
